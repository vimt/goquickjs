// Promise — single-flight A+ style with a minimal microtask backend
// supplied by the VM. Supports the common surface that user code
// reaches for: Promise.resolve/reject, .then/.catch/.finally,
// Promise.all/race. Reactions run by enqueuing onto the VM's
// microtask queue, which drainMicrotasks runs after the top-level
// Eval (and after every nested Call re-entry through the same VM).
package builtins

import (
	"fmt"
	"os"

	"github.com/vimt/goquickjs/internal/value"
)

// promiseSlots is the internal-slots companion map keyed by the
// wrapper *Object. We don't expose state/value via real properties
// so user code can't trivially tamper with them.
type promiseSlots struct {
	state      string // "pending" | "fulfilled" | "rejected"
	value      value.Value
	onFulfill  []func()
	onReject   []func()
	// handled flips to true once any .then(_, onR) / .catch / .finally
	// attaches a rejection handler (directly or transitively). Used
	// by reportUnhandledRejections to spot promises that finish in
	// the rejected state without anybody having taken responsibility.
	handled bool
}

var promiseStorage = map[*value.Object]*promiseSlots{}

// promisePrototype is set in installPromise; every fresh promise's
// proto points here so `.then` / `.catch` / `.finally` resolve via
// the chain rather than needing to be re-attached per instance.
var promisePrototype *value.Object

func newPromise() (*value.Object, *promiseSlots) {
	o := value.NewObject()
	if promisePrototype != nil {
		o.SetProto(promisePrototype)
	}
	s := &promiseSlots{state: "pending"}
	promiseStorage[o] = s
	return o, s
}

func slotsOf(v value.Value) *promiseSlots {
	if v.Type() != value.TypeObject {
		return nil
	}
	return promiseStorage[v.AsObject()]
}

// resolvePromise transitions p to fulfilled with v (no-op if already
// settled). Spec actually does the resolution-procedure dance to
// chain through nested thenables; we handle the simple Promise-vs-
// non-Promise case (good enough for our corpus).
func resolvePromise(caller value.Caller, p *value.Object, s *promiseSlots, v value.Value) {
	if s.state != "pending" {
		return
	}
	// Thenable chaining: if v is itself a Promise, wait on it.
	if vs := slotsOf(v); vs != nil {
		thenOnSettled(caller, v.AsObject(), vs,
			func(x value.Value) {
				resolvePromise(caller, p, s, x)
			},
			func(x value.Value) {
				rejectPromise(caller, p, s, x)
			})
		return
	}
	s.state = "fulfilled"
	s.value = v
	for _, cb := range s.onFulfill {
		caller.EnqueueMicrotask(cb)
	}
	s.onFulfill = nil
	s.onReject = nil
}

func rejectPromise(caller value.Caller, p *value.Object, s *promiseSlots, v value.Value) {
	if s.state != "pending" {
		return
	}
	s.state = "rejected"
	s.value = v
	for _, cb := range s.onReject {
		caller.EnqueueMicrotask(cb)
	}
	s.onFulfill = nil
	s.onReject = nil
}

// thenOnSettled wires (onF, onR) to run when p settles. If p is
// already settled they're queued immediately; otherwise stashed on
// the slot's callback lists.
func thenOnSettled(caller value.Caller, p *value.Object, s *promiseSlots, onF, onR func(value.Value)) {
	wrapF := func() { onF(s.value) }
	wrapR := func() { onR(s.value) }
	switch s.state {
	case "fulfilled":
		caller.EnqueueMicrotask(wrapF)
	case "rejected":
		caller.EnqueueMicrotask(wrapR)
	default:
		s.onFulfill = append(s.onFulfill, wrapF)
		s.onReject = append(s.onReject, wrapR)
	}
}

// init wires the value-package promise hooks so the VM can wrap
// async-function results without importing builtins.
func init() {
	value.MakePromiseFulfilled = func(v value.Value) value.Value {
		// If v is already a promise, return as-is.
		if slotsOf(v) != nil {
			return v
		}
		p, s := newPromise()
		s.state = "fulfilled"
		s.value = v
		return value.ObjectVal(p)
	}
	value.MakePromiseRejected = func(v value.Value) value.Value {
		p, s := newPromise()
		s.state = "rejected"
		s.value = v
		return value.ObjectVal(p)
	}
	value.PromiseUnwrap = func(v value.Value) (value.Value, string) {
		s := slotsOf(v)
		if s == nil {
			return v, "fulfilled"
		}
		return s.value, s.state
	}
	value.NewPendingPromise = func(c value.Caller) (value.Value, func(value.Value), func(value.Value)) {
		p, s := newPromise()
		pv := value.ObjectVal(p)
		s.handled = true
		resolve := func(x value.Value) { resolvePromise(c, p, s, x) }
		reject := func(x value.Value) { rejectPromise(c, p, s, x) }
		return pv, resolve, reject
	}
	value.PromiseSubscribe = func(c value.Caller, p value.Value, onF, onR func(value.Value)) {
		s := slotsOf(p)
		if s == nil {
			onF(p)
			return
		}
		thenOnSettled(c, p.AsObject(), s, onF, onR)
	}
	value.ReportUnhandledRejections = func() {
		for p, s := range promiseStorage {
			if s.state == "rejected" && !s.handled {
				fmt.Fprintf(os.Stderr, "goquickjs: unhandled promise rejection: %s\n", s.value.String())
				s.handled = true // suppress duplicate reports
				_ = p
			}
		}
	}
}

func installPromise(globals map[string]value.Value) {
	// Constructor: new Promise((resolve, reject) => { ... })
	ctorFn := &value.Function{Name: "Promise", Arity: 1, Native: promiseConstruct}
	ctorFn.Props = value.NewBareObject()
	proto := value.NewObject()
	proto.Set("then", nativeFn("then", 2, promiseThen))
	proto.Set("catch", nativeFn("catch", 1, promiseCatch))
	proto.Set("finally", nativeFn("finally", 1, promiseFinally))
	promisePrototype = proto
	ctorFn.Props.Set("prototype", value.ObjectVal(proto))
	ctorFn.Props.Set("resolve", nativeFn("resolve", 1, promiseStaticResolve))
	ctorFn.Props.Set("reject", nativeFn("reject", 1, promiseStaticReject))
	ctorFn.Props.Set("all", nativeFn("all", 1, promiseAll))
	ctorFn.Props.Set("race", nativeFn("race", 1, promiseRace))
	globals["Promise"] = value.FunctionVal(ctorFn)

	// queueMicrotask global per HTML spec, useful for tests.
	globals["queueMicrotask"] = nativeFn("queueMicrotask", 1, queueMicrotask)
}

func promiseConstruct(caller value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	executor := argOrUndef(args, 0)
	if executor.Type() != value.TypeFunction {
		return value.Value{}, &value.JSThrow{Val: makeError("TypeError", "Promise: executor must be a function")}
	}
	var p *value.Object
	var s *promiseSlots
	if this.Type() == value.TypeObject {
		p = this.AsObject()
		if promisePrototype != nil && p.Proto() == nil {
			p.SetProto(promisePrototype)
		}
		s = &promiseSlots{state: "pending"}
		promiseStorage[p] = s
	} else {
		p, s = newPromise()
	}
	resolve := nativeFn("resolve", 1, func(_ value.Caller, _ value.Value, a []value.Value) (value.Value, error) {
		resolvePromise(caller, p, s, argOrUndef(a, 0))
		return value.Undefined(), nil
	})
	reject := nativeFn("reject", 1, func(_ value.Caller, _ value.Value, a []value.Value) (value.Value, error) {
		rejectPromise(caller, p, s, argOrUndef(a, 0))
		return value.Undefined(), nil
	})
	// Spec: executor is invoked synchronously; if it throws, the
	// promise is rejected with the throw value.
	_, err := caller.Call(executor.AsFunction(), value.Undefined(), []value.Value{resolve, reject})
	if err != nil {
		if t, ok := err.(*value.JSThrow); ok {
			rejectPromise(caller, p, s, t.Val)
		} else {
			rejectPromise(caller, p, s, value.String(err.Error()))
		}
	}
	return value.ObjectVal(p), nil
}

func promiseStaticResolve(caller value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	v := argOrUndef(args, 0)
	// If v is already a Promise, return it as-is.
	if vs := slotsOf(v); vs != nil {
		return v, nil
	}
	p, s := newPromise()
	resolvePromise(caller, p, s, v)
	return value.ObjectVal(p), nil
}

func promiseStaticReject(caller value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	p, s := newPromise()
	rejectPromise(caller, p, s, argOrUndef(args, 0))
	return value.ObjectVal(p), nil
}

func promiseThen(caller value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	if this.Type() != value.TypeObject {
		return value.Value{}, badThis("Promise.prototype.then", "Promise")
	}
	s := slotsOf(this)
	if s == nil {
		return value.Value{}, badThis("Promise.prototype.then", "Promise")
	}
	onF := argOrUndef(args, 0)
	onR := argOrUndef(args, 1)
	if onR.Type() == value.TypeFunction {
		s.handled = true
	}
	p2, s2 := newPromise()
	onFulfilled := func(x value.Value) {
		if onF.Type() == value.TypeFunction {
			out, err := caller.Call(onF.AsFunction(), value.Undefined(), []value.Value{x})
			if err != nil {
				if t, ok := err.(*value.JSThrow); ok {
					rejectPromise(caller, p2, s2, t.Val)
				} else {
					rejectPromise(caller, p2, s2, value.String(err.Error()))
				}
				return
			}
			resolvePromise(caller, p2, s2, out)
		} else {
			// Identity pass-through: forward value.
			resolvePromise(caller, p2, s2, x)
		}
	}
	onRejected := func(x value.Value) {
		if onR.Type() == value.TypeFunction {
			out, err := caller.Call(onR.AsFunction(), value.Undefined(), []value.Value{x})
			if err != nil {
				if t, ok := err.(*value.JSThrow); ok {
					rejectPromise(caller, p2, s2, t.Val)
				} else {
					rejectPromise(caller, p2, s2, value.String(err.Error()))
				}
				return
			}
			resolvePromise(caller, p2, s2, out)
		} else {
			// No handler → propagate rejection.
			rejectPromise(caller, p2, s2, x)
		}
	}
	thenOnSettled(caller, this.AsObject(), s, onFulfilled, onRejected)
	return value.ObjectVal(p2), nil
}

func promiseCatch(caller value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	return promiseThen(caller, this, []value.Value{value.Undefined(), argOrUndef(args, 0)})
}

func promiseFinally(caller value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	cb := argOrUndef(args, 0)
	wrap := func(_ value.Caller, _ value.Value, a []value.Value) (value.Value, error) {
		if cb.Type() == value.TypeFunction {
			if _, err := caller.Call(cb.AsFunction(), value.Undefined(), nil); err != nil {
				return value.Value{}, err
			}
		}
		return argOrUndef(a, 0), nil
	}
	throwWrap := func(_ value.Caller, _ value.Value, a []value.Value) (value.Value, error) {
		if cb.Type() == value.TypeFunction {
			if _, err := caller.Call(cb.AsFunction(), value.Undefined(), nil); err != nil {
				return value.Value{}, err
			}
		}
		return value.Value{}, &value.JSThrow{Val: argOrUndef(a, 0)}
	}
	return promiseThen(caller, this, []value.Value{
		nativeFn("", 1, wrap),
		nativeFn("", 1, throwWrap),
	})
}

func promiseAll(caller value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	src := argOrUndef(args, 0)
	if src.Type() != value.TypeArray {
		return value.Value{}, &value.JSThrow{Val: makeError("TypeError", "Promise.all: expected an array")}
	}
	arr := src.AsArray()
	n := arr.Length()
	p, s := newPromise()
	if n == 0 {
		resolvePromise(caller, p, s, value.ArrayVal(value.NewArray()))
		return value.ObjectVal(p), nil
	}
	results := make([]value.Value, n)
	remaining := n
	for i := 0; i < n; i++ {
		item := arr.Get(i)
		var itemP value.Value
		if ps := slotsOf(item); ps != nil {
			itemP = item
		} else {
			pp, ps := newPromise()
			resolvePromise(caller, pp, ps, item)
			itemP = value.ObjectVal(pp)
		}
		idx := i
		thenOnSettled(caller, itemP.AsObject(), slotsOf(itemP),
			func(x value.Value) {
				results[idx] = x
				remaining--
				if remaining == 0 {
					out := value.NewArray()
					for _, r := range results {
						out.Push(r)
					}
					resolvePromise(caller, p, s, value.ArrayVal(out))
				}
			},
			func(x value.Value) {
				rejectPromise(caller, p, s, x)
			})
	}
	return value.ObjectVal(p), nil
}

func promiseRace(caller value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	src := argOrUndef(args, 0)
	if src.Type() != value.TypeArray {
		return value.Value{}, &value.JSThrow{Val: makeError("TypeError", "Promise.race: expected an array")}
	}
	arr := src.AsArray()
	p, s := newPromise()
	for i := 0; i < arr.Length(); i++ {
		item := arr.Get(i)
		var itemP value.Value
		if ps := slotsOf(item); ps != nil {
			itemP = item
		} else {
			pp, ps := newPromise()
			resolvePromise(caller, pp, ps, item)
			itemP = value.ObjectVal(pp)
		}
		thenOnSettled(caller, itemP.AsObject(), slotsOf(itemP),
			func(x value.Value) { resolvePromise(caller, p, s, x) },
			func(x value.Value) { rejectPromise(caller, p, s, x) })
	}
	return value.ObjectVal(p), nil
}

func queueMicrotask(caller value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	cb := argOrUndef(args, 0)
	if cb.Type() != value.TypeFunction {
		return value.Value{}, &value.JSThrow{Val: makeError("TypeError", "queueMicrotask: expected a function")}
	}
	fn := cb.AsFunction()
	caller.EnqueueMicrotask(func() {
		caller.Call(fn, value.Undefined(), nil)
	})
	return value.Undefined(), nil
}
