package vm

import (
	"fmt"
	"math"
	"math/big"

	"github.com/vimt/goquickjs/internal/bytecode"
	"github.com/vimt/goquickjs/internal/jserrors"
	"github.com/vimt/goquickjs/internal/value"
)

// Aliases so the inline jsMod helper doesn't have to import math
// inside its tiny body (kept here for grep-ability).
var (
	mathNaN = math.NaN
	mathMod = math.Mod
)

// doCall dispatches a call into either a native function (inline, no
// frame push) or a JS function (push a frame, return the new cur
// frame to the dispatch loop). consumeFrom is where the call's stack
// reservation begins (fnIdx for OpCall, thisIdx for OpCallMethod);
// everything from there up is consumed before the result is pushed.
func (v *VM) doCall(
	cur *frame,
	callStack *[]frame,
	valStack *[]value.Value,
	fn *value.Function,
	thisVal value.Value,
	args []value.Value,
	argCount int,
	consumeFrom int,
) (frame, error) {
	if fn.Native != nil {
		argsCopy := make([]value.Value, argCount)
		copy(argsCopy, args)
		*valStack = (*valStack)[:consumeFrom]
		result, err := fn.Native(v, thisVal, argsCopy)
		if err != nil {
			return *cur, err
		}
		*valStack = append(*valStack, result)
		return *cur, nil
	}

	// Generator: don't push a body frame — synthesize a generator
	// object whose .next() starts the body on demand. Same logic as
	// v.Call, mirrored here so OpCall / OpCallMethod stay consistent.
	if fn.IsGenerator {
		argsCopy := make([]value.Value, argCount)
		copy(argsCopy, args)
		*valStack = (*valStack)[:consumeFrom]
		gen := v.makeGenerator(fn, thisVal, argsCopy)
		*valStack = append(*valStack, value.ObjectVal(gen))
		return *cur, nil
	}

	// JS function: push current frame, switch to callee.
	fnChunk := (*bytecode.Chunk)(fn.Body)
	newLocals := v.getLocals(fnChunk.MaxLocals)
	n := argCount
	if n > fn.Arity {
		n = fn.Arity
	}
	for i := 0; i < n; i++ {
		newLocals[i] = args[i]
	}
	// Collect the extra args into the rest slot (declared right after
	// the last fixed param). Always create the array so the body can
	// rely on it being an Array, even when no extras were passed.
	if fn.HasRest {
		rest := value.NewArray()
		for i := fn.Arity; i < argCount; i++ {
			rest.Push(args[i])
		}
		newLocals[fn.Arity] = value.ArrayVal(rest)
	}
	if fn.HasArguments && int(fn.ArgumentsSlot) < int(fnChunk.MaxLocals) {
		argsArr := value.NewArray()
		for i := 0; i < argCount; i++ {
			argsArr.Push(args[i])
		}
		newLocals[fn.ArgumentsSlot] = value.ArrayVal(argsArr)
	}
	// Arrow functions inherit the surrounding `this` from creation
	// time rather than the call site.
	calleeThis := thisVal
	if fn.IsArrow {
		calleeThis = fn.BoundThis
	}
	// Cap the JS call stack so unbounded recursion surfaces as a
	// catchable RangeError instead of blowing the Go stack.
	const maxCallDepth = 500
	if len(*callStack) >= maxCallDepth {
		return *cur, &value.JSThrow{Val: value.MakeError("RangeError", "Maximum call stack size exceeded")}
	}
	*callStack = append(*callStack, *cur)
	*valStack = (*valStack)[:consumeFrom]
	return frame{
		chunk:        fnChunk,
		code:         fnChunk.Code,
		ip:           0,
		locals:       newLocals,
		function:     fn,
		thisVal:      calleeThis,
		valStackBase: consumeFrom,
	}, nil
}

// bigArith dispatches a binary BigInt operation by name. Inputs are
// pre-validated to be TypeBigInt. Returns a JS-side error (which the
// dispatch loop will route through the unwind path) for division by
// zero and negative-exponent cases.
func bigArith(op string, l, r value.Value) (value.Value, error) {
	a := l.AsBigInt().I
	b := r.AsBigInt().I
	out := new(big.Int)
	switch op {
	case "add":
		out.Add(a, b)
	case "sub":
		out.Sub(a, b)
	case "mul":
		out.Mul(a, b)
	case "div":
		if b.Sign() == 0 {
			return value.Value{}, &value.JSThrow{Val: value.MakeError("RangeError", "BigInt division by zero")}
		}
		out.Quo(a, b)
	case "mod":
		if b.Sign() == 0 {
			return value.Value{}, &value.JSThrow{Val: value.MakeError("RangeError", "BigInt division by zero")}
		}
		out.Rem(a, b)
	case "pow":
		if b.Sign() < 0 {
			return value.Value{}, &value.JSThrow{Val: value.MakeError("RangeError", "BigInt negative exponent")}
		}
		out.Exp(a, b, nil)
	}
	return value.BigIntVal(&value.BigInt{I: out}), nil
}

// keyString turns a property key Value into the string we actually
// store under. Symbol-typed keys collapse to their description so
// `obj[Symbol.iterator]` and OpGetIterator can read the same slot
// without us modelling separate symbol-keyed maps.
func keyString(v value.Value) string {
	if v.Type() == value.TypeSymbol {
		return v.AsSymbol().Description
	}
	return v.String()
}

// stringAsIndex parses s as a non-negative decimal integer. Returns
// (n, true) when s is exactly the canonical decimal form (no leading
// zeros except "0" itself, no sign, all digits). False if anything
// else — keeps the indexed fast path away from non-index string
// keys like "length", "foo", " 1", "01".
func stringAsIndex(s string) (int, bool) {
	if len(s) == 0 {
		return 0, false
	}
	if s == "0" {
		return 0, true
	}
	if s[0] < '1' || s[0] > '9' {
		return 0, false
	}
	n := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	return n, true
}

// jsInstanceof implements the `left instanceof fn` chain walk:
// pulls fn.prototype off the function's own props, then walks the
// left side's [[Prototype]] chain looking for it. Returns false if
// either side doesn't supply what's needed (instead of erroring; the
// dispatch site has already validated fn is a Function).
// isConstructorReturn reports whether a value returned from a
// constructor body should replace the freshly-created `this`. Per spec
// "Object" here means reference type — Array and Function qualify too.
func isConstructorReturn(v value.Value) bool {
	switch v.Type() {
	case value.TypeObject, value.TypeArray, value.TypeFunction:
		return true
	}
	return false
}

func jsInstanceof(left value.Value, fn *value.Function) bool {
	// Built-in kind matching: Array, Function, Object, RegExp, Date,
	// Error don't share an Object [[Prototype]] slot with the
	// constructor's `prototype`. Match by the constructor's name so
	// `[] instanceof Array` etc. give the spec answer without
	// requiring a per-type proto rewire.
	switch fn.Name {
	case "Array":
		return left.Type() == value.TypeArray
	case "Function":
		return left.Type() == value.TypeFunction
	case "Object":
		// `x instanceof Object` is true for any non-null/undefined
		// reference type — arrays, functions, objects all qualify.
		t := left.Type()
		return t == value.TypeObject || t == value.TypeArray || t == value.TypeFunction
	}
	if fn.Props == nil {
		return false
	}
	protoV, ok := fn.Props.GetOwn("prototype")
	if !ok || protoV.Type() != value.TypeObject {
		return false
	}
	target := protoV.AsObject()
	if left.Type() != value.TypeObject {
		return false
	}
	for p := left.AsObject().Proto(); p != nil; p = p.Proto() {
		if p == target {
			return true
		}
	}
	return false
}

// unwindTo searches for a catch handler covering cur.ip-1; if not
// found, pops frames from callStack and retries until either a
// handler is found (returns (newCur, true)) or the call stack is
// exhausted (returns (cur, false), with valStack truncated to the
// top frame's base so the caller can propagate). When a handler is
// found, valStack is truncated to the handling frame's base and the
// exception value is pushed for the catch param to store.
func unwindTo(cur frame, callStack *[]frame, valStack *[]value.Value, ex value.Value) (frame, bool) {
	for {
		if h := findHandler(cur.chunk.Handlers, cur.ip-1); h != nil {
			cur.ip = h.HandlerPC
			// Truncate to the operand-stack depth recorded at try-entry
			// (relative to frame base), preserving anything an outer
			// construct kept on the stack (for-of iterator, switch
			// discriminant, ...). Then push the exception as the
			// catch parameter.
			*valStack = (*valStack)[:cur.valStackBase+h.Depth]
			*valStack = append(*valStack, ex)
			return cur, true
		}
		if len(*callStack) == 0 {
			return cur, false
		}
		last := len(*callStack) - 1
		cur = (*callStack)[last]
		*callStack = (*callStack)[:last]
	}
}

// findHandler picks the innermost handler whose range covers pc.
// Compile order is innermost-first (nested try-blocks finish their
// AddHandler call before their enclosing try does), so a plain
// forward scan returns the innermost match first — exactly what
// JS semantics requires.
func findHandler(handlers []bytecode.Handler, pc int) *bytecode.Handler {
	for i := range handlers {
		h := &handlers[i]
		if pc >= h.StartPC && pc < h.EndPC {
			return h
		}
	}
	return nil
}

// getProp resolves the dotted access `obj.name` for any value kind
// we know about. Anything unknown returns undefined (matches JS).
func getProp(obj value.Value, name string) value.Value {
	switch obj.Type() {
	case value.TypeObject:
		v, _ := obj.AsObject().Get(name)
		return v
	case value.TypeArray:
		return obj.AsArray().Prop(name)
	case value.TypeString:
		return value.StringProp(obj.AsString(), name)
	case value.TypeNumber:
		return value.NumberProp(name)
	case value.TypeFunction:
		return value.FunctionGetProp(obj.AsFunction(), name)
	case value.TypeSymbol:
		if name == "description" {
			return value.String(obj.AsSymbol().Description)
		}
	}
	return value.Undefined()
}

// getByVal resolves the bracketed access `obj[key]`. Number keys on
// arrays/strings take the indexed fast path; everything else falls
// back to string-keyed property access.
func getByVal(obj, key value.Value) value.Value {
	switch obj.Type() {
	case value.TypeArray:
		if key.Type() == value.TypeNumber {
			return obj.AsArray().Get(int(key.AsNumber()))
		}
		ks := keyString(key)
		// String-typed numeric key ("0", "1", ...): JS treats these
		// as indices on Arrays. Needed for `for-in` (which yields
		// stringified indices) and code that builds keys dynamically.
		if i, ok := stringAsIndex(ks); ok {
			return obj.AsArray().Get(i)
		}
		return obj.AsArray().Prop(ks)
	case value.TypeString:
		s := obj.AsString()
		if key.Type() == value.TypeNumber {
			i := int(key.AsNumber())
			if i < 0 || i >= len(s) {
				return value.Undefined()
			}
			return value.String(string(s[i]))
		}
		ks := keyString(key)
		if i, ok := stringAsIndex(ks); ok {
			if i < 0 || i >= len(s) {
				return value.Undefined()
			}
			return value.String(string(s[i]))
		}
		return value.StringProp(s, ks)
	case value.TypeObject:
		o := obj.AsObject()
		// TypedArray indexed read.
		if o.IndexedRead != nil {
			if key.Type() == value.TypeNumber {
				if v, ok := o.IndexedRead(int(key.AsNumber())); ok {
					return v
				}
			} else {
				if i, ok := stringAsIndex(keyString(key)); ok {
					if v, ok2 := o.IndexedRead(i); ok2 {
						return v
					}
				}
			}
		}
		v, _ := o.Get(keyString(key))
		return v
	}
	return value.Undefined()
}

func setByVal(obj, key, v value.Value) error {
	switch obj.Type() {
	case value.TypeArray:
		if key.Type() == value.TypeNumber {
			obj.AsArray().Set(int(key.AsNumber()), v)
			return nil
		}
		ks := keyString(key)
		if i, ok := stringAsIndex(ks); ok {
			obj.AsArray().Set(i, v)
			return nil
		}
		return fmt.Errorf("vm: array string-key write: %w", jserrors.ErrNotImplemented)
	case value.TypeObject:
		o := obj.AsObject()
		// TypedArray fast path: numeric index goes through the
		// IndexedWrite hook so the element-kind truncation rules
		// (Uint8 → byte, Int32 → int32) actually apply.
		if o.IndexedWrite != nil {
			if key.Type() == value.TypeNumber {
				if o.IndexedWrite(int(key.AsNumber()), v) {
					return nil
				}
			} else {
				if i, ok := stringAsIndex(keyString(key)); ok {
					if o.IndexedWrite(i, v) {
						return nil
					}
				}
			}
		}
		o.Set(keyString(key), v)
		return nil
	case value.TypeFunction:
		value.FunctionSetProp(obj.AsFunction(), keyString(key), v)
		return nil
	}
	// null/undefined throw TypeError; other primitives are sloppy-mode
	// silent no-ops (the wrapper is discarded). Mirrors OpSetProp.
	switch obj.Type() {
	case value.TypeNull:
		return &value.JSThrow{Val: value.MakeError("TypeError",
			"Cannot set property '"+keyString(key)+"' of null")}
	case value.TypeUndefined:
		return &value.JSThrow{Val: value.MakeError("TypeError",
			"Cannot set property '"+keyString(key)+"' of undefined")}
	}
	return nil
}

// jsModFast hot-path % for the common all-integer case (which `%`
// in spec is allowed to fall back to native integer % on). Floats
// or out-of-int64-range values fall through to the slower jsMod.
//
//go:inline
func jsModFast(a, b float64) float64 {
	if b == 0 {
		return mathNaN()
	}
	// IEEE 754 doubles round-trip exactly through int64 for any
	// integer in (-2^53, 2^53). That's far wider than the typical
	// modulus arithmetic users write, so we can avoid math.Mod's
	// frexp dance.
	ai, bi := int64(a), int64(b)
	if float64(ai) == a && float64(bi) == b {
		return float64(ai % bi)
	}
	return mathMod(a, b)
}

// jsMod implements JS's % operator: sign follows the dividend, NaN
// propagates, division by zero is NaN. math.Mod matches this exactly.
func jsMod(a, b float64) float64 {
	// Standard library handles the spec-correct cases (NaN, ±0, ±Inf).
	// Go's binary % is integer-only so we deliberately use math.Mod.
	if b == 0 {
		return mathNaN()
	}
	return mathMod(a, b)
}

// toInt32 implements the JS ToInt32 abstract op: NaN/Inf → 0;
// otherwise truncate-then-mod-2^32 then reinterpret as signed.
// Bitwise ops in JS work on int32; ToInt32 makes them well-defined
// for non-integer inputs.
func toInt32(f float64) int32 {
	if f != f || f >= 1.7976931348623157e308 || f <= -1.7976931348623157e308 {
		return 0
	}
	// Truncate toward zero, then take low 32 bits.
	t := int64(f)
	return int32(t)
}

func toUint32(f float64) uint32 { return uint32(toInt32(f)) }

// typeofValue mirrors the JS `typeof` operator. Note that arrays
// report as "object" (matching spec, not Array.isArray) and the only
// callable category is Function.
func typeofValue(v value.Value) string {
	switch v.Type() {
	case value.TypeUndefined:
		return "undefined"
	case value.TypeNull:
		return "object"
	case value.TypeBool:
		return "boolean"
	case value.TypeNumber:
		return "number"
	case value.TypeString:
		return "string"
	case value.TypeFunction:
		return "function"
	case value.TypeSymbol:
		return "symbol"
	case value.TypeBigInt:
		return "bigint"
	}
	return "object"
}

func truthy(v value.Value) bool {
	switch v.Type() {
	case value.TypeUndefined, value.TypeNull:
		return false
	case value.TypeBool, value.TypeNumber:
		f := v.AsNumber()
		return f != 0 && f == f
	case value.TypeString:
		return v.AsString() != ""
	}
	return true
}

func jsEqual(a, b value.Value, strict bool) bool {
	if a.Type() == b.Type() {
		switch a.Type() {
		case value.TypeUndefined, value.TypeNull:
			return true
		case value.TypeBool, value.TypeNumber:
			af, bf := a.AsNumber(), b.AsNumber()
			if af != af || bf != bf {
				return false
			}
			return af == bf
		case value.TypeString:
			return a.AsString() == b.AsString()
		case value.TypeObject:
			return a.AsObject() == b.AsObject()
		case value.TypeArray:
			return a.AsArray() == b.AsArray()
		case value.TypeFunction:
			return a.AsFunction() == b.AsFunction()
		case value.TypeSymbol:
			return a.AsSymbol() == b.AsSymbol()
		case value.TypeBigInt:
			return a.AsBigInt().I.Cmp(b.AsBigInt().I) == 0
		}
		return false
	}
	if strict {
		return false
	}
	if (a.Type() == value.TypeUndefined && b.Type() == value.TypeNull) ||
		(a.Type() == value.TypeNull && b.Type() == value.TypeUndefined) {
		return true
	}
	return false
}
