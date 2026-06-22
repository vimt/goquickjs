// Package compiler lowers the parser AST to a bytecode Chunk.
//
// Scope model:
//
//	The top-level (program) scope is the "global" scope. Bindings
//	declared at the top level live in the program's globals — a
//	name-keyed map looked up at runtime by OpLoadGlobal/OpStoreGlobal.
//	This is the only way nested functions can refer to top-level
//	names (including each other) without us implementing full
//	closure capture.
//
//	Function bodies open a new "local" frame. Bindings inside a
//	function use frame-local slots (OpLoadLocal/OpStoreLocal). When
//	a function body references a name not found in any of its own
//	lexical scopes, we ask the parent compiler — if the parent
//	resolves it as a global, we treat it as a global at this site
//	too. If the parent resolves it as one of its own locals, that's
//	a true closure capture, which we currently report as
//	ErrNotImplemented.
//
//	for-let's `let i` shares a single slot across all iterations
//	(not per-iteration fresh binding). Fine until a corpus needs a
//	closure that captures `i`.
//
// QuickJS-style completion: the last top-level statement's value is
// the program's completion; earlier statements have their values
// popped. An empty program completes with undefined.
package compiler

import (
	"fmt"

	"github.com/vimt/goquickjs/internal/bytecode"
	"github.com/vimt/goquickjs/internal/parser"
	"github.com/vimt/goquickjs/internal/value"
)

// symKind discriminates how a name resolves.
type symKind int

const (
	sLocal   symKind = iota // frame slot in the current function
	sGlobal                 // by-name lookup at runtime
	sUpvalue                // captured outer local/upvalue (closure)
)

type symRef struct {
	kind symKind
	slot uint16 // sLocal: frame slot; sUpvalue: index into upvalueDescs
}

type scope struct {
	locals    map[string]uint16
	startSlot uint16
}

// loopFrame tracks an enclosing loop or switch for break/continue
// statement resolution. catchContinue=false means a switch (break
// targets this, but continue passes through to an outer real loop).
// label, when non-empty, lets a `break LBL` / `continue LBL` target
// this frame specifically.
type loopFrame struct {
	breakJumps    []int
	continueJumps []int
	catchContinue bool
	label         string
	// stackItems is the count of operand-stack slots this construct
	// keeps live across its body (for-of/for-in keep their iterator,
	// switch keeps its discriminant). Inner try blocks add this to
	// Handler.Depth so unwinding doesn't drop those slots.
	stackItems int
}

type compiler struct {
	chunk    *bytecode.Chunk
	parent   *compiler
	scopes   []scope
	nextSlot uint16
	maxSlots uint16
	tmpCount int // monotonic, used by tempName() for unique slot keys

	// Upvalue table for closure capture. Populated during resolve()
	// whenever a child compiler reaches up through us to a local
	// further out. upvalues maps name → index into upvalueDescs;
	// upvalueDescs is what we hand to the produced Function so
	// OpClosure can wire up the runtime *Value pointers.
	upvalues     map[string]uint16
	upvalueDescs []value.UpvalueDesc

	// loops is a stack of enclosing loops / switches for break and
	// continue resolution. Innermost is at the end.
	loops []*loopFrame

	// pendingLabel is set by LabeledStmt before emitting its body
	// and consumed by the next loop / switch emit (which copies it
	// into its loopFrame and clears the slot).
	pendingLabel string

	// localsEscape is flipped to true when a nested function
	// captures one of our locals as an upvalue. The VM consults
	// the corresponding flag on the produced Function to decide
	// whether the locals slice can safely return to the pool on
	// OpReturn (it can't if a closure still holds *Value into it).
	localsEscape bool

	// usesArguments flips to true the first time the body
	// references the special `arguments` binding. We allocate the
	// per-call Array in the VM only when this is set, saving a
	// malloc + push loop per function call on the (very common)
	// path where `arguments` isn't read.
	usesArguments bool
}

func newCompiler(parent *compiler) *compiler {
	return &compiler{
		chunk:    &bytecode.Chunk{},
		parent:   parent,
		scopes:   []scope{{locals: map[string]uint16{}}},
		upvalues: map[string]uint16{},
	}
}

// Compile lowers a Program AST to a top-level Chunk.
func Compile(prog *parser.Program) (*bytecode.Chunk, error) {
	c := newCompiler(nil)
	if len(prog.Body) == 0 {
		c.chunk.Emit(bytecode.OpConstUndefined)
		c.chunk.Emit(bytecode.OpReturn)
		return c.chunk, nil
	}
	for i, stmt := range prog.Body {
		last := i == len(prog.Body)-1
		if err := c.emitStmt(stmt, last); err != nil {
			return nil, err
		}
	}
	c.chunk.Emit(bytecode.OpReturn)
	c.chunk.MaxLocals = c.maxSlots
	return c.chunk, nil
}

func (c *compiler) isTopLevel() bool { return c.parent == nil }

func (c *compiler) declare(name string) (symRef, error) {
	if c.isTopLevel() {
		// No slot needed at top level; bindings are global by name.
		return symRef{kind: sGlobal}, nil
	}
	sc := &c.scopes[len(c.scopes)-1]
	if _, exists := sc.locals[name]; exists {
		return symRef{}, fmt.Errorf("compiler: redeclaration of %q", name)
	}
	slot := c.nextSlot
	sc.locals[name] = slot
	c.nextSlot++
	if c.nextSlot > c.maxSlots {
		c.maxSlots = c.nextSlot
	}
	return symRef{kind: sLocal, slot: slot}, nil
}

func (c *compiler) resolve(name string) symRef {
	if name == "arguments" {
		c.usesArguments = true
	}
	// 1. Local in this function's lexical scopes.
	for i := len(c.scopes) - 1; i >= 0; i-- {
		if slot, ok := c.scopes[i].locals[name]; ok {
			return symRef{kind: sLocal, slot: slot}
		}
	}
	// 2. Already-allocated upvalue (cached so repeated refs share idx).
	if idx, ok := c.upvalues[name]; ok {
		return symRef{kind: sUpvalue, slot: idx}
	}
	// 3. Top-level falls back to globals; missing globals are
	//    undefined at runtime, not a compile error.
	if c.isTopLevel() {
		return symRef{kind: sGlobal}
	}
	// 4. Walk up. If the parent resolves it as local/upvalue, we
	//    capture it as an upvalue here; if as global, propagate.
	parentRef := c.parent.resolve(name)
	switch parentRef.kind {
	case sGlobal:
		return symRef{kind: sGlobal}
	case sLocal:
		// The parent's local is now captured by reference from us
		// (or some descendant we routed this through). Mark the
		// owning compiler so its produced Function refuses to pool
		// its locals slice — the captured *Value pointer must
		// outlive the frame.
		c.parent.localsEscape = true
		return symRef{kind: sUpvalue, slot: c.addUpvalue(name, parentRef.slot, true)}
	case sUpvalue:
		return symRef{kind: sUpvalue, slot: c.addUpvalue(name, parentRef.slot, false)}
	}
	return parentRef
}

// addUpvalue records that this function needs to capture an outer
// binding `name` and returns the upvalue index it'll live at. The
// (index, isLocal) pair describes WHERE in the parent frame to grab
// the *Value pointer at OpClosure time.
func (c *compiler) addUpvalue(name string, index uint16, isLocal bool) uint16 {
	if idx, ok := c.upvalues[name]; ok {
		return idx
	}
	idx := uint16(len(c.upvalueDescs))
	c.upvalueDescs = append(c.upvalueDescs, value.UpvalueDesc{Index: index, IsLocal: isLocal})
	c.upvalues[name] = idx
	return idx
}

func (c *compiler) enterScope() {
	c.scopes = append(c.scopes, scope{
		locals:    map[string]uint16{},
		startSlot: c.nextSlot,
	})
}

func (c *compiler) exitScope() {
	sc := c.scopes[len(c.scopes)-1]
	c.scopes = c.scopes[:len(c.scopes)-1]
	c.nextSlot = sc.startSlot
}

func (c *compiler) emitLoad(ref symRef, name string) error {
	switch ref.kind {
	case sLocal:
		c.chunk.EmitU16(bytecode.OpLoadLocal, ref.slot)
	case sGlobal:
		idx := c.chunk.AddConstant(value.String(name))
		c.chunk.EmitU16(bytecode.OpLoadGlobal, idx)
	case sUpvalue:
		c.chunk.EmitU16(bytecode.OpLoadUpvalue, ref.slot)
	}
	return nil
}

// tempName returns a unique synthetic identifier (with a leading
// space so it can never collide with a user-visible name). Used by
// destructuring to stash the source object/array into a hidden local
// the user's body can't accidentally reference.
func (c *compiler) tempName() string {
	c.tmpCount++
	return fmt.Sprintf(" tmp%d", c.tmpCount)
}

// emitLoadRef pushes the value at ref onto the stack. Mirror of
// emitStore's dispatch. Inlined here rather than added as a method
// because emitDestructure is the only multi-read caller today.
func (c *compiler) emitLoadRef(ref symRef, name string) {
	switch ref.kind {
	case sLocal:
		c.chunk.EmitU16(bytecode.OpLoadLocal, ref.slot)
	case sGlobal:
		idx := c.chunk.AddConstant(value.String(name))
		c.chunk.EmitU16(bytecode.OpLoadGlobal, idx)
	case sUpvalue:
		c.chunk.EmitU16(bytecode.OpLoadUpvalue, ref.slot)
	}
}

func (c *compiler) emitStore(ref symRef, name string) error {
	switch ref.kind {
	case sLocal:
		c.chunk.EmitU16(bytecode.OpStoreLocal, ref.slot)
	case sGlobal:
		idx := c.chunk.AddConstant(value.String(name))
		c.chunk.EmitU16(bytecode.OpStoreGlobal, idx)
	case sUpvalue:
		c.chunk.EmitU16(bytecode.OpStoreUpvalue, ref.slot)
	}
	return nil
}

var binaryOps = map[string]bytecode.Op{
	"+":          bytecode.OpAdd,
	"-":          bytecode.OpSub,
	"*":          bytecode.OpMul,
	"/":          bytecode.OpDiv,
	"%":          bytecode.OpMod,
	"**":         bytecode.OpPow,
	"&":          bytecode.OpBitAnd,
	"|":          bytecode.OpBitOr,
	"^":          bytecode.OpBitXor,
	"<<":         bytecode.OpShl,
	">>":         bytecode.OpShr,
	">>>":        bytecode.OpUShr,
	"instanceof": bytecode.OpInstanceof,
	"in":         bytecode.OpIn,
	"<":   bytecode.OpLt,
	"<=":  bytecode.OpLe,
	">":   bytecode.OpGt,
	">=":  bytecode.OpGe,
	"==":  bytecode.OpEq,
	"!=":  bytecode.OpNeq,
	"===": bytecode.OpStrictEq,
	"!==": bytecode.OpStrictNeq,
}
