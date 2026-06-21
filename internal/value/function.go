package value

import "unsafe"

// NativeFn is the signature for Go-implemented JS functions.
//   - caller: a Caller the native can use to re-invoke any Function
//     (JS or native) — required for callback-taking methods like
//     Array.prototype.map. Pass it the receiver, args, and you get
//     the completion value or an error.
//   - this: the bound receiver (Undefined for plain calls, the
//     receiver object for method calls).
//   - args: caller-owned slice; do not retain it, the VM may reuse
//     the backing array between calls.
//
// The error surfaces as a Go error from Eval until we add try/catch.
type NativeFn func(caller Caller, this Value, args []Value) (Value, error)

// Caller lets a NativeFn invoke any Function — JS or native — to
// support callback methods (map, filter, sort comparators, etc.).
// The VM implements it. The native fn does not need to know whether
// the target is JS or native; Caller hides that.
//
// EnqueueMicrotask is the hook Promise builtins use to defer a
// callback to the end-of-tick microtask queue. The VM drains the
// queue after the program's main expression returns, mirroring how
// real engines run Promise reactions.
type Caller interface {
	Call(fn *Function, this Value, args []Value) (Value, error)
	EnqueueMicrotask(task func())
}

// UpvalueDesc tells OpClosure how to wire one slot of a new Function
// instance's Upvalues array:
//   - IsLocal=true: take &creator-frame.locals[Index] (a fresh capture
//     of an enclosing frame's local).
//   - IsLocal=false: copy creator-frame.function.Upvalues[Index] (chain
//     through the enclosing function's own upvalue, for deeper nesting).
//
// Because Upvalues are *Value pointers shared across all closures that
// captured the same local, mutation through any of them is visible to
// every holder — that's the by-reference closure semantics JS expects.
type UpvalueDesc struct {
	Index   uint16
	IsLocal bool
}

// Function is the runtime form of any callable value:
//   - JS function: Body points at a bytecode.Chunk; UpvalueDescs is
//     the "template" set when compiled; Upvalues is filled at OpClosure
//     time per instance so two closures from the same proto can capture
//     different enclosing frames.
//   - Go function: Native is non-nil; Body, Upvalues, UpvalueDescs
//     all unused.
//
// Body is an unsafe.Pointer to bytecode.Chunk because bytecode already
// imports value (for the constants pool) — a reverse import would form
// a cycle. The VM type-asserts it back at call time.
type Function struct {
	Name         string
	Arity        int
	Body         unsafe.Pointer
	Native       NativeFn
	UpvalueDescs []UpvalueDesc
	Upvalues     []*Value

	IsArrow   bool
	BoundThis Value

	HasRest      bool
	IsAsync      bool
	IsGenerator  bool
	HasArguments bool // non-arrow fn — VM injects an Array at ArgumentsSlot
	ArgumentsSlot uint16

	Props *Object
}

// FunctionGetProp resolves a property on a function value. Own props
// (Function.Props) take priority over Function.prototype methods.
// Reading `.prototype` on a JS constructor function lazily creates a
// fresh prototype object (and stashes it) so the canonical pattern
// `Foo.prototype.method = function(){}` works without an explicit
// initializer.
func FunctionGetProp(fn *Function, name string) Value {
	if fn.Props != nil {
		if v, ok := fn.Props.GetOwn(name); ok {
			return v
		}
	}
	if name == "prototype" && fn.Native == nil && !fn.IsArrow {
		if fn.Props == nil {
			fn.Props = NewObject()
		}
		proto := NewObject()
		fn.Props.Set("prototype", ObjectVal(proto))
		return ObjectVal(proto)
	}
	if proto, ok := FunctionProto[name]; ok {
		return FunctionVal(proto)
	}
	return Undefined()
}

// FunctionSetProp stores a property on a function value, lazily
// allocating Props. Used by user code like `Foo.prototype = ...` or
// `Foo.staticThing = ...`.
func FunctionSetProp(fn *Function, name string, v Value) {
	if fn.Props == nil {
		fn.Props = NewObject()
	}
	fn.Props.Set(name, v)
}

// FunctionProto is Function.prototype's method table (call / apply /
// bind). Populated by the builtins package at init.
var FunctionProto = map[string]*Function{}

// FunctionProp dispatches a property on a function primitive.
func FunctionProp(name string) Value {
	if fn, ok := FunctionProto[name]; ok {
		return FunctionVal(fn)
	}
	return Undefined()
}
