// Package vm is the stack-based bytecode interpreter.
//
// Dispatch is a plain switch over Op — Go's compiler turns a dense
// switch on a small integer type into a jump table, so this is the
// idiomatic shape with no manual threaded-code tricks. We revisit
// only if profiling later shows dispatch dominating.
//
// Call model:
//
//	The VM keeps a stack of call frames per top-level invocation; the
//	active frame is held as a local struct (`cur`) so the hot opcode
//	dispatch doesn't pay an indirection. OpCall saves `cur` to the
//	call stack and switches to the callee's chunk; OpReturn pops back.
//	Operand stack is shared across frames.
//
//	Native (Go-implemented) functions are called inline — we never
//	push a frame for them — and their result lands on the operand
//	stack just like a JS function return.
//
//	Native functions that take callbacks (Array.prototype.map etc.)
//	receive a value.Caller — the *VM — through which they can
//	re-enter the interpreter on any Function. JS re-entry runs in a
//	NESTED invoke() call (separate callStack/valStack), so a single
//	top-level Eval can be modelled as a tree of invocations: the
//	main run, plus one nested run per native→JS callback frame.
//	Pure JS-to-JS calls stay inside one invoke()'s callStack array,
//	so deep JS recursion is bounded by heap rather than Go stack.
//
//	Globals (the top-level scope) live in a map shared by all
//	frames, accessed by OpLoadGlobal / OpStoreGlobal keyed by the
//	name string from the chunk's constants pool.
package vm

import (
	"encoding/binary"
	"fmt"
	"math"
	"math/big"
	"unsafe"

	"github.com/vimt/goquickjs/internal/bytecode"
	"github.com/vimt/goquickjs/internal/jserrors"
	"github.com/vimt/goquickjs/internal/value"
)

type frame struct {
	chunk  *bytecode.Chunk
	code   []byte
	ip     int
	locals []value.Value

	// function is the currently-executing Function value; needed so
	// OpLoadUpvalue / OpStoreUpvalue can reach its captured Upvalues
	// and so newly-created closures (OpClosure) can chain through
	// our upvalues. Top-level Run wraps the program chunk in a
	// synthetic Function with no upvalues, so this is always non-nil.
	function *value.Function

	// thisVal is the bound `this` for the call; OpLoadThis pushes it.
	// Top-level Run binds undefined; OpCallMethod binds the receiver
	// for JS function frames (same as it does for native fns).
	thisVal value.Value

	// valStackBase is the index in the shared valStack where this
	// frame's working stack begins. Throw unwinding truncates the
	// valStack back to a target frame's base before pushing the
	// exception to the catch handler.
	valStackBase int
}

// VM is one Eval's runtime state: the globals object plus the
// invoke()/Call() methods that drive interpretation. A fresh VM is
// created per Eval so user-mutated built-ins don't leak.
type VM struct {
	globals    map[string]value.Value
	microtasks []func()
	// callDepth tracks how deep the Go-level v.Call nesting goes
	// (each native-fn → JS callback re-entry adds one). Capped so
	// runaway native ↔ JS recursion surfaces as a JS-side
	// RangeError instead of growing the Go stack indefinitely.
	callDepth int
	// localsPool recycles frame-local slices by size class so deep
	// recursion doesn't churn the allocator. We bucket by exact
	// size up to 32 (covers virtually all real function frames);
	// larger sizes skip the pool.
	localsPool [33][]([]value.Value)
}

// getLocals grabs a zeroed []Value of length n, preferring a recycled
// one from the pool. The slice is exclusive to one frame at a time.
//
//go:inline
func (v *VM) getLocals(n uint16) []value.Value {
	if n < uint16(len(v.localsPool)) {
		bucket := v.localsPool[n]
		if k := len(bucket); k > 0 {
			out := bucket[k-1]
			v.localsPool[n] = bucket[:k-1]
			// Zero the recycled slice — caller relies on locals
			// starting at the zero Value{} so absent params read
			// as undefined.
			for i := range out {
				out[i] = value.Value{}
			}
			return out
		}
	}
	return make([]value.Value, n)
}

// putLocals returns a frame's locals to the pool. Caller must not
// retain the slice afterwards.
//
//go:inline
func (v *VM) putLocals(s []value.Value) {
	n := len(s)
	if n == 0 || n >= len(v.localsPool) {
		return
	}
	if len(v.localsPool[n]) < 128 {
		v.localsPool[n] = append(v.localsPool[n], s)
	}
}

const maxNativeReentry = 64

// EnqueueMicrotask appends a callback to the end-of-tick queue. The
// queue is drained by Run after the top-level Eval finishes — and is
// also drained after each foreground re-entry so that .then chains
// resolve in nested invocations.
func (v *VM) EnqueueMicrotask(task func()) {
	v.microtasks = append(v.microtasks, task)
}

// drainMicrotasks runs queued microtasks to completion. New tasks
// enqueued by reactions get picked up in subsequent loop iterations.
// Panics surfaced by tasks are caught by the JSThrow path inside
// Call, so a task that throws an uncaught exception just terminates
// silently — matching Promise/Web spec for unhandled rejections in
// the simplest engines.
func (v *VM) drainMicrotasks() {
	for len(v.microtasks) > 0 {
		t := v.microtasks[0]
		v.microtasks = v.microtasks[1:]
		t()
	}
	// Once nothing else is scheduled, surface any Promise that
	// finished in the rejected state without a handler attached.
	value.ReportUnhandledRejections()
}

// Compile-time interface check: VM is the canonical Caller given to
// every NativeFn.
var _ value.Caller = (*VM)(nil)

// Run executes a compiled chunk against the supplied globals map and
// returns the program's completion value. globals may be nil; the VM
// will create an empty map. Pre-populate globals with built-ins
// (Math, etc.) before calling.
//
// Convenience wrapper around NewVM + RunChunk for one-shot Evals.
func Run(chunk *bytecode.Chunk, globals map[string]value.Value) (value.Value, error) {
	v := NewVM(globals)
	return v.RunChunk(chunk)
}

// NewVM builds a fresh VM bound to the given globals map. Reuse the
// same VM across RunChunk calls when you need persistent state (e.g.
// a Runtime that lets the host inject Go data and re-Eval multiple
// snippets).
func NewVM(globals map[string]value.Value) *VM {
	if globals == nil {
		globals = map[string]value.Value{}
	}
	return &VM{globals: globals}
}

// RunChunk executes one compiled program against this VM's globals.
// Microtasks are drained at the end so async reactions settle before
// returning.
func (v *VM) RunChunk(chunk *bytecode.Chunk) (value.Value, error) {
	// Wrap the top-level chunk in a synthetic Function so
	// frame.function is never nil. Top-level OpClosure inside this
	// proto needs to instantiate descendants with upvalues from this
	// frame, which is only safe because top-level OpClosure descs
	// are always empty under the globals-not-locals scope model.
	topFn := &value.Function{Body: unsafe.Pointer(chunk)}
	ret, err := v.invoke(topFn, value.Undefined(), nil, nil, nil)
	v.drainMicrotasks()
	return ret, err
}

// Call lets a NativeFn re-enter the interpreter on any Function (JS
// or native). Args are passed by slice; thisVal binds the callee's
// receiver (only meaningful for natives until `this` keyword lands).
func (v *VM) Call(fn *value.Function, this value.Value, args []value.Value) (value.Value, error) {
	if fn == nil {
		return value.Value{}, fmt.Errorf("vm: nil function")
	}
	v.callDepth++
	defer func() { v.callDepth-- }()
	if v.callDepth > maxNativeReentry {
		return value.Value{}, &value.JSThrow{Val: value.MakeError("RangeError", "Maximum call stack size exceeded")}
	}
	if fn.Native != nil {
		return fn.Native(v, this, args)
	}
	if fn.IsArrow {
		// Arrow's `this` is fixed at creation; ignore the caller's.
		this = fn.BoundThis
	}
	// Generator functions: don't run the body — hand back a fresh
	// generator instance the caller can step with .next().
	if fn.IsGenerator {
		return value.ObjectVal(v.makeGenerator(fn, this, args)), nil
	}
	// Async functions get a fiber goroutine so `await` on a pending
	// Promise can really suspend the body.
	if fn.IsAsync {
		return v.runAsyncFiber(fn, this, args), nil
	}
	return v.invoke(fn, this, args, nil, nil)
}

// runAsyncFiber spawns a goroutine to execute fn, returning the
// settlement Promise to the caller immediately. The fiber rendez-
// vouses with main goroutine on ctx.paused/resume; each `await` on a
// pending promise unparks main and waits for the .then reaction to
// hand back the resolved value.
func (v *VM) runAsyncFiber(fn *value.Function, this value.Value, args []value.Value) value.Value {
	p, resolveP, rejectP := value.NewPendingPromise(v)
	ctx := &asyncCtx{
		resume: make(chan asyncMsg),
		paused: make(chan asyncStop),
	}
	go func() {
		ret, err := v.invoke(fn, this, args, nil, ctx)
		if err != nil {
			if t, ok := err.(*value.JSThrow); ok {
				rejectP(t.Val)
			} else {
				rejectP(value.String(err.Error()))
			}
		} else {
			resolveP(ret)
		}
		ctx.paused <- asyncStopDone
	}()
	// Block until the fiber either finishes or hits its first await.
	<-ctx.paused
	return p
}

// genCtx is the runtime coordination for a single generator. yieldCh
// is the body→caller signal (carries the produced value or a final
// returnSignal); sentCh is the caller→body wakeup carrying whatever
// the next .next(val) invocation passed in.
type genCtx struct {
	yieldCh chan genMsg
	sentCh  chan value.Value
	// stop is closed by .return() to tell the generator goroutine
	// to unwind. OpYield selects on it so a suspended fiber can be
	// woken from either the caller-resume direction or the kill
	// direction without leaking.
	stop chan struct{}
}

// errGeneratorStopped is the sentinel OpYield returns when stop has
// been signalled. invoke recognises it and unwinds cleanly so the
// fiber goroutine exits instead of blocking on the next sentCh recv.
var errGeneratorStopped = &generatorStopError{}

type generatorStopError struct{}

func (*generatorStopError) Error() string { return "vm: generator stopped" }

// asyncCtx is the runtime coordination for one async-function call.
// The fiber goroutine and its caller alternate on the two channels:
//
//   paused <- asyncStop{...}   // fiber tells main "I yielded / done"
//   <-resume                    // fiber waits for await to settle
//
// At every paused-send the rendez-vous matching receive is either
// the initial caller (in runAsyncFiber) or the .then reaction
// callback we install for an awaited promise. Either way only one
// of {main, fiber} is interacting with the VM at any moment.
type asyncCtx struct {
	resume chan asyncMsg
	paused chan asyncStop
}

type asyncMsg struct {
	val      value.Value
	rejected bool
}

type asyncStop int

const (
	asyncStopAwait asyncStop = iota
	asyncStopDone
)

// genMsg is what the generator goroutine sends across yieldCh. done
// distinguishes a yielded value from the body's final return.
type genMsg struct {
	val  value.Value
	done bool
	err  error
}

// makeGenerator wraps fn into a generator instance object. The body
// only starts running on the first .next() call; each yield rendez-
// vouses on the gen's channels so the caller and body alternate.
func (v *VM) makeGenerator(fn *value.Function, this value.Value, args []value.Value) *value.Object {
	ctx := &genCtx{
		yieldCh: make(chan genMsg),
		sentCh:  make(chan value.Value),
		stop:    make(chan struct{}),
	}
	state := &genState{ctx: ctx}
	obj := value.NewObject()
	obj.Set("next", value.FunctionVal(&value.Function{
		Name: "next", Arity: 1,
		Native: func(_ value.Caller, _ value.Value, callArgs []value.Value) (value.Value, error) {
			return state.advance(v, fn, this, args, callArgs), nil
		},
	}))
	obj.Set("return", value.FunctionVal(&value.Function{
		Name: "return", Arity: 1,
		Native: func(_ value.Caller, _ value.Value, callArgs []value.Value) (value.Value, error) {
			ret := value.Undefined()
			if len(callArgs) > 0 {
				ret = callArgs[0]
			}
			state.terminate(ret)
			r := value.NewObject()
			r.Set("value", ret)
			r.Set("done", value.Bool(true))
			return value.ObjectVal(r), nil
		},
	}))
	return obj
}

// genState is per-generator bookkeeping the .next/.return closures
// share. Kept outside the Object so callers can't introspect it.
type genState struct {
	ctx     *genCtx
	started bool
	done    bool
}

// advance pumps one tick of the generator: on first call it spawns
// the body goroutine; on subsequent calls it sends sentVal and waits
// for the next yield (or completion).
func (g *genState) advance(v *VM, fn *value.Function, this value.Value, args, callArgs []value.Value) value.Value {
	result := value.NewObject()
	if g.done {
		result.Set("value", value.Undefined())
		result.Set("done", value.Bool(true))
		return value.ObjectVal(result)
	}
	if !g.started {
		g.started = true
		go func() {
			ret, err := v.invoke(fn, this, args, g.ctx, nil)
			if err == errGeneratorStopped {
				return
			}
			// Final send may have nobody listening if .return()
			// terminated us between yields — fall back on stop.
			select {
			case g.ctx.yieldCh <- genMsg{val: ret, done: true, err: err}:
			case <-g.ctx.stop:
			}
		}()
	} else {
		var sent value.Value
		if len(callArgs) > 0 {
			sent = callArgs[0]
		} else {
			sent = value.Undefined()
		}
		g.ctx.sentCh <- sent
	}
	msg := <-g.ctx.yieldCh
	if msg.done {
		g.done = true
		result.Set("value", msg.val)
		result.Set("done", value.Bool(true))
	} else {
		result.Set("value", msg.val)
		result.Set("done", value.Bool(false))
	}
	return value.ObjectVal(result)
}

// terminate is the .return() hook. Closes the stop channel so the
// suspended fiber's OpYield select unblocks with errGeneratorStopped
// and the goroutine unwinds — preventing the long-running leak that
// would otherwise hold the chunk + locals indefinitely.
func (g *genState) terminate(_ value.Value) {
	if !g.done {
		g.done = true
		close(g.ctx.stop)
	}
}

// invoke runs a single function to completion, seeded with args in the
// leading locals. fn becomes frame.function so its Upvalues are
// reachable to OpLoadUpvalue / OpClosure inside the body.
// gen, when non-nil, makes this a generator body — OpYield will use
// gen's channels to suspend/resume rather than crashing.
func (v *VM) invoke(fn *value.Function, this value.Value, args []value.Value, gen *genCtx, async *asyncCtx) (value.Value, error) {
	chunk := (*bytecode.Chunk)(fn.Body)
	callStack := make([]frame, 0, 8)
	cur := frame{
		chunk:        chunk,
		code:         chunk.Code,
		ip:           0,
		locals:       make([]value.Value, chunk.MaxLocals),
		function:     fn,
		thisVal:      this,
		valStackBase: 0,
	}
	// Honour fn.Arity / HasRest so v.Call (used by OpNew, native
	// callbacks, OpCallApply) lays out locals the same way doCall
	// does. Without this, a rest-using constructor invoked via
	// `new Foo(...)` sees the first arg as a primitive rather than
	// the wrapping Array.
	argN := len(args)
	if argN > fn.Arity {
		argN = fn.Arity
	}
	for i := 0; i < argN; i++ {
		cur.locals[i] = args[i]
	}
	if fn.HasRest {
		rest := value.NewArray()
		for i := fn.Arity; i < len(args); i++ {
			rest.Push(args[i])
		}
		if fn.Arity < int(chunk.MaxLocals) {
			cur.locals[fn.Arity] = value.ArrayVal(rest)
		}
	}
	if fn.HasArguments && int(fn.ArgumentsSlot) < int(chunk.MaxLocals) {
		argsArr := value.NewArray()
		for _, a := range args {
			argsArr.Push(a)
		}
		cur.locals[fn.ArgumentsSlot] = value.ArrayVal(argsArr)
	}

	valStack := make([]value.Value, 0, 64)
	push := func(x value.Value) { valStack = append(valStack, x) }
	pop := func() value.Value {
		x := valStack[len(valStack)-1]
		valStack = valStack[:len(valStack)-1]
		return x
	}
	readI16 := func(code []byte, ip int) int {
		return int(int16(binary.LittleEndian.Uint16(code[ip:])))
	}

	for cur.ip < len(cur.code) {
		op := bytecode.Op(cur.code[cur.ip])
		cur.ip++
		switch op {
		case bytecode.OpConstUndefined:
			valStack = append(valStack, value.Undefined())
		case bytecode.OpConstNull:
			valStack = append(valStack, value.Null())
		case bytecode.OpConstTrue:
			valStack = append(valStack, value.Bool(true))
		case bytecode.OpConstFalse:
			valStack = append(valStack, value.Bool(false))
		case bytecode.OpConstK:
			idx := binary.LittleEndian.Uint16(cur.code[cur.ip:])
			cur.ip += 2
			valStack = append(valStack, cur.chunk.Constants[idx])

		case bytecode.OpAdd:
			// In-place: read top two slots, compute, write back to
			// the second-from-top slot, shrink stack by one. Saves
			// the pop/pop/push closure dance and the append's
			// growslice check on the hot path.
			last := len(valStack)
			l := valStack[last-2]
			r := valStack[last-1]
			lt, rt := l.Type(), r.Type()
			if lt == value.TypeNumber && rt == value.TypeNumber {
				valStack[last-2] = value.Number(l.AsNumber() + r.AsNumber())
				valStack = valStack[:last-1]
			} else if lt == value.TypeString || rt == value.TypeString {
				valStack[last-2] = value.String(l.String() + r.String())
				valStack = valStack[:last-1]
			} else if lt == value.TypeBigInt && rt == value.TypeBigInt {
				ret, err := bigArith("add", l, r)
				if err != nil {
					if t, ok := err.(*value.JSThrow); ok {
						valStack = valStack[:last-2]
						newCur, handled := unwindTo(cur, &callStack, &valStack, t.Val)
						if !handled {
							return value.Value{}, t
						}
						cur = newCur
						continue
					}
					return value.Value{}, err
				}
				valStack[last-2] = ret
				valStack = valStack[:last-1]
			} else {
				valStack[last-2] = value.Number(l.AsNumber() + r.AsNumber())
				valStack = valStack[:last-1]
			}
		case bytecode.OpSub:
			last := len(valStack)
			l := valStack[last-2]
			r := valStack[last-1]
			if l.Type() == value.TypeBigInt && r.Type() == value.TypeBigInt {
				ret, err := bigArith("sub", l, r)
				if err != nil {
					if t, ok := err.(*value.JSThrow); ok {
						valStack = valStack[:last-2]
						newCur, handled := unwindTo(cur, &callStack, &valStack, t.Val)
						if !handled {
							return value.Value{}, t
						}
						cur = newCur
						continue
					}
					return value.Value{}, err
				}
				valStack[last-2] = ret
			} else {
				valStack[last-2] = value.Number(l.AsNumber() - r.AsNumber())
			}
			valStack = valStack[:last-1]
		case bytecode.OpMul:
			last := len(valStack)
			l := valStack[last-2]
			r := valStack[last-1]
			if l.Type() == value.TypeBigInt && r.Type() == value.TypeBigInt {
				ret, err := bigArith("mul", l, r)
				if err != nil {
					if t, ok := err.(*value.JSThrow); ok {
						valStack = valStack[:last-2]
						newCur, handled := unwindTo(cur, &callStack, &valStack, t.Val)
						if !handled {
							return value.Value{}, t
						}
						cur = newCur
						continue
					}
					return value.Value{}, err
				}
				valStack[last-2] = ret
			} else {
				valStack[last-2] = value.Number(l.AsNumber() * r.AsNumber())
			}
			valStack = valStack[:last-1]
		case bytecode.OpDiv:
			last := len(valStack)
			l := valStack[last-2]
			r := valStack[last-1]
			if l.Type() == value.TypeBigInt && r.Type() == value.TypeBigInt {
				ret, err := bigArith("div", l, r)
				if err != nil {
					if t, ok := err.(*value.JSThrow); ok {
						valStack = valStack[:last-2]
						newCur, handled := unwindTo(cur, &callStack, &valStack, t.Val)
						if !handled {
							return value.Value{}, t
						}
						cur = newCur
						continue
					}
					return value.Value{}, err
				}
				valStack[last-2] = ret
			} else {
				valStack[last-2] = value.Number(l.AsNumber() / r.AsNumber())
			}
			valStack = valStack[:last-1]
		case bytecode.OpMod:
			last := len(valStack)
			l := valStack[last-2]
			r := valStack[last-1]
			if l.Type() == value.TypeBigInt && r.Type() == value.TypeBigInt {
				ret, err := bigArith("mod", l, r)
				if err != nil {
					if t, ok := err.(*value.JSThrow); ok {
						valStack = valStack[:last-2]
						newCur, handled := unwindTo(cur, &callStack, &valStack, t.Val)
						if !handled {
							return value.Value{}, t
						}
						cur = newCur
						continue
					}
					return value.Value{}, err
				}
				valStack[last-2] = ret
			} else {
				valStack[last-2] = value.Number(jsModFast(l.AsNumber(), r.AsNumber()))
			}
			valStack = valStack[:last-1]
		case bytecode.OpPow:
			last := len(valStack)
			l := valStack[last-2]
			r := valStack[last-1]
			if l.Type() == value.TypeBigInt && r.Type() == value.TypeBigInt {
				ret, err := bigArith("pow", l, r)
				if err != nil {
					if t, ok := err.(*value.JSThrow); ok {
						valStack = valStack[:last-2]
						newCur, handled := unwindTo(cur, &callStack, &valStack, t.Val)
						if !handled {
							return value.Value{}, t
						}
						cur = newCur
						continue
					}
					return value.Value{}, err
				}
				valStack[last-2] = ret
			} else {
				valStack[last-2] = value.Number(math.Pow(l.AsNumber(), r.AsNumber()))
			}
			valStack = valStack[:last-1]
		case bytecode.OpNeg:
			x := pop()
			if x.Type() == value.TypeBigInt {
				out := new(big.Int).Neg(x.AsBigInt().I)
				push(value.BigIntVal(&value.BigInt{I: out}))
			} else {
				push(value.Number(-x.AsNumber()))
			}

		case bytecode.OpBitAnd:
			r := pop()
			l := pop()
			push(value.Number(float64(toInt32(l.AsNumber()) & toInt32(r.AsNumber()))))
		case bytecode.OpBitOr:
			r := pop()
			l := pop()
			push(value.Number(float64(toInt32(l.AsNumber()) | toInt32(r.AsNumber()))))
		case bytecode.OpBitXor:
			r := pop()
			l := pop()
			push(value.Number(float64(toInt32(l.AsNumber()) ^ toInt32(r.AsNumber()))))
		case bytecode.OpBitNot:
			push(value.Number(float64(^toInt32(pop().AsNumber()))))
		case bytecode.OpShl:
			r := pop()
			l := pop()
			push(value.Number(float64(toInt32(l.AsNumber()) << (toUint32(r.AsNumber()) & 31))))
		case bytecode.OpShr:
			r := pop()
			l := pop()
			push(value.Number(float64(toInt32(l.AsNumber()) >> (toUint32(r.AsNumber()) & 31))))
		case bytecode.OpUShr:
			r := pop()
			l := pop()
			push(value.Number(float64(toUint32(l.AsNumber()) >> (toUint32(r.AsNumber()) & 31))))

		case bytecode.OpNot:
			push(value.Bool(!truthy(pop())))
		case bytecode.OpTypeof:
			push(value.String(typeofValue(pop())))
		case bytecode.OpVoid:
			pop()
			push(value.Undefined())
		case bytecode.OpForInKeys:
			src := pop()
			out := value.NewArray()
			switch src.Type() {
			case value.TypeObject:
				for _, n := range src.AsObject().PropNames() {
					out.Push(value.String(n))
				}
			case value.TypeArray:
				n := src.AsArray().Length()
				for i := 0; i < n; i++ {
					out.Push(value.String(fmt.Sprintf("%d", i)))
				}
			case value.TypeString:
				s := src.AsString()
				for i := 0; i < len(s); i++ {
					out.Push(value.String(fmt.Sprintf("%d", i)))
				}
			}
			push(value.ArrayVal(out))
		case bytecode.OpYield:
			if gen == nil {
				return value.Value{}, fmt.Errorf("vm: yield outside generator")
			}
			y := pop()
			select {
			case gen.yieldCh <- genMsg{val: y, done: false}:
			case <-gen.stop:
				return value.Undefined(), errGeneratorStopped
			}
			select {
			case sent := <-gen.sentCh:
				push(sent)
			case <-gen.stop:
				return value.Undefined(), errGeneratorStopped
			}
		case bytecode.OpAwait:
			vv := pop()
			val, state := value.PromiseUnwrap(vv)
			// True suspension path: when we're inside an async fiber
			// and the awaited promise hasn't settled yet, register a
			// .then reaction that wakes us, then yield the VM back to
			// the caller via the fiber's paused channel.
			if state == "pending" && async != nil {
				value.PromiseSubscribe(v, vv,
					func(x value.Value) {
						async.resume <- asyncMsg{val: x, rejected: false}
						<-async.paused
					},
					func(x value.Value) {
						async.resume <- asyncMsg{val: x, rejected: true}
						<-async.paused
					},
				)
				async.paused <- asyncStopAwait
				msg := <-async.resume
				if msg.rejected {
					newCur, handled := unwindTo(cur, &callStack, &valStack, msg.val)
					if !handled {
						return value.Value{}, &value.JSThrow{Val: msg.val}
					}
					cur = newCur
					continue
				}
				push(msg.val)
				continue
			}
			// No fiber context (or already settled): the legacy
			// drain-and-retry path covers fulfilled / rejected /
			// synchronously-settleable pending promises.
			for state == "pending" && len(v.microtasks) > 0 {
				v.drainMicrotasks()
				val, state = value.PromiseUnwrap(vv)
			}
			switch state {
			case "fulfilled":
				push(val)
			case "rejected":
				newCur, handled := unwindTo(cur, &callStack, &valStack, val)
				if !handled {
					return value.Value{}, &value.JSThrow{Val: val}
				}
				cur = newCur
				continue
			default:
				ex := value.MakeError("TypeError", "await on an indefinitely-pending promise is not supported")
				newCur, handled := unwindTo(cur, &callStack, &valStack, ex)
				if !handled {
					return value.Value{}, &value.JSThrow{Val: ex}
				}
				cur = newCur
				continue
			}
		case bytecode.OpDeleteProp:
			nameIdx := binary.LittleEndian.Uint16(cur.code[cur.ip:])
			cur.ip += 2
			name := cur.chunk.Constants[nameIdx].AsString()
			obj := pop()
			if obj.Type() == value.TypeObject {
				push(value.Bool(obj.AsObject().Delete(name)))
			} else {
				push(value.Bool(true))
			}
		case bytecode.OpDeleteByVal:
			key := pop()
			obj := pop()
			if obj.Type() == value.TypeObject {
				push(value.Bool(obj.AsObject().Delete(key.String())))
			} else {
				push(value.Bool(true))
			}
		case bytecode.OpLt:
			last := len(valStack)
			valStack[last-2] = value.Bool(valStack[last-2].AsNumber() < valStack[last-1].AsNumber())
			valStack = valStack[:last-1]
		case bytecode.OpLe:
			last := len(valStack)
			valStack[last-2] = value.Bool(valStack[last-2].AsNumber() <= valStack[last-1].AsNumber())
			valStack = valStack[:last-1]
		case bytecode.OpGt:
			last := len(valStack)
			valStack[last-2] = value.Bool(valStack[last-2].AsNumber() > valStack[last-1].AsNumber())
			valStack = valStack[:last-1]
		case bytecode.OpGe:
			last := len(valStack)
			valStack[last-2] = value.Bool(valStack[last-2].AsNumber() >= valStack[last-1].AsNumber())
			valStack = valStack[:last-1]
		case bytecode.OpEq:
			last := len(valStack)
			valStack[last-2] = value.Bool(jsEqual(valStack[last-2], valStack[last-1], false))
			valStack = valStack[:last-1]
		case bytecode.OpNeq:
			last := len(valStack)
			valStack[last-2] = value.Bool(!jsEqual(valStack[last-2], valStack[last-1], false))
			valStack = valStack[:last-1]
		case bytecode.OpStrictEq:
			last := len(valStack)
			valStack[last-2] = value.Bool(jsEqual(valStack[last-2], valStack[last-1], true))
			valStack = valStack[:last-1]
		case bytecode.OpStrictNeq:
			last := len(valStack)
			valStack[last-2] = value.Bool(!jsEqual(valStack[last-2], valStack[last-1], true))
			valStack = valStack[:last-1]

		case bytecode.OpJump:
			rel := readI16(cur.code, cur.ip)
			cur.ip += 2 + rel
		case bytecode.OpJumpIfFalsePeek:
			rel := readI16(cur.code, cur.ip)
			cur.ip += 2
			if !truthy(valStack[len(valStack)-1]) {
				cur.ip += rel
			}
		case bytecode.OpJumpIfTruePeek:
			rel := readI16(cur.code, cur.ip)
			cur.ip += 2
			if truthy(valStack[len(valStack)-1]) {
				cur.ip += rel
			}
		case bytecode.OpJumpIfFalse:
			rel := readI16(cur.code, cur.ip)
			cur.ip += 2
			n := len(valStack) - 1
			top := valStack[n]
			valStack = valStack[:n]
			if !truthy(top) {
				cur.ip += rel
			}
		case bytecode.OpJumpIfTrue:
			rel := readI16(cur.code, cur.ip)
			cur.ip += 2
			n := len(valStack) - 1
			top := valStack[n]
			valStack = valStack[:n]
			if truthy(top) {
				cur.ip += rel
			}
		case bytecode.OpJumpIfNotNullishPeek:
			rel := readI16(cur.code, cur.ip)
			cur.ip += 2
			t := valStack[len(valStack)-1].Type()
			if t != value.TypeUndefined && t != value.TypeNull {
				cur.ip += rel
			}
		case bytecode.OpJumpIfNullishPeek:
			rel := readI16(cur.code, cur.ip)
			cur.ip += 2
			t := valStack[len(valStack)-1].Type()
			if t == value.TypeUndefined || t == value.TypeNull {
				cur.ip += rel
			}

		case bytecode.OpDup:
			valStack = append(valStack, valStack[len(valStack)-1])

		case bytecode.OpLoadThis:
			valStack = append(valStack, cur.thisVal)

		case bytecode.OpGetIterator:
			src := pop()
			switch src.Type() {
			case value.TypeArray:
				push(value.MakeArrayIterator(src.AsArray()))
			case value.TypeString:
				push(value.MakeStringIterator(src.AsString()))
			case value.TypeObject:
				obj := src.AsObject()
				// Spec: call obj[@@iterator](). We store @@iterator
				// as a string key (Symbol-keyed properties aren't a
				// thing here yet); fall back to duck-typed
				// `obj.next` so a generator's result Object — which
				// IS its own iterator — works out of the box.
				if iterFn, ok := obj.Get("Symbol.iterator"); ok && iterFn.Type() == value.TypeFunction {
					it, err := v.Call(iterFn.AsFunction(), src, nil)
					if err != nil {
						if t, ok := err.(*value.JSThrow); ok {
							newCur, handled := unwindTo(cur, &callStack, &valStack, t.Val)
							if !handled {
								return value.Value{}, t
							}
							cur = newCur
							continue
						}
						return value.Value{}, err
					}
					push(it)
				} else if next, ok := obj.Get("next"); ok && next.Type() == value.TypeFunction {
					push(src)
				} else {
					ex := value.MakeError("TypeError", "value is not iterable")
					newCur, handled := unwindTo(cur, &callStack, &valStack, ex)
					if !handled {
						return value.Value{}, &value.JSThrow{Val: ex}
					}
					cur = newCur
				}
			default:
				ex := value.MakeError("TypeError", "value is not iterable")
				newCur, handled := unwindTo(cur, &callStack, &valStack, ex)
				if !handled {
					return value.Value{}, &value.JSThrow{Val: ex}
				}
				cur = newCur
			}

		case bytecode.OpDefineGetter:
			nameIdx := binary.LittleEndian.Uint16(cur.code[cur.ip:])
			cur.ip += 2
			fnVal := pop()
			obj := valStack[len(valStack)-1]
			if obj.Type() == value.TypeObject && fnVal.Type() == value.TypeFunction {
				name := cur.chunk.Constants[nameIdx].AsString()
				prev := obj.AsObject().Accessor(name)
				var setter *value.Function
				if prev != nil {
					setter = prev.Set
				}
				obj.AsObject().SetAccessor(name, fnVal.AsFunction(), setter)
			}
		case bytecode.OpDefineSetter:
			nameIdx := binary.LittleEndian.Uint16(cur.code[cur.ip:])
			cur.ip += 2
			fnVal := pop()
			obj := valStack[len(valStack)-1]
			if obj.Type() == value.TypeObject && fnVal.Type() == value.TypeFunction {
				name := cur.chunk.Constants[nameIdx].AsString()
				existing := obj.AsObject().Accessor(name)
				var getter *value.Function
				if existing != nil {
					getter = existing.Get
				}
				obj.AsObject().SetAccessor(name, getter, fnVal.AsFunction())
			}
		case bytecode.OpIn:
			right := pop()
			left := pop()
			key := left.String()
			has := false
			switch right.Type() {
			case value.TypeObject:
				_, has = right.AsObject().Get(key)
			case value.TypeArray:
				if i, ok := stringAsIndex(key); ok && i < right.AsArray().Length() {
					has = true
				} else if key == "length" {
					has = true
				}
			case value.TypeString:
				if i, ok := stringAsIndex(key); ok && i < len(right.AsString()) {
					has = true
				} else if key == "length" {
					has = true
				}
			default:
				ex := value.MakeError("TypeError", "Cannot use 'in' on non-object")
				newCur, handled := unwindTo(cur, &callStack, &valStack, ex)
				if !handled {
					return value.Value{}, &value.JSThrow{Val: ex}
				}
				cur = newCur
				continue
			}
			push(value.Bool(has))
		case bytecode.OpInstanceof:
			right := pop()
			left := pop()
			if right.Type() != value.TypeFunction {
				ex := value.MakeError("TypeError", "Right-hand side of 'instanceof' is not callable")
				newCur, handled := unwindTo(cur, &callStack, &valStack, ex)
				if !handled {
					return value.Value{}, &value.JSThrow{Val: ex}
				}
				cur = newCur
				continue
			}
			// Symbol.hasInstance: if the right side has a function at
			// the well-known Symbol.hasInstance slot, call it with
			// `left` and use its truthiness as the result. Symbol-
			// keyed properties collapse to the symbol's description
			// string (`Symbol.hasInstance`).
			fn := right.AsFunction()
			if fn.Props != nil {
				if hi, ok := fn.Props.GetOwn("Symbol.hasInstance"); ok && hi.Type() == value.TypeFunction {
					ret, err := v.Call(hi.AsFunction(), right, []value.Value{left})
					if err != nil {
						if t, ok := err.(*value.JSThrow); ok {
							newCur, handled := unwindTo(cur, &callStack, &valStack, t.Val)
							if !handled {
								return value.Value{}, t
							}
							cur = newCur
							continue
						}
						return value.Value{}, err
					}
					push(value.Bool(truthy(ret)))
					continue
				}
			}
			push(value.Bool(jsInstanceof(left, fn)))

		case bytecode.OpThrow:
			ex := pop()
			newCur, handled := unwindTo(cur, &callStack, &valStack, ex)
			if !handled {
				return value.Value{}, &value.JSThrow{Val: ex}
			}
			cur = newCur

		case bytecode.OpNew:
			argCount := int(cur.code[cur.ip])
			cur.ip++
			argsStart := len(valStack) - argCount
			fnIdx := argsStart - 1
			if fnIdx < 0 {
				return value.Value{}, fmt.Errorf("vm: new stack underflow")
			}
			fnVal := valStack[fnIdx]
			if fnVal.Type() != value.TypeFunction {
				ex := value.MakeError("TypeError", "value is not a constructor")
				newCur, handled := unwindTo(cur, &callStack, &valStack, ex)
				if !handled {
					return value.Value{}, &value.JSThrow{Val: ex}
				}
				cur = newCur
				continue
			}
			fn := fnVal.AsFunction()
			// Build the new object with proto = fn.prototype. Route
			// through FunctionGetProp so the canonical lazy init
			// (`Foo.prototype` autogenerated on first access) fires
			// the moment `new Foo()` is evaluated — without this the
			// instance has proto=nil and `instanceof` always returns
			// false for fresh constructors.
			newObj := value.NewObject()
			protoV := value.FunctionGetProp(fn, "prototype")
			if protoV.Type() == value.TypeObject {
				newObj.SetProto(protoV.AsObject())
			}
			thisVal := value.ObjectVal(newObj)
			argsCopy := make([]value.Value, argCount)
			copy(argsCopy, valStack[argsStart:])
			valStack = valStack[:fnIdx]
			ret, err := v.Call(fn, thisVal, argsCopy)
			if err != nil {
				if t, ok := err.(*value.JSThrow); ok {
					newCur, handled := unwindTo(cur, &callStack, &valStack, t.Val)
					if !handled {
						return value.Value{}, t
					}
					cur = newCur
					continue
				}
				return value.Value{}, err
			}
			// Spec: if the constructor returned an object, use it;
			// otherwise the freshly created instance is the result.
			if ret.Type() == value.TypeObject {
				push(ret)
			} else {
				push(thisVal)
			}

		case bytecode.OpClosure:
			idx := binary.LittleEndian.Uint16(cur.code[cur.ip:])
			cur.ip += 2
			proto := cur.chunk.Constants[idx].AsFunction()
			bound := &value.Function{
				Name:         proto.Name,
				Arity:        proto.Arity,
				Body:         proto.Body,
				UpvalueDescs: proto.UpvalueDescs,
				Upvalues:     make([]*value.Value, len(proto.UpvalueDescs)),
				IsArrow:      proto.IsArrow,
				HasRest:       proto.HasRest,
				IsAsync:       proto.IsAsync,
				IsGenerator:   proto.IsGenerator,
				HasArguments:  proto.HasArguments,
				ArgumentsSlot: proto.ArgumentsSlot,
				LocalsEscape:  proto.LocalsEscape,
			}
			if proto.IsArrow {
				// Arrow captures the enclosing frame's `this`.
				bound.BoundThis = cur.thisVal
			}
			for i, desc := range proto.UpvalueDescs {
				if desc.IsLocal {
					bound.Upvalues[i] = &cur.locals[desc.Index]
				} else {
					bound.Upvalues[i] = cur.function.Upvalues[desc.Index]
				}
			}
			push(value.FunctionVal(bound))
		case bytecode.OpLoadUpvalue:
			idx := binary.LittleEndian.Uint16(cur.code[cur.ip:])
			cur.ip += 2
			push(*cur.function.Upvalues[idx])
		case bytecode.OpStoreUpvalue:
			idx := binary.LittleEndian.Uint16(cur.code[cur.ip:])
			cur.ip += 2
			*cur.function.Upvalues[idx] = pop()

		case bytecode.OpNewObject:
			push(value.ObjectVal(value.NewObject()))
		case bytecode.OpGetProp:
			nameIdx := binary.LittleEndian.Uint16(cur.code[cur.ip:])
			icIdx := binary.LittleEndian.Uint16(cur.code[cur.ip+2:])
			cur.ip += 4
			obj := pop()
			name := cur.chunk.Constants[nameIdx].AsString()
			// Proxy trap: route through handler.get if present.
			if obj.Type() == value.TypeObject && obj.AsObject().Proxy != nil {
				p := obj.AsObject().Proxy
				if trap, ok := p.Handler.GetOwn("get"); ok && trap.Type() == value.TypeFunction {
					ret, err := v.Call(trap.AsFunction(), value.ObjectVal(p.Handler),
						[]value.Value{value.ObjectVal(p.Target), value.String(name), obj})
					if err != nil {
						if t, ok := err.(*value.JSThrow); ok {
							newCur, handled := unwindTo(cur, &callStack, &valStack, t.Val)
							if !handled {
								return value.Value{}, t
							}
							cur = newCur
							continue
						}
						return value.Value{}, err
					}
					push(ret)
					continue
				}
				obj = value.ObjectVal(p.Target)
			}
			if obj.Type() == value.TypeObject {
				o := obj.AsObject()
				// Resolve the accessor that applies, if any. An OWN accessor
				// for `name` wins over its data slot. Absent one, an own
				// DATA property is authoritative — it shadows every inherited
				// accessor — so we take the inline-cache hit straight away and
				// skip the prototype-chain accessor walk entirely. o.Accessor
				// is a cheap nil-map check on the common (no-own-accessor)
				// path, far cheaper than LookupAccessor's chain walk.
				var acc *value.Accessor
				if own := o.Accessor(name); own != nil {
					acc = own
				} else {
					// Inline-cache fast path: a shape hit collapses the
					// name→slot map lookup to a pointer compare.
					if val, ok := o.GetOwnCached(&cur.chunk.PropCaches[icIdx], name); ok {
						push(val)
						continue
					}
					// Own-data miss: only inherited accessors/data remain.
					acc = o.LookupAccessor(name)
				}
				if acc != nil {
					if acc.Get == nil {
						push(value.Undefined())
						continue
					}
					ret, err := v.Call(acc.Get, obj, nil)
					if err != nil {
						if t, ok := err.(*value.JSThrow); ok {
							newCur, handled := unwindTo(cur, &callStack, &valStack, t.Val)
							if !handled {
								return value.Value{}, t
							}
							cur = newCur
							continue
						}
						return value.Value{}, err
					}
					push(ret)
					continue
				}
				push(o.GetInherited(name))
				continue
			}
			push(getProp(obj, name))
		case bytecode.OpSetProp:
			nameIdx := binary.LittleEndian.Uint16(cur.code[cur.ip:])
			icIdx := binary.LittleEndian.Uint16(cur.code[cur.ip+2:])
			cur.ip += 4
			val := pop()
			obj := pop()
			name := cur.chunk.Constants[nameIdx].AsString()
			// Proxy trap.
			if obj.Type() == value.TypeObject && obj.AsObject().Proxy != nil {
				p := obj.AsObject().Proxy
				if trap, ok := p.Handler.GetOwn("set"); ok && trap.Type() == value.TypeFunction {
					if _, err := v.Call(trap.AsFunction(), value.ObjectVal(p.Handler),
						[]value.Value{value.ObjectVal(p.Target), value.String(name), val, obj}); err != nil {
						if t, ok := err.(*value.JSThrow); ok {
							newCur, handled := unwindTo(cur, &callStack, &valStack, t.Val)
							if !handled {
								return value.Value{}, t
							}
							cur = newCur
							continue
						}
						return value.Value{}, err
					}
					push(val)
					continue
				}
				obj = value.ObjectVal(p.Target)
			}
			switch obj.Type() {
			case value.TypeObject:
				if a := obj.AsObject().LookupAccessor(name); a != nil {
					if a.Set != nil {
						if _, err := v.Call(a.Set, obj, []value.Value{val}); err != nil {
							if t, ok := err.(*value.JSThrow); ok {
								newCur, handled := unwindTo(cur, &callStack, &valStack, t.Val)
								if !handled {
									return value.Value{}, t
								}
								cur = newCur
								continue
							}
							return value.Value{}, err
						}
					}
					push(val)
					continue
				}
				obj.AsObject().SetCached(&cur.chunk.SetCaches[icIdx], name, val)
			case value.TypeFunction:
				value.FunctionSetProp(obj.AsFunction(), name, val)
			case value.TypeNull, value.TypeUndefined:
				// Spec: PutValue on null/undefined throws TypeError in
				// every mode (the wrapper toObject step has nothing to
				// coerce).
				what := "null"
				if obj.Type() == value.TypeUndefined {
					what = "undefined"
				}
				ex := value.MakeError("TypeError",
					"Cannot set property '"+name+"' of "+what)
				newCur, handled := unwindTo(cur, &callStack, &valStack, ex)
				if !handled {
					return value.Value{}, &value.JSThrow{Val: ex}
				}
				cur = newCur
				continue
			default:
				// Sloppy-mode primitive assignment (number/string/bool):
				// the value is coerced to a temporary wrapper, the write
				// hits that wrapper, the wrapper is discarded — observable
				// effect is a no-op, and the assignment's completion is
				// the rhs (push below). Strict mode would TypeError; we
				// run sloppy.
			}
			push(val)

		case bytecode.OpLoadLocal:
			slot := binary.LittleEndian.Uint16(cur.code[cur.ip:])
			cur.ip += 2
			valStack = append(valStack, cur.locals[slot])
		case bytecode.OpStoreLocal:
			slot := binary.LittleEndian.Uint16(cur.code[cur.ip:])
			cur.ip += 2
			n := len(valStack) - 1
			cur.locals[slot] = valStack[n]
			valStack = valStack[:n]

		case bytecode.OpLoadGlobal:
			nameIdx := binary.LittleEndian.Uint16(cur.code[cur.ip:])
			cur.ip += 2
			name := cur.chunk.Constants[nameIdx].AsString()
			valStack = append(valStack, v.globals[name])
		case bytecode.OpStoreGlobal:
			nameIdx := binary.LittleEndian.Uint16(cur.code[cur.ip:])
			cur.ip += 2
			name := cur.chunk.Constants[nameIdx].AsString()
			n := len(valStack) - 1
			v.globals[name] = valStack[n]
			valStack = valStack[:n]

		case bytecode.OpCall:
			argCount := int(cur.code[cur.ip])
			cur.ip++
			argsStart := len(valStack) - argCount
			fnIdx := argsStart - 1
			if fnIdx < 0 {
				return value.Value{}, fmt.Errorf("vm: call stack underflow")
			}
			fnVal := valStack[fnIdx]
			if fnVal.Type() != value.TypeFunction {
				ex := value.MakeError("TypeError", "value is not a function")
				newCur, handled := unwindTo(cur, &callStack, &valStack, ex)
				if !handled {
					return value.Value{}, &value.JSThrow{Val: ex}
				}
				cur = newCur
				continue
			}
			fn := fnVal.AsFunction()
			args := valStack[argsStart:]
			next, err := v.doCall(&cur, &callStack, &valStack, fn, value.Undefined(), args, argCount, fnIdx)
			if err != nil {
				if t, ok := err.(*value.JSThrow); ok {
					newCur, handled := unwindTo(cur, &callStack, &valStack, t.Val)
					if !handled {
						return value.Value{}, t
					}
					cur = newCur
					continue
				}
				return value.Value{}, err
			}
			cur = next

		case bytecode.OpCallMethod:
			argCount := int(cur.code[cur.ip])
			cur.ip++
			argsStart := len(valStack) - argCount
			fnIdx := argsStart - 1
			thisIdx := fnIdx - 1
			if thisIdx < 0 {
				return value.Value{}, fmt.Errorf("vm: method call stack underflow")
			}
			thisVal := valStack[thisIdx]
			fnVal := valStack[fnIdx]
			if fnVal.Type() != value.TypeFunction {
				ex := value.MakeError("TypeError", "called value is not a function")
				newCur, handled := unwindTo(cur, &callStack, &valStack, ex)
				if !handled {
					return value.Value{}, &value.JSThrow{Val: ex}
				}
				cur = newCur
				continue
			}
			fn := fnVal.AsFunction()
			args := valStack[argsStart:]
			next, err := v.doCall(&cur, &callStack, &valStack, fn, thisVal, args, argCount, thisIdx)
			if err != nil {
				if t, ok := err.(*value.JSThrow); ok {
					newCur, handled := unwindTo(cur, &callStack, &valStack, t.Val)
					if !handled {
						return value.Value{}, t
					}
					cur = newCur
					continue
				}
				return value.Value{}, err
			}
			cur = next

		case bytecode.OpNewArray:
			push(value.ArrayVal(value.NewArray()))
		case bytecode.OpArrayPush:
			x := pop()
			arr := valStack[len(valStack)-1]
			if arr.Type() != value.TypeArray {
				return value.Value{}, fmt.Errorf("vm: ArrayPush on non-array: %w", jserrors.ErrNotImplemented)
			}
			arr.AsArray().Push(x)
		case bytecode.OpArraySpread:
			src := pop()
			arr := valStack[len(valStack)-1]
			if arr.Type() != value.TypeArray {
				return value.Value{}, fmt.Errorf("vm: ArraySpread target not array: %w", jserrors.ErrNotImplemented)
			}
			a := arr.AsArray()
			switch src.Type() {
			case value.TypeArray:
				sa := src.AsArray()
				for i := 0; i < sa.Length(); i++ {
					a.Push(sa.Get(i))
				}
			case value.TypeString:
				for _, r := range src.AsString() {
					a.Push(value.String(string(r)))
				}
			default:
				ex := value.MakeError("TypeError", "spread source is not iterable")
				newCur, handled := unwindTo(cur, &callStack, &valStack, ex)
				if !handled {
					return value.Value{}, &value.JSThrow{Val: ex}
				}
				cur = newCur
				continue
			}
		case bytecode.OpObjectSpread:
			src := pop()
			obj := valStack[len(valStack)-1]
			if obj.Type() != value.TypeObject {
				return value.Value{}, fmt.Errorf("vm: ObjectSpread target not object: %w", jserrors.ErrNotImplemented)
			}
			// Per spec, primitives are silently ignored (`{...1}` is
			// {}). Only object/array contribute keys.
			if src.Type() == value.TypeObject {
				to := obj.AsObject()
				so := src.AsObject()
				for _, name := range so.PropNames() {
					val, _ := so.GetOwn(name)
					to.Set(name, val)
				}
			}
		case bytecode.OpSwap:
			n := len(valStack)
			valStack[n-1], valStack[n-2] = valStack[n-2], valStack[n-1]
		case bytecode.OpNewApply:
			argsV := pop()
			fnVal := pop()
			if fnVal.Type() != value.TypeFunction {
				ex := value.MakeError("TypeError", "value is not a constructor")
				newCur, handled := unwindTo(cur, &callStack, &valStack, ex)
				if !handled {
					return value.Value{}, &value.JSThrow{Val: ex}
				}
				cur = newCur
				continue
			}
			if argsV.Type() != value.TypeArray {
				return value.Value{}, fmt.Errorf("vm: NewApply args not array: %w", jserrors.ErrNotImplemented)
			}
			fn := fnVal.AsFunction()
			srcArr := argsV.AsArray()
			n := srcArr.Length()
			args := make([]value.Value, n)
			for i := 0; i < n; i++ {
				args[i] = srcArr.Get(i)
			}
			newObj := value.NewObject()
			protoV := value.FunctionGetProp(fn, "prototype")
			if protoV.Type() == value.TypeObject {
				newObj.SetProto(protoV.AsObject())
			}
			thisVal := value.ObjectVal(newObj)
			ret, err := v.Call(fn, thisVal, args)
			if err != nil {
				if t, ok := err.(*value.JSThrow); ok {
					newCur, handled := unwindTo(cur, &callStack, &valStack, t.Val)
					if !handled {
						return value.Value{}, t
					}
					cur = newCur
					continue
				}
				return value.Value{}, err
			}
			if ret.Type() == value.TypeObject {
				push(ret)
			} else {
				push(thisVal)
			}
		case bytecode.OpCallApply:
			// stack: [fn, this, argsArr]
			argsV := pop()
			thisVal := pop()
			fnVal := pop()
			if fnVal.Type() != value.TypeFunction {
				ex := value.MakeError("TypeError", "value is not a function")
				newCur, handled := unwindTo(cur, &callStack, &valStack, ex)
				if !handled {
					return value.Value{}, &value.JSThrow{Val: ex}
				}
				cur = newCur
				continue
			}
			if argsV.Type() != value.TypeArray {
				return value.Value{}, fmt.Errorf("vm: CallApply args not array: %w", jserrors.ErrNotImplemented)
			}
			fn := fnVal.AsFunction()
			srcArr := argsV.AsArray()
			n := srcArr.Length()
			args := make([]value.Value, n)
			for i := 0; i < n; i++ {
				args[i] = srcArr.Get(i)
			}
			ret, err := v.Call(fn, thisVal, args)
			if err != nil {
				if t, ok := err.(*value.JSThrow); ok {
					newCur, handled := unwindTo(cur, &callStack, &valStack, t.Val)
					if !handled {
						return value.Value{}, t
					}
					cur = newCur
					continue
				}
				return value.Value{}, err
			}
			push(ret)
		case bytecode.OpGetByVal:
			key := pop()
			obj := pop()
			push(getByVal(obj, key))
		case bytecode.OpSetByVal:
			x := pop()
			key := pop()
			obj := pop()
			if err := setByVal(obj, key, x); err != nil {
				if t, ok := err.(*value.JSThrow); ok {
					newCur, handled := unwindTo(cur, &callStack, &valStack, t.Val)
					if !handled {
						return value.Value{}, t
					}
					cur = newCur
					continue
				}
				return value.Value{}, err
			}
			push(x)

		case bytecode.OpPop:
			valStack = valStack[:len(valStack)-1]
		case bytecode.OpReturn:
			var ret value.Value
			if len(valStack) > cur.valStackBase {
				ret = valStack[len(valStack)-1]
			} else {
				ret = value.Undefined()
			}
			// async function bodies hand their completion value to a
			// Promise wrapper before the caller sees it. Throws are
			// handled in the unwind path (and turn into rejected
			// promises in v.Call too).
			if cur.function != nil && cur.function.IsAsync {
				ret = value.MakePromiseFulfilled(ret)
			}
			// Recycle the returning frame's locals — but only when no
			// nested closure captured them by reference. The compiler
			// sets LocalsEscape=true in that case so we can leave the
			// slice for the GC to collect once the closures die.
			if cur.locals != nil && cur.function != nil && !cur.function.LocalsEscape {
				v.putLocals(cur.locals)
			}
			// Truncate any leftover working-stack values belonging to
			// the returning frame (e.g. a switch's discriminant when
			// the case body executes `return`). Without this the
			// caller sees a polluted stack and miscounts arguments.
			valStack = valStack[:cur.valStackBase]
			if len(callStack) == 0 {
				return ret, nil
			}
			cur = callStack[len(callStack)-1]
			callStack = callStack[:len(callStack)-1]
			push(ret)

		default:
			return value.Value{}, fmt.Errorf("vm: opcode %d at ip=%d: %w",
				op, cur.ip-1, jserrors.ErrNotImplemented)
		}
	}
	return value.Undefined(), nil
}
