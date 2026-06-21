// Package bytecode defines the VM's instruction set and the Chunk
// container produced by the compiler and consumed by the VM.
//
// The ISA grows one feature at a time. New opcodes get appended to
// the end of the const block — never inserted in the middle — so
// existing tests stay stable across changes.
package bytecode

import (
	"fmt"

	"github.com/vimt/goquickjs/internal/value"
)

// Op is a single-byte opcode.
type Op uint8

const (
	OpConstUndefined Op = iota
	OpConstNull
	OpConstTrue
	OpConstFalse
	OpConstK // u16 operand: index into Chunk.Constants

	OpAdd
	OpSub
	OpMul
	OpDiv
	OpMod
	OpNeg

	OpNot
	OpLt
	OpLe
	OpGt
	OpGe
	OpEq
	OpNeq
	OpStrictEq
	OpStrictNeq

	// Jumps take a signed i16 operand encoding the offset from the
	// instruction after the operand to the target instruction.
	OpJump
	// Peek variants leave the tested value on the stack — that's the
	// short-circuit semantics of && / || (the left value becomes the
	// expression's result when it short-circuits, so we can't pop it).
	OpJumpIfFalsePeek
	OpJumpIfTruePeek

	OpPop
	OpReturn

	// --- Step B ---

	// OpDup duplicates the top of the stack. Assignment and update
	// expressions use it to leave the new (or old) value on the
	// stack after the StoreLocal pops it.
	OpDup

	// Local variable slot access. Slots are pre-allocated by the
	// compiler; Chunk.MaxLocals records how big the VM's locals
	// array must be.
	OpLoadLocal  // u16 slot
	OpStoreLocal // u16 slot, pops

	// Popping jump variants used by if / for / while where the test
	// value should not survive the branch.
	OpJumpIfFalse
	OpJumpIfTrue

	// --- Objects (Step C-2) ---

	// OpNewObject pushes a fresh empty Object.
	OpNewObject

	// OpGetProp pops obj, pushes obj[name] (undefined if absent).
	// Operand: u16 index of the property name in Constants.
	OpGetProp

	// OpSetProp expects (obj, value) on the stack. It assigns
	// obj[name] = value, then leaves value on the stack so the
	// assignment expression evaluates to the new value.
	// Operand: u16 index of the property name in Constants.
	OpSetProp

	// --- Functions / globals (Step C-3) ---

	// OpLoadGlobal pushes globals[name] (undefined if absent).
	// Operand: u16 index of name in Constants.
	OpLoadGlobal

	// OpStoreGlobal pops and assigns globals[name] = value.
	// Operand: u16 index of name in Constants.
	OpStoreGlobal

	// OpCall pops (fn, arg0, ..., argN-1), invokes fn with `this`
	// bound to undefined, pushes the return value.
	// Operand: u8 number of arguments.
	OpCall

	// --- Arrays / method calls (Step C-4) ---

	// OpNewArray pushes a fresh empty array.
	OpNewArray

	// OpArrayPush pops a value and appends it to the array currently
	// on the top of the stack (the array stays on top). Used to
	// build array literals one element at a time.
	OpArrayPush

	// OpGetByVal expects (obj, key) on the stack; pops both and
	// pushes obj[key]. Handles array index, string char, object
	// string-key — the per-type semantics live in the VM.
	OpGetByVal

	// OpSetByVal expects (obj, key, value) on the stack; pops all
	// three, performs the write, then pushes value (so chained
	// assignment expressions work).
	OpSetByVal

	// OpCallMethod is OpCall's method-form. The stack layout is
	// (this, fn, arg0, ..., argN-1); fn is invoked with `this`
	// bound to the receiver. Operand: u8 number of arguments.
	OpCallMethod

	// --- Closures (Step C-5) ---

	// OpClosure instantiates a Function value from a proto Function
	// in the constants pool, binding its Upvalues from the creating
	// frame per the proto's UpvalueDescs.
	// Operand: u16 index of the proto Function in Constants.
	OpClosure

	// OpLoadUpvalue / OpStoreUpvalue read/write the currently
	// executing function's captured slot. Operand: u16 upvalue idx
	// (into cur.function.Upvalues).
	OpLoadUpvalue
	OpStoreUpvalue

	// OpLoadThis pushes the current frame's bound `this`. Top-level
	// `this` is undefined under our simplified model (no host environ).
	OpLoadThis

	// OpNew constructs a new object from a constructor function:
	// pops (fn, arg0, ..., argN-1); creates a fresh Object whose
	// [[Prototype]] is fn.prototype; invokes fn with this bound to
	// that new object; if the call returned an object, that's the
	// result, otherwise the new object is. Operand: u8 argCount.
	OpNew

	// OpThrow pops the value on top and throws it. The VM walks
	// the chunk's Handlers (and up the call stack if needed) to find
	// a covering catch; if none, propagates as *value.JSThrow.
	OpThrow

	// OpInstanceof pops (left, right); right must be a function;
	// pushes true iff right.prototype is in left's prototype chain.
	OpInstanceof

	// OpGetIterator pops an iterable and pushes an iterator object
	// (one with a `next()` method per the JS iterator protocol).
	// Arrays and strings have built-in dispatch; objects fall back
	// to looking for an `@@iterator` method (TODO).
	OpGetIterator

	// Bitwise ops. Operands coerced to int32 (uint32 for OpUShr's
	// right operand interpretation).
	OpBitAnd
	OpBitOr
	OpBitXor
	OpBitNot
	OpShl
	OpShr
	OpUShr

	// OpJumpIfNotNullishPeek leaves the value on the stack; jumps
	// when the value is neither null nor undefined. Used by `??`.
	OpJumpIfNotNullishPeek
	// OpJumpIfNullishPeek is the opposite — used by `?.`.
	OpJumpIfNullishPeek

	// OpTypeof replaces TOS with a string per JS typeof semantics:
	// "undefined" / "boolean" / "number" / "string" / "object" /
	// "function". Crucially never throws — even an unresolved global
	// returns "undefined" (handled at the typeof-ident shortcut in the
	// compiler).
	OpTypeof
	// OpVoid pops TOS (executing its side effects) and pushes undefined.
	OpVoid

	// OpDeleteProp pops obj, deletes own property named const[u16] from
	// it, pushes a boolean (true if the property existed, also true if
	// the target wasn't an object — matching JS leniency).
	OpDeleteProp
	// OpDeleteByVal pops (obj, key), deletes obj[String(key)], pushes
	// the same boolean result as OpDeleteProp.
	OpDeleteByVal

	// OpArraySpread pops src, iterates it (Array fast path; String
	// per-codeunit) and Push()es each element to the array sitting
	// below it on the stack. The array stays.
	OpArraySpread
	// OpObjectSpread pops src, copies each own enumerable string-keyed
	// property into the object sitting below it on the stack. The
	// object stays.
	OpObjectSpread
	// OpCallApply pops (argsArr, this, fn), invokes fn with `this` and
	// the array-unpacked args, pushes the return value. Used by call
	// expressions that contain a spread argument.
	OpCallApply
	// OpSwap exchanges TOS and TOS-1. Used when we need to reorder
	// two values without spilling to a slot.
	OpSwap

	// OpAwait pops a value; if it is a fulfilled Promise pushes its
	// resolved value, if rejected throws the rejection, if pending
	// throws (we don't have true coroutine suspension yet). Non-
	// Promise values are passed through unchanged.
	OpAwait

	// OpYield is the generator suspension point: pop the value to
	// yield, send it to the caller of .next(), then receive whatever
	// the next .next(val) hands back and push that. Only legal in a
	// frame whose function is IsGenerator (the VM panics otherwise).
	OpYield

	// OpForInKeys pops a value and pushes an Array of its own
	// enumerable string keys. Used by `for (k in obj)` after
	// lowering to an array-iteration loop.
	OpForInKeys

	// OpIn pops (key, obj); pushes a boolean reporting whether obj
	// has String(key) as an own or inherited property. Mirrors the
	// JS `in` operator.
	OpIn

	// OpDefineGetter (u16 nameIdx) pops the getter function and
	// attaches it to the object on top of the stack as the getter
	// for the given property name. Object stays. Setter, if any,
	// is preserved.
	OpDefineGetter
	// OpDefineSetter (u16 nameIdx) is the setter counterpart.
	OpDefineSetter

	// OpPow pops (rhs, lhs) and pushes lhs ** rhs.
	OpPow

	// OpNewApply pops (argsArr, ctorFn), invokes the constructor
	// with the unpacked args, pushes the resulting instance. Same
	// role for `new` that OpCallApply plays for function calls.
	OpNewApply
)

// Handler describes one try-catch region in a Chunk. A pc lies
// inside the region iff StartPC <= pc < EndPC; HandlerPC is the
// catch-clause's entry point, where the throw value is pushed onto
// the stack as the catch parameter.
type Handler struct {
	StartPC, EndPC int
	HandlerPC      int
}


// Chunk is one compiled program: a flat byte stream of opcodes +
// little-endian operands, a constants pool that OpConstK indexes
// into, the locals frame size the VM must pre-allocate, and the
// table of try-block handlers.
type Chunk struct {
	Code      []byte
	Constants []value.Value
	MaxLocals uint16
	Handlers  []Handler
	// PropCaches backs the inline caches for OpGetProp. Each OpGetProp
	// carries a u16 index into this slice (allocated by EmitGetProp).
	// One cell per site; populated lazily by the VM at run time.
	PropCaches []value.PropCache
	// SetCaches is the OpSetProp counterpart, indexed by EmitSetProp.
	SetCaches []value.SetCache
}

// AddHandler records a try-region. Called by the compiler after the
// catch arm has been laid down so HandlerPC is final.
func (c *Chunk) AddHandler(start, end, handlerPC int) {
	c.Handlers = append(c.Handlers, Handler{StartPC: start, EndPC: end, HandlerPC: handlerPC})
}

// Emit appends a zero-operand opcode.
func (c *Chunk) Emit(op Op) {
	c.Code = append(c.Code, byte(op))
}

// EmitU16 appends an opcode followed by a little-endian u16 operand.
func (c *Chunk) EmitU16(op Op, arg uint16) {
	c.Code = append(c.Code, byte(op), byte(arg), byte(arg>>8))
}

// EmitGetProp appends OpGetProp with two u16 operands: the name
// constant index and a freshly-allocated inline-cache index. Every
// OpGetProp must go through here so its IC cell exists.
func (c *Chunk) EmitGetProp(nameIdx uint16) {
	ic := uint16(len(c.PropCaches))
	c.PropCaches = append(c.PropCaches, value.PropCache{})
	c.Code = append(c.Code,
		byte(OpGetProp),
		byte(nameIdx), byte(nameIdx>>8),
		byte(ic), byte(ic>>8))
}

// EmitSetProp appends OpSetProp with two u16 operands: the name
// constant index and a freshly-allocated set-cache index.
func (c *Chunk) EmitSetProp(nameIdx uint16) {
	ic := uint16(len(c.SetCaches))
	c.SetCaches = append(c.SetCaches, value.SetCache{})
	c.Code = append(c.Code,
		byte(OpSetProp),
		byte(nameIdx), byte(nameIdx>>8),
		byte(ic), byte(ic>>8))
}

// EmitU8 appends an opcode followed by a single-byte operand.
func (c *Chunk) EmitU8(op Op, arg uint8) {
	c.Code = append(c.Code, byte(op), arg)
}

// AddConstant interns v and returns its index.
func (c *Chunk) AddConstant(v value.Value) uint16 {
	idx := uint16(len(c.Constants))
	c.Constants = append(c.Constants, v)
	return idx
}

// EmitJump appends op with a placeholder i16 operand and returns the
// byte offset of that placeholder so the caller can later PatchJump.
func (c *Chunk) EmitJump(op Op) int {
	c.Code = append(c.Code, byte(op), 0, 0)
	return len(c.Code) - 2
}

// PatchJump fills in a placeholder produced by EmitJump so the jump
// lands at the current end-of-code position.
func (c *Chunk) PatchJump(patchAt int) error {
	return c.PatchJumpTo(patchAt, len(c.Code))
}

// PatchJumpTo fills in a placeholder so that, when executed, the
// jump lands at the supplied target PC. Used by switch/loop bodies
// where the target is known to be a specific instruction that has
// not yet (or is no longer) at end-of-code.
func (c *Chunk) PatchJumpTo(patchAt, target int) error {
	nextIP := patchAt + 2
	rel := target - nextIP
	if rel < -32768 || rel > 32767 {
		return fmt.Errorf("bytecode: jump offset %d out of i16 range", rel)
	}
	u := uint16(int16(rel))
	c.Code[patchAt] = byte(u)
	c.Code[patchAt+1] = byte(u >> 8)
	return nil
}

// EmitBackJump emits a jump op whose i16 operand points back to the
// instruction at byte offset target (a label captured earlier, e.g.
// at the top of a loop).
func (c *Chunk) EmitBackJump(op Op, target int) error {
	nextIP := len(c.Code) + 3
	rel := target - nextIP
	if rel < -32768 || rel > 32767 {
		return fmt.Errorf("bytecode: back jump offset %d out of i16 range", rel)
	}
	u := uint16(int16(rel))
	c.Code = append(c.Code, byte(op), byte(u), byte(u>>8))
	return nil
}
