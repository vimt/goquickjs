// Number constructor object and Number.prototype methods.
//
// Prototype methods (toString, toFixed, ...) land in
// value.NumberProto. Static constants (MAX_SAFE_INTEGER, EPSILON,
// ...) go on the constructor object built by installNumber.

package builtins

import (
	"math"
	"strconv"
	"strings"
	"unicode"

	"github.com/vimt/goquickjs/internal/jserrors"
	"github.com/vimt/goquickjs/internal/value"
)

// maxSafeInteger / minSafeInteger mirror the ECMAScript constants
// (2^53 - 1). EPSILON is the smallest gap between 1 and the next
// representable double.
const (
	maxSafeInteger = 9007199254740991  // 2^53 - 1
	minSafeInteger = -9007199254740991 // -(2^53 - 1)
)

// numberCoerce implements `Number(x)` as the spec ToNumber coercion.
// `new Number(x)` returns the same primitive (no wrapper Object).
func numberCoerce(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	if len(args) == 0 {
		return value.Number(0), nil
	}
	return value.Number(args[0].AsNumber()), nil
}

func installNumber(globals map[string]value.Value) {
	fn := &value.Function{Name: "Number", Arity: 1, Native: numberCoerce}
	fn.Props = value.NewObject()
	ctor := fn.Props

	// Numeric constants.
	ctor.Set("NaN", value.Number(math.NaN()))
	ctor.Set("POSITIVE_INFINITY", value.Number(math.Inf(1)))
	ctor.Set("NEGATIVE_INFINITY", value.Number(math.Inf(-1)))
	ctor.Set("MAX_SAFE_INTEGER", value.Number(float64(maxSafeInteger)))
	ctor.Set("MIN_SAFE_INTEGER", value.Number(float64(minSafeInteger)))
	ctor.Set("EPSILON", value.Number(2.220446049250313e-16))
	ctor.Set("MAX_VALUE", value.Number(math.MaxFloat64))
	ctor.Set("MIN_VALUE", value.Number(5e-324))

	// Static predicate methods. Unlike the global isFinite/isNaN,
	// these never coerce: a non-number argument is rejected outright.
	ctor.Set("isFinite", nativeFn("isFinite", 1, numberIsFinite))
	ctor.Set("isInteger", nativeFn("isInteger", 1, numberIsInteger))
	ctor.Set("isNaN", nativeFn("isNaN", 1, numberIsNaN))
	ctor.Set("isSafeInteger", nativeFn("isSafeInteger", 1, numberIsSafeInteger))

	// String → number parsers (mirror the global functions of the
	// same name, which we do not have yet).
	ctor.Set("parseFloat", nativeFn("parseFloat", 1, numberParseFloat))
	ctor.Set("parseInt", nativeFn("parseInt", 2, numberParseInt))
	ctor.Set("prototype", exposeProto(value.NumberProto))

	globals["Number"] = value.FunctionVal(fn)
}

func registerNumberPrototype() {
	value.NumberProto["toString"] = &value.Function{Name: "toString", Arity: 1, Native: numberToString}
	value.NumberProto["toFixed"] = &value.Function{Name: "toFixed", Arity: 1, Native: numberToFixed}
	value.NumberProto["toExponential"] = &value.Function{Name: "toExponential", Arity: 1, Native: numberToExponential}
	value.NumberProto["toPrecision"] = &value.Function{Name: "toPrecision", Arity: 1, Native: numberToPrecision}
	value.NumberProto["valueOf"] = &value.Function{Name: "valueOf", Arity: 0, Native: numberValueOf}
}

// numberIsFinite implements Number.isFinite — strict: a non-number
// receiver is false (no ToNumber coercion, unlike the global isFinite).
func numberIsFinite(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	v := argOrUndef(args, 0)
	if v.Type() != value.TypeNumber {
		return value.Bool(false), nil
	}
	f := v.AsNumber()
	return value.Bool(!math.IsNaN(f) && !math.IsInf(f, 0)), nil
}

// numberIsInteger reports whether v is a number that is a finite
// integer value (3.0 is integer; 3.5 is not; NaN/Inf are not).
func numberIsInteger(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	v := argOrUndef(args, 0)
	if v.Type() != value.TypeNumber {
		return value.Bool(false), nil
	}
	f := v.AsNumber()
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return value.Bool(false), nil
	}
	return value.Bool(math.Trunc(f) == f), nil
}

// numberIsNaN — strict counterpart to the global isNaN.
func numberIsNaN(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	v := argOrUndef(args, 0)
	if v.Type() != value.TypeNumber {
		return value.Bool(false), nil
	}
	f := v.AsNumber()
	return value.Bool(math.IsNaN(f)), nil
}

// numberIsSafeInteger — isInteger and within ±(2^53 - 1).
func numberIsSafeInteger(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	v := argOrUndef(args, 0)
	if v.Type() != value.TypeNumber {
		return value.Bool(false), nil
	}
	f := v.AsNumber()
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return value.Bool(false), nil
	}
	if math.Trunc(f) != f {
		return value.Bool(false), nil
	}
	return value.Bool(math.Abs(f) <= float64(maxSafeInteger)), nil
}

// numberParseFloat mirrors global parseFloat: skip leading whitespace,
// then parse the longest prefix that forms a valid decimal number.
func numberParseFloat(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	s := strings.TrimLeftFunc(argString(args, 0), unicode.IsSpace)
	if s == "" {
		return value.Number(math.NaN()), nil
	}
	// Handle Infinity prefix explicitly — strconv would not.
	sign := 1.0
	rest := s
	if rest[0] == '+' || rest[0] == '-' {
		if rest[0] == '-' {
			sign = -1
		}
		rest = rest[1:]
	}
	if strings.HasPrefix(rest, "Infinity") {
		return value.Number(sign * math.Inf(1)), nil
	}
	// Walk forward to find the longest numeric prefix, then hand to
	// strconv. Accepts an optional fractional part and exponent.
	end := numericPrefix(s)
	if end == 0 {
		return value.Number(math.NaN()), nil
	}
	f, err := strconv.ParseFloat(s[:end], 64)
	if err != nil {
		return value.Number(math.NaN()), nil
	}
	return value.Number(f), nil
}

// numericPrefix returns the length of the longest prefix of s that
// parses as a JS decimal number (sign, digits, optional fraction,
// optional exponent). Used by parseFloat.
func numericPrefix(s string) int {
	i := 0
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		i++
	}
	start := i
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	hasInt := i > start
	if i < len(s) && s[i] == '.' {
		i++
		fracStart := i
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
		if !hasInt && i == fracStart {
			return 0
		}
	} else if !hasInt {
		return 0
	}
	// Optional exponent.
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		j := i + 1
		if j < len(s) && (s[j] == '+' || s[j] == '-') {
			j++
		}
		expStart := j
		for j < len(s) && s[j] >= '0' && s[j] <= '9' {
			j++
		}
		if j > expStart {
			i = j
		}
	}
	return i
}

// numberParseInt mirrors global parseInt: trim whitespace, optional
// sign, optional 0x prefix that auto-sets radix to 16; otherwise
// uses the supplied radix (default 10).
func numberParseInt(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	s := strings.TrimLeftFunc(argString(args, 0), unicode.IsSpace)
	radixV := argOrUndef(args, 1)
	radix := 0 // 0 = unspecified; defaulted to 10 below after 0x check
	if radixV.Type() == value.TypeNumber {
		radix = int(radixV.AsNumber())
	} else if radixV.Type() != value.TypeUndefined {
		if r, err := strconv.Atoi(radixV.String()); err == nil {
			radix = r
		}
	}

	if s == "" {
		return value.Number(math.NaN()), nil
	}
	sign := 1.0
	if s[0] == '+' || s[0] == '-' {
		if s[0] == '-' {
			sign = -1
		}
		s = s[1:]
	}
	if s == "" {
		return value.Number(math.NaN()), nil
	}
	// 0x / 0X prefix forces radix 16 — but only when the caller did
	// not pin a non-16 radix.
	if (radix == 0 || radix == 16) && len(s) >= 2 && s[0] == '0' && (s[1] == 'x' || s[1] == 'X') {
		s = s[2:]
		radix = 16
	}
	if radix == 0 {
		radix = 10
	}
	if radix < 2 || radix > 36 {
		return value.Number(math.NaN()), nil
	}
	// Consume the longest valid prefix in this radix.
	end := 0
	for end < len(s) {
		d := digitValue(s[end])
		if d < 0 || d >= radix {
			break
		}
		end++
	}
	if end == 0 {
		return value.Number(math.NaN()), nil
	}
	n, err := strconv.ParseInt(s[:end], radix, 64)
	if err != nil {
		// Overflow falls back to float parsing so very large bases-10
		// numbers (which can lose precision) still return *something*
		// numeric. Match JS by going through ParseFloat for radix 10.
		if radix == 10 {
			if f, ferr := strconv.ParseFloat(s[:end], 64); ferr == nil {
				return value.Number(sign * f), nil
			}
		}
		return value.Number(math.NaN()), nil
	}
	return value.Number(sign * float64(n)), nil
}

// digitValue maps a single ASCII byte to its numeric digit value
// (0-35), or -1 if it's not a valid base-36 digit.
func digitValue(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'z':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'Z':
		return int(c-'A') + 10
	}
	return -1
}

// numberToString implements Number.prototype.toString. The optional
// radix defaults to 10. For radix 10 we go through the same
// formatter Value.String() uses so 3 → "3", 3.14 → "3.14", etc.
func numberToString(_ value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	if this.Type() != value.TypeNumber {
		return value.Value{}, badThis("Number.prototype.toString", "number")
	}
	radix := 10
	if r := argOrUndef(args, 0); r.Type() != value.TypeUndefined {
		radix = int(r.AsNumber())
	}
	if radix < 2 || radix > 36 {
		return value.Value{}, &typeError{msg: "Number.prototype.toString: radix out of range"}
	}
	f := this.AsNumber()
	if radix == 10 {
		return value.String(this.String()), nil
	}
	if math.IsNaN(f) {
		return value.String("NaN"), nil
	}
	if math.IsInf(f, 1) {
		return value.String("Infinity"), nil
	}
	if math.IsInf(f, -1) {
		return value.String("-Infinity"), nil
	}
	if f == math.Trunc(f) && !math.IsInf(f, 0) && f >= -float64(1<<62) && f <= float64(1<<62) {
		return value.String(strconv.FormatInt(int64(f), radix)), nil
	}
	// Non-integer non-radix-10 toString is rarely used and ambiguous
	// in spec corners; punt rather than risk diverging.
	return value.Value{}, jserrors.ErrNotImplemented
}

// numberToFixed implements Number.prototype.toFixed(digits=0).
// Produces the standard fixed-point notation. NaN renders as "NaN".
func numberToFixed(_ value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	if this.Type() != value.TypeNumber {
		return value.Value{}, badThis("Number.prototype.toFixed", "number")
	}
	digits := intArg(args, 0, 0)
	if digits < 0 || digits > 100 {
		return value.Value{}, &typeError{msg: "Number.prototype.toFixed: digits out of range"}
	}
	f := this.AsNumber()
	if math.IsNaN(f) {
		return value.String("NaN"), nil
	}
	if math.IsInf(f, 1) {
		return value.String("Infinity"), nil
	}
	if math.IsInf(f, -1) {
		return value.String("-Infinity"), nil
	}
	return value.String(strconv.FormatFloat(f, 'f', digits, 64)), nil
}

// numberValueOf — Number.prototype.valueOf. Spec is "return the
// [[NumberData]] internal slot"; since our value-class numbers are
// primitives, we just hand back `this` once we've type-checked it.
func numberValueOf(_ value.Caller, this value.Value, _ []value.Value) (value.Value, error) {
	if this.Type() != value.TypeNumber {
		return value.Value{}, badThis("Number.prototype.valueOf", "number")
	}
	return this, nil
}

// numberToExponential implements Number.prototype.toExponential.
// JS wants the exponent printed with no leading zeros and an
// explicit sign ("1.23e+2" / "1.23e-4"). Go's strconv pads the
// exponent ("1.23e+02"); normaliseExp strips the padding so output
// agrees with the oracle.
func numberToExponential(_ value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	if this.Type() != value.TypeNumber {
		return value.Value{}, badThis("Number.prototype.toExponential", "number")
	}
	f := this.AsNumber()
	if math.IsNaN(f) {
		return value.String("NaN"), nil
	}
	if math.IsInf(f, 1) {
		return value.String("Infinity"), nil
	}
	if math.IsInf(f, -1) {
		return value.String("-Infinity"), nil
	}
	// digits omitted / undefined → shortest representation
	// (Go's -1 precision picks the minimum).
	digits := -1
	if a := argOrUndef(args, 0); a.Type() != value.TypeUndefined {
		d := intArg(args, 0, 0)
		if d < 0 || d > 100 {
			return value.Value{}, &value.JSThrow{Val: makeError("RangeError",
				"Number.prototype.toExponential: digits out of range")}
		}
		digits = d
	}
	s := strconv.FormatFloat(f, 'e', digits, 64)
	return value.String(normaliseExp(s)), nil
}

// numberToPrecision implements Number.prototype.toPrecision.
// The spec branches on the magnitude of the value vs the requested
// precision; we sidestep most of the bookkeeping by formatting once
// in 'e' form with (p-1) fractional digits, then deciding whether
// to render that as exponential or as fixed by repositioning the
// decimal point.
func numberToPrecision(_ value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	if this.Type() != value.TypeNumber {
		return value.Value{}, badThis("Number.prototype.toPrecision", "number")
	}
	// Missing / undefined precision → toString behaviour.
	if len(args) == 0 || args[0].Type() == value.TypeUndefined {
		return value.String(this.String()), nil
	}
	f := this.AsNumber()
	if math.IsNaN(f) {
		return value.String("NaN"), nil
	}
	if math.IsInf(f, 1) {
		return value.String("Infinity"), nil
	}
	if math.IsInf(f, -1) {
		return value.String("-Infinity"), nil
	}
	p := intArg(args, 0, 0)
	if p < 1 || p > 100 {
		return value.Value{}, &value.JSThrow{Val: makeError("RangeError",
			"Number.prototype.toPrecision: precision out of range")}
	}

	// Format in 'e' form with p-1 fractional digits so we have
	// exactly p significant digits to work with.
	raw := strconv.FormatFloat(f, 'e', p-1, 64)

	// Pull apart: optional sign, mantissa digits (sans dot), exponent.
	sign := ""
	body := raw
	if body[0] == '-' || body[0] == '+' {
		if body[0] == '-' {
			sign = "-"
		}
		body = body[1:]
	}
	// Find 'e'.
	eIdx := strings.IndexByte(body, 'e')
	mant := body[:eIdx]
	expStr := body[eIdx+1:]
	exp, err := strconv.Atoi(expStr)
	if err != nil {
		return value.Value{}, &value.JSThrow{Val: makeError("RangeError",
			"Number.prototype.toPrecision: format error")}
	}
	// Strip dot from mantissa to leave just the p significant digits.
	digits := strings.Replace(mant, ".", "", 1)
	if len(digits) < p {
		digits += strings.Repeat("0", p-len(digits))
	}

	// Spec: if exp < -6 or exp >= p → exponential form.
	if exp < -6 || exp >= p {
		out := digits[:1]
		if p > 1 {
			out += "." + digits[1:]
		}
		out += "e" + signedExp(exp)
		// -0 special case: drop the leading minus to match QuickJS.
		if sign == "-" && f == 0 {
			sign = ""
		}
		return value.String(sign + out), nil
	}

	// Fixed form.
	var out string
	switch {
	case exp >= 0:
		// exp+1 digits before the decimal point.
		intLen := exp + 1
		intPart := digits[:intLen]
		fracPart := digits[intLen:]
		if len(fracPart) > 0 {
			out = intPart + "." + fracPart
		} else {
			out = intPart
		}
	default:
		// exp is in [-6, -1]; prefix is "0." + (-exp-1) zeros + digits.
		zeros := strings.Repeat("0", -exp-1)
		out = "0." + zeros + digits
	}
	if sign == "-" && f == 0 {
		sign = ""
	}
	return value.String(sign + out), nil
}

// normaliseExp converts a Go-formatted scientific number such as
// "1.23e+02" into the JS shape "1.23e+2". The input is assumed to
// have a single 'e' followed by a sign and at least one digit.
func normaliseExp(s string) string {
	i := strings.IndexByte(s, 'e')
	if i < 0 {
		return s
	}
	head := s[:i]
	tail := s[i+1:]
	if tail == "" {
		return s
	}
	sign := byte('+')
	if tail[0] == '+' || tail[0] == '-' {
		sign = tail[0]
		tail = tail[1:]
	}
	// Strip leading zeros (but keep at least one digit).
	j := 0
	for j < len(tail)-1 && tail[j] == '0' {
		j++
	}
	tail = tail[j:]
	return head + "e" + string(sign) + tail
}

// signedExp formats an integer exponent the way JS prints it:
// always-prefixed with "+" or "-" and no leading zeros.
func signedExp(e int) string {
	if e >= 0 {
		return "+" + strconv.Itoa(e)
	}
	return strconv.Itoa(e)
}
