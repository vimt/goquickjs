// Helpers shared by every builtin method. Keep them small and
// purpose-built so divergent error wording / arg defaulting doesn't
// proliferate across files.

package builtins

import (
	"fmt"
	"math"

	"github.com/vimt/goquickjs/internal/value"
)

// argOrUndef returns args[i] or undefined when missing.
func argOrUndef(args []value.Value, i int) value.Value {
	if i >= len(args) {
		return value.Undefined()
	}
	return args[i]
}

// arrayLikeView abstracts a read-side view of either a real Array or
// an arbitrary array-like object (anything with a `length` property
// and integer-indexed slots). It lets Array.prototype.X methods that
// the spec defines via the generic array-like protocol accept e.g.
// `Array.prototype.map.call({length: 2, 0: 1, 1: 2}, ...)` instead of
// rejecting with a badThis. Only the read side is modelled — methods
// that mutate `this` keep their original Array-only fast path.
type arrayLikeView struct {
	arr *value.Array
	obj *value.Object
}

// arrayLikeFrom returns a view over `this` if it's array-shaped, or
// (zero, false) otherwise. arguments-like Objects, strings as objects,
// and the wrapper output of Object(primitive) all qualify.
func arrayLikeFrom(this value.Value) (arrayLikeView, bool) {
	switch this.Type() {
	case value.TypeArray:
		return arrayLikeView{arr: this.AsArray()}, true
	case value.TypeObject:
		return arrayLikeView{obj: this.AsObject()}, true
	}
	return arrayLikeView{}, false
}

// Len reads the array's length. For an Object, missing/non-numeric/NaN
// length reads as 0 and negative is clamped, matching the spec's
// ToLength.
func (v arrayLikeView) Length() int {
	if v.arr != nil {
		return v.arr.Length()
	}
	n, _ := v.obj.Get("length")
	if n.Type() != value.TypeNumber {
		return 0
	}
	f := n.AsNumber()
	if f != f || f < 0 {
		return 0
	}
	return int(f)
}

// Get fetches index i. Out-of-range / missing reads return undefined.
func (v arrayLikeView) Get(i int) value.Value {
	if v.arr != nil {
		return v.arr.Get(i)
	}
	val, _ := v.obj.Get(intToKey(i))
	return val
}

// exposeProto builds the Foo.prototype Object that mirrors a builtin's
// prototype method table (value.ArrayProto, StringProto, NumberProto,
// FunctionProto). It's read-only as far as method dispatch is concerned
// — instance lookup still goes through the proto map directly — but it
// lets `Foo.prototype.method` resolve in user code and supports
// .call/.apply against arbitrary receivers (each method's own badThis
// check enforces the receiver type).
func exposeProto(table map[string]*value.Function) value.Value {
	o := value.NewObject()
	for name, fn := range table {
		o.Set(name, value.FunctionVal(fn))
	}
	return value.ObjectVal(o)
}

// argNumber returns args[i] coerced to float64; missing → NaN
// (matches what `Math.abs()` does in real QuickJS).
func argNumber(args []value.Value, i int) float64 {
	if i >= len(args) {
		return math.NaN()
	}
	return args[i].AsNumber()
}

// argString returns args[i] as a string (using the value's
// canonical String()), or "" when missing.
func argString(args []value.Value, i int) string {
	if i >= len(args) {
		return ""
	}
	return args[i].String()
}

// intArg returns args[i] truncated to int, or fallback when missing.
// JS semantics: ToInteger rounds toward zero; NaN → 0.
func intArg(args []value.Value, i, fallback int) int {
	if i >= len(args) {
		return fallback
	}
	f := args[i].AsNumber()
	if f != f { // NaN
		return 0
	}
	return int(f)
}

// badThis returns the canonical "this is not a foo" TypeError the
// real engine throws when a prototype method is invoked on the
// wrong receiver type. It's a real JS throw so user code can catch
// it; pre-Error-class corpus that just checks "did we error" still
// works because the differ treats both-sides-errored as equal.
func badThis(method, want string) error {
	return &value.JSThrow{Val: makeError("TypeError", fmt.Sprintf("%s: this is not a %s", method, want))}
}

// isTruthy implements JS ToBoolean. Mirrors vm.truthy but vm imports
// value not builtins, so we duplicate the (tiny) logic here.
func isTruthy(v value.Value) bool {
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

// typeError is a residual Go-error type for the few places that
// pre-dated try/catch. New code should throw a real JS Error via
// value.JSThrow + makeError instead.
type typeError struct{ msg string }

func (e *typeError) Error() string { return e.msg }
