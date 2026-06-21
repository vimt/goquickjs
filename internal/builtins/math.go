// Math static methods and constants.
//
// Add new entries directly inside installMath. Most map 1:1 to
// Go's math package; coerce args through argNumber so missing args
// behave like real QuickJS (NaN, not 0).

package builtins

import (
	"math"
	"math/bits"

	"github.com/vimt/goquickjs/internal/value"
)

func installMath(globals map[string]value.Value) {
	m := value.NewObject()
	// Constants
	m.Set("PI", value.Number(math.Pi))
	m.Set("E", value.Number(math.E))
	m.Set("LN2", value.Number(math.Ln2))
	m.Set("LN10", value.Number(math.Log(10)))
	m.Set("LOG2E", value.Number(math.Log2E))
	m.Set("LOG10E", value.Number(math.Log10E))
	m.Set("SQRT2", value.Number(math.Sqrt2))
	m.Set("SQRT1_2", value.Number(1/math.Sqrt2))

	// Integralization
	m.Set("abs", nativeFn("abs", 1, mathAbs))
	m.Set("floor", nativeFn("floor", 1, mathFloor))
	m.Set("ceil", nativeFn("ceil", 1, mathCeil))
	m.Set("round", nativeFn("round", 1, mathRound))
	m.Set("trunc", nativeFn("trunc", 1, mathTrunc))
	m.Set("sign", nativeFn("sign", 1, mathSign))

	// Power / roots / logs
	m.Set("sqrt", nativeFn("sqrt", 1, mathSqrt))
	m.Set("cbrt", nativeFn("cbrt", 1, mathCbrt))
	m.Set("pow", nativeFn("pow", 2, mathPow))
	m.Set("exp", nativeFn("exp", 1, mathExp))
	m.Set("expm1", nativeFn("expm1", 1, mathExpm1))
	m.Set("log", nativeFn("log", 1, mathLog))
	m.Set("log1p", nativeFn("log1p", 1, mathLog1p))
	m.Set("log2", nativeFn("log2", 1, mathLog2))
	m.Set("log10", nativeFn("log10", 1, mathLog10))

	// Trigonometry
	m.Set("sin", nativeFn("sin", 1, mathSin))
	m.Set("cos", nativeFn("cos", 1, mathCos))
	m.Set("tan", nativeFn("tan", 1, mathTan))
	m.Set("asin", nativeFn("asin", 1, mathAsin))
	m.Set("acos", nativeFn("acos", 1, mathAcos))
	m.Set("atan", nativeFn("atan", 1, mathAtan))
	m.Set("atan2", nativeFn("atan2", 2, mathAtan2))

	// Hyperbolic
	m.Set("sinh", nativeFn("sinh", 1, mathSinh))
	m.Set("cosh", nativeFn("cosh", 1, mathCosh))
	m.Set("tanh", nativeFn("tanh", 1, mathTanh))
	m.Set("asinh", nativeFn("asinh", 1, mathAsinh))
	m.Set("acosh", nativeFn("acosh", 1, mathAcosh))
	m.Set("atanh", nativeFn("atanh", 1, mathAtanh))

	// Comparison (variadic)
	m.Set("min", nativeFn("min", 2, mathMin))
	m.Set("max", nativeFn("max", 2, mathMax))

	// Other
	m.Set("hypot", nativeFn("hypot", 2, mathHypot))
	m.Set("clz32", nativeFn("clz32", 1, mathClz32))
	m.Set("fround", nativeFn("fround", 1, mathFround))
	m.Set("imul", nativeFn("imul", 2, mathImul))

	globals["Math"] = value.ObjectVal(m)
}

func mathAbs(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	return value.Number(math.Abs(argNumber(args, 0))), nil
}

func mathFloor(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	return value.Number(math.Floor(argNumber(args, 0))), nil
}

func mathCeil(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	return value.Number(math.Ceil(argNumber(args, 0))), nil
}

// mathRound implements JS Math.round: round to nearest integer with
// ties going toward +Infinity (so round(-0.5) === 0, round(0.5) === 1,
// round(-1.5) === -1). Differs from Go's math.Round (ties away from
// zero). math.Floor(x + 0.5) gives the right behaviour for all finite
// inputs; NaN/Infinity propagate naturally.
func mathRound(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	x := argNumber(args, 0)
	if math.IsNaN(x) || math.IsInf(x, 0) {
		return value.Number(x), nil
	}
	return value.Number(math.Floor(x + 0.5)), nil
}

// mathTrunc: NaN stays NaN (Go's math.Trunc handles this), +/-Infinity
// stay infinite. Truncates toward zero otherwise.
func mathTrunc(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	return value.Number(math.Trunc(argNumber(args, 0))), nil
}

func mathSign(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	x := argNumber(args, 0)
	if math.IsNaN(x) {
		return value.Number(math.NaN()), nil
	}
	if x > 0 {
		return value.Number(1), nil
	}
	if x < 0 {
		return value.Number(-1), nil
	}
	// preserve +0 / -0
	return value.Number(x), nil
}

func mathSqrt(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	return value.Number(math.Sqrt(argNumber(args, 0))), nil
}

func mathCbrt(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	return value.Number(math.Cbrt(argNumber(args, 0))), nil
}

func mathPow(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	return value.Number(math.Pow(argNumber(args, 0), argNumber(args, 1))), nil
}

func mathExp(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	return value.Number(math.Exp(argNumber(args, 0))), nil
}

func mathExpm1(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	return value.Number(math.Expm1(argNumber(args, 0))), nil
}

func mathLog(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	return value.Number(math.Log(argNumber(args, 0))), nil
}

func mathLog1p(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	return value.Number(math.Log1p(argNumber(args, 0))), nil
}

func mathLog2(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	return value.Number(math.Log2(argNumber(args, 0))), nil
}

func mathLog10(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	return value.Number(math.Log10(argNumber(args, 0))), nil
}

func mathSin(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	return value.Number(math.Sin(argNumber(args, 0))), nil
}

func mathCos(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	return value.Number(math.Cos(argNumber(args, 0))), nil
}

func mathTan(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	return value.Number(math.Tan(argNumber(args, 0))), nil
}

func mathAsin(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	return value.Number(math.Asin(argNumber(args, 0))), nil
}

func mathAcos(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	return value.Number(math.Acos(argNumber(args, 0))), nil
}

func mathAtan(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	return value.Number(math.Atan(argNumber(args, 0))), nil
}

func mathAtan2(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	return value.Number(math.Atan2(argNumber(args, 0), argNumber(args, 1))), nil
}

func mathSinh(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	return value.Number(math.Sinh(argNumber(args, 0))), nil
}

func mathCosh(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	return value.Number(math.Cosh(argNumber(args, 0))), nil
}

func mathTanh(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	return value.Number(math.Tanh(argNumber(args, 0))), nil
}

func mathAsinh(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	return value.Number(math.Asinh(argNumber(args, 0))), nil
}

func mathAcosh(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	return value.Number(math.Acosh(argNumber(args, 0))), nil
}

func mathAtanh(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	return value.Number(math.Atanh(argNumber(args, 0))), nil
}

// mathMin: variadic. With no args returns +Infinity. Any NaN arg → NaN.
func mathMin(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	if len(args) == 0 {
		return value.Number(math.Inf(1)), nil
	}
	result := math.Inf(1)
	for i := range args {
		x := argNumber(args, i)
		if math.IsNaN(x) {
			return value.Number(math.NaN()), nil
		}
		// math.Min preserves -0 vs +0 (treats -0 < +0).
		result = math.Min(result, x)
	}
	return value.Number(result), nil
}

// mathMax: variadic. With no args returns -Infinity. Any NaN arg → NaN.
func mathMax(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	if len(args) == 0 {
		return value.Number(math.Inf(-1)), nil
	}
	result := math.Inf(-1)
	for i := range args {
		x := argNumber(args, i)
		if math.IsNaN(x) {
			return value.Number(math.NaN()), nil
		}
		result = math.Max(result, x)
	}
	return value.Number(result), nil
}

// mathHypot: sqrt(sum of squares). Variadic. Empty → 0. Any Infinity → +Infinity
// (even if a NaN is present, per JS spec). Otherwise any NaN → NaN.
func mathHypot(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	if len(args) == 0 {
		return value.Number(0), nil
	}
	hasNaN := false
	for i := range args {
		x := argNumber(args, i)
		if math.IsInf(x, 0) {
			return value.Number(math.Inf(1)), nil
		}
		if math.IsNaN(x) {
			hasNaN = true
		}
	}
	if hasNaN {
		return value.Number(math.NaN()), nil
	}
	// Use math.Hypot for the 2-arg fast path; fall back to a stable
	// sum-of-squares walk for the variadic case.
	if len(args) == 1 {
		return value.Number(math.Abs(argNumber(args, 0))), nil
	}
	if len(args) == 2 {
		return value.Number(math.Hypot(argNumber(args, 0), argNumber(args, 1))), nil
	}
	// Scale by the max magnitude to avoid overflow/underflow.
	maxAbs := 0.0
	for i := range args {
		a := math.Abs(argNumber(args, i))
		if a > maxAbs {
			maxAbs = a
		}
	}
	if maxAbs == 0 {
		return value.Number(0), nil
	}
	sum := 0.0
	for i := range args {
		v := argNumber(args, i) / maxAbs
		sum += v * v
	}
	return value.Number(maxAbs * math.Sqrt(sum)), nil
}

// mathClz32: count leading zero bits in the ToUint32 of x.
func mathClz32(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	x := argNumber(args, 0)
	u := toUint32(x)
	if u == 0 {
		return value.Number(32), nil
	}
	return value.Number(float64(bits.LeadingZeros32(u))), nil
}

// mathFround: round to nearest float32.
func mathFround(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	x := argNumber(args, 0)
	return value.Number(float64(float32(x))), nil
}

// mathImul: 32-bit signed integer multiply.
func mathImul(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	a := int32(toUint32(argNumber(args, 0)))
	b := int32(toUint32(argNumber(args, 1)))
	return value.Number(float64(a * b)), nil
}

// toUint32 implements the ECMAScript ToUint32 abstract operation:
// NaN/+/-Infinity/+/-0 → 0; otherwise truncate toward zero and reduce
// modulo 2^32.
func toUint32(x float64) uint32 {
	if math.IsNaN(x) || math.IsInf(x, 0) {
		return 0
	}
	// Truncate toward zero, then reduce mod 2^32.
	t := math.Trunc(x)
	m := math.Mod(t, 4294967296) // 2^32
	if m < 0 {
		m += 4294967296
	}
	return uint32(m)
}
