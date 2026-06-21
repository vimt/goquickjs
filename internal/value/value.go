// Package value defines the runtime JS value and the Object/Shape
// pair that backs reference types.
//
// Layout note: we aimed for 16 bytes but Go's alignment of float64
// alongside a pointer field forces 24 bytes. Any layout that squeezes
// into 16 bytes either packs the reference into a uintptr (Go GC
// would lose sight of objects) or sacrifices float64 precision. 24
// bytes is the smallest representation that keeps small primitives
// allocation-free and reference types GC-traced.
//
// Fields:
//   - num: payload for Number; for Bool, 0/1 carries the value.
//   - ref: payload for String/Object/BigInt/Symbol. unsafe.Pointer
//     means Go GC still traces it as long as it points to a Go-heap
//     object (which is the only thing we put there). nil for primitives.
//   - tag: discriminator.
//
// File layout: this file holds Value itself; Shape/Object live in
// object.go; Function in function.go; Array in array.go; iterators
// in iterator.go; primitive-property dispatchers in string_helpers.go.
package value

import (
	"math"
	"math/big"
	"strconv"
	"strings"
	"unsafe"
)

// Type discriminates the variant carried by a Value.
type Type uint8

const (
	TypeUndefined Type = iota
	TypeNull
	TypeBool
	TypeNumber
	TypeString
	TypeObject
	TypeArray
	TypeBigInt
	TypeSymbol
	TypeFunction
)

// Value is the runtime form of any JS value.
type Value struct {
	num float64
	ref unsafe.Pointer
	tag Type
}

// Constructors.

func Undefined() Value { return Value{tag: TypeUndefined} }
func Null() Value      { return Value{tag: TypeNull} }

func Bool(b bool) Value {
	if b {
		return Value{tag: TypeBool, num: 1}
	}
	return Value{tag: TypeBool}
}

func Number(f float64) Value { return Value{tag: TypeNumber, num: f} }

func String(s string) Value {
	return Value{tag: TypeString, ref: unsafe.Pointer(&s)}
}

func ObjectVal(o *Object) Value {
	return Value{tag: TypeObject, ref: unsafe.Pointer(o)}
}

func FunctionVal(f *Function) Value {
	return Value{tag: TypeFunction, ref: unsafe.Pointer(f)}
}

func ArrayVal(a *Array) Value {
	return Value{tag: TypeArray, ref: unsafe.Pointer(a)}
}

// Symbol is the runtime form of a JS Symbol primitive. We use the
// pointer identity of *Symbol as the SameValue identity — two calls
// to `Symbol("x")` yield distinct pointers and therefore distinct
// Symbols, matching the spec.
type Symbol struct {
	Description string
}

func SymbolVal(s *Symbol) Value {
	return Value{tag: TypeSymbol, ref: unsafe.Pointer(s)}
}

func (v Value) AsSymbol() *Symbol { return (*Symbol)(v.ref) }

// BigInt wraps math/big.Int as a JS primitive. Use BigIntVal /
// AsBigInt to get a Value form; arithmetic happens through the
// wrapped *big.Int.
type BigInt struct {
	I *big.Int
}

func NewBigInt(i int64) *BigInt { return &BigInt{I: big.NewInt(i)} }

func BigIntVal(b *BigInt) Value {
	return Value{tag: TypeBigInt, ref: unsafe.Pointer(b)}
}

func (v Value) AsBigInt() *BigInt { return (*BigInt)(v.ref) }

// Accessors. Callers must check Type() first; these do no checks so
// the hot path stays branchless.

func (v Value) Type() Type             { return v.tag }
func (v Value) AsNumber() float64      { return v.num }
func (v Value) AsBool() bool           { return v.num != 0 }
func (v Value) AsString() string       { return *(*string)(v.ref) }
func (v Value) AsObject() *Object      { return (*Object)(v.ref) }
func (v Value) AsArray() *Array        { return (*Array)(v.ref) }
func (v Value) AsFunction() *Function  { return (*Function)(v.ref) }

// String renders a Value as QuickJS's REPL/Eval does, so the differ
// can compare two engines' completion values as plain text. Objects
// use a JSON-style serialisation (the same one upstream prints when
// fmt.Sprint is called on the returned Value).
func (v Value) String() string {
	switch v.tag {
	case TypeUndefined:
		return "undefined"
	case TypeNull:
		return "null"
	case TypeBool:
		if v.AsBool() {
			return "true"
		}
		return "false"
	case TypeNumber:
		return formatNumber(v.num)
	case TypeString:
		return v.AsString()
	case TypeObject:
		return v.AsObject().stringify()
	case TypeArray:
		return v.AsArray().stringify()
	case TypeFunction:
		fn := v.AsFunction()
		if fn.Name != "" {
			return "function " + fn.Name + "() { [native code] }"
		}
		return "function () { [native code] }"
	case TypeSymbol:
		return "Symbol(" + v.AsSymbol().Description + ")"
	case TypeBigInt:
		return v.AsBigInt().I.String()
	}
	return "[unknown]"
}

// stringifyForJSON is the per-value serialiser used while rendering
// an Object or Array. Strings get JSON-quoted and escaped here
// (unlike the top-level Value.String() which returns them bare).
func (v Value) stringifyForJSON() string {
	switch v.tag {
	case TypeString:
		return `"` + jsonEscape(v.AsString()) + `"`
	case TypeObject:
		return v.AsObject().stringify()
	case TypeArray:
		return v.AsArray().stringify()
	default:
		return v.String()
	}
}

// formatNumber matches the upstream QuickJS oracle's rendering — which
// is NOT spec ToString but Go's fmt.Sprint convention: +Inf / -Inf /
// -0 instead of Infinity / -Infinity / 0. The differ uses this as its
// ground truth, so we follow suit; when we add test262 we'll wire a
// separate spec-strict ToString and switch by context.
func formatNumber(f float64) string {
	switch {
	case f != f:
		return "NaN"
	case math.IsInf(f, 1):
		return "+Inf"
	case math.IsInf(f, -1):
		return "-Inf"
	}
	if f == 0 {
		if math.Signbit(f) {
			return "-0"
		}
		return "0"
	}
	const safeInt = 9.007199254740992e15
	if f >= -safeInt && f <= safeInt && f == float64(int64(f)) {
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'g', -1, 64)
}

func jsonEscape(s string) string {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '"' || c == '\\' || c < 0x20 {
			goto slow
		}
	}
	return s
slow:
	var b strings.Builder
	b.Grow(len(s) + 4)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		default:
			if c < 0x20 {
				const hex = "0123456789abcdef"
				b.WriteString(`\u00`)
				b.WriteByte(hex[c>>4])
				b.WriteByte(hex[c&0xf])
			} else {
				b.WriteByte(c)
			}
		}
	}
	return b.String()
}

// JSThrow is the canonical error type a NativeFn or VM step returns
// when JS code throws. The VM catches it at try-block boundaries; if
// no handler is found it propagates back from Eval as a Go error.
// Val holds the JS value the user actually threw — usually an Error
// instance but in principle any value.
type JSThrow struct{ Val Value }

func (e *JSThrow) Error() string { return "uncaught: " + e.Val.String() }

// MakeError is a hook the builtins package installs at init time so
// the VM (or other leaf packages) can synthesise real JS Error /
// TypeError / RangeError / SyntaxError instances without importing
// the builtins package — which would form a cycle. Falls back to a
// plain string value if nobody installed it yet (init order edge case).
var MakeError = func(_, msg string) Value { return String(msg) }

// MakePromiseFulfilled / MakePromiseRejected are installed by
// builtins/promise.go so the VM can wrap an async-function result
// into a Promise without importing the builtins package. Until
// installed they fall through to the bare value/error.
var MakePromiseFulfilled = func(v Value) Value { return v }
var MakePromiseRejected = func(v Value) Value { return v }

// PromiseUnwrap returns (resolvedValue, "fulfilled") if v is a
// promise in the fulfilled state, (rejectionValue, "rejected") if
// rejected, ("", "pending") if pending. Hook installed by
// builtins/promise.go.
var PromiseUnwrap = func(v Value) (Value, string) { return v, "fulfilled" }

// ReportUnhandledRejections walks the promise package's bookkeeping
// for any rejected promise without a handler attached and logs a
// warning. Hook installed by builtins/promise.go; called by
// vm.drainMicrotasks after the queue empties.
var ReportUnhandledRejections = func() {}

// NewPendingPromise returns a fresh pending Promise plus its
// resolve/reject closures. Caller is the Caller used to enqueue
// reaction microtasks when the returned closures settle the promise.
// Hook installed by builtins/promise.go.
var NewPendingPromise = func(c Caller) (Value, func(Value), func(Value)) {
	var v Value
	return v, func(x Value) { v = x }, func(x Value) { v = x }
}

// PromiseSubscribe attaches (onF, onR) reactions to p. If p is
// already settled the reaction is queued as a microtask; otherwise
// it joins the promise's pending callback list. Hook installed by
// builtins/promise.go — used by OpAwait to wake a suspended async
// fiber when the awaited promise settles.
var PromiseSubscribe = func(c Caller, p Value, onF, onR func(Value)) {
	// Fallback: synchronously invoke onF with the value (non-promise
	// or fulfilled case).
	v, state := PromiseUnwrap(p)
	if state == "rejected" {
		onR(v)
	} else {
		onF(v)
	}
}
