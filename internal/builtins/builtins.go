// Package builtins implements the JavaScript built-in globals (Math,
// String, Array, Object, Number, ...) and their prototype methods.
//
// # Fan-out contract
//
// Every built-in method follows the same shape so new methods can be
// added in isolation — one file per built-in class, one func per
// method, one registration line, plus a corpus entry under
// internal/differ/testdata. An agent should never have to edit code
// outside its target file (plus its corpus entries).
//
//  1. Pick the right file: Array.prototype.X goes in array.go,
//     String.prototype.X in string.go, Math.X in math.go, etc.
//  2. Write the Go func with signature
//     `func(this value.Value, args []value.Value) (value.Value, error)`.
//     Use the helpers in helpers.go (argOrUndef, argNumber, argString,
//     intArg, badThis) so error messages and arg-defaulting stay
//     consistent across all methods.
//  3. Register it in the file's install function. For prototype
//     methods, write to value.ArrayProto / value.StringProto /
//     value.NumberProto. For static methods (Math.abs,
//     Object.keys), set them on the constructor object.
//  4. Add 1–3 corpus entries under
//     internal/differ/testdata/, named NNN_<class>_<method>.js,
//     covering: the spec-canonical case, an edge case (empty input
//     / undefined arg / boundary), and any divergence with QuickJS
//     that surprised you. The differ runs every fixture against the
//     upstream oracle automatically.
//  5. Run `go test ./internal/differ/` — every test must pass. If
//     a divergence is intentional (we don't implement some corner),
//     return ErrNotImplemented and let the corpus entry skip.
//
// # Install lifecycle
//
// init() populates the prototype maps once at process start.
// Install(globals) is called from each Eval to (re-)register the
// constructor namespace objects (Math, Array, ...) into a fresh
// globals map. Mutations the user makes to e.g. the Math object
// don't leak across Evals.
package builtins

import (
	"math"
	"strconv"
	"strings"

	"github.com/vimt/goquickjs/internal/value"
)

func init() {
	// Populate prototype tables. New prototype methods get appended
	// inside their per-class register functions.
	registerArrayPrototype()
	registerStringPrototype()
	registerNumberPrototype()
	registerFunctionPrototype()
}

// Install registers the top-level built-in globals into the given
// map. Caller passes a fresh map per Eval; this function does not
// touch prototype tables (those are init-time).
func Install(globals map[string]value.Value) {
	installMath(globals)
	installArray(globals)
	installString(globals)
	installObject(globals)
	installNumber(globals)
	installError(globals)
	installSet(globals)
	installMap(globals)
	installDate(globals)
	installJSON(globals)
	installFunctionAndBoolean(globals)
	installSymbol(globals)
	installRegExp(globals)
	installPromise(globals)
	installWeakMap(globals)
	installWeakSet(globals)
	installReflect(globals)
	installBigInt(globals)
	installProxy(globals)
	installTypedArrays(globals)

	// Numeric constants reachable as bare identifiers.
	globals["Infinity"] = value.Number(math.Inf(1))
	globals["NaN"] = value.Number(math.NaN())
	globals["undefined"] = value.Undefined() // also a keyword; harmless dup

	// globalThis points at a fresh container exposing the same
	// builtins. Users mostly read `globalThis.X === X`, which
	// holds because we copy the same Value entries.
	gtObj := value.NewObject()
	for k, val := range globals {
		gtObj.Set(k, val)
	}
	gtObj.Set("globalThis", value.ObjectVal(gtObj))
	globals["globalThis"] = value.ObjectVal(gtObj)

	// Global functions (parseInt, parseFloat, isNaN, isFinite,
	// encodeURIComponent, decodeURIComponent). They are plain
	// callables hung directly on the global scope, not on a
	// namespace object.
	globals["parseInt"] = nativeFn("parseInt", 2, globalParseInt)
	globals["parseFloat"] = nativeFn("parseFloat", 1, globalParseFloat)
	globals["isNaN"] = nativeFn("isNaN", 1, globalIsNaN)
	globals["isFinite"] = nativeFn("isFinite", 1, globalIsFinite)
	globals["encodeURIComponent"] = nativeFn("encodeURIComponent", 1, globalEncodeURIComponent)
	globals["decodeURIComponent"] = nativeFn("decodeURIComponent", 1, globalDecodeURIComponent)
}

// nativeFn wraps a NativeFn into a Value, the boilerplate every
// registration line would otherwise repeat.
func nativeFn(name string, arity int, fn value.NativeFn) value.Value {
	return value.FunctionVal(&value.Function{Name: name, Arity: arity, Native: fn})
}

// --- parseInt / parseFloat ---

// trimLeadingWS strips ASCII whitespace from the front of s. JS spec
// trims a wider set (incl. U+00A0, U+FEFF, etc.) but ASCII covers
// the cases the differ throws at us.
func trimLeadingWS(s string) string {
	i := 0
	for i < len(s) {
		c := s[i]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\v' || c == '\f' {
			i++
			continue
		}
		break
	}
	return s[i:]
}

// globalParseInt implements parseInt(string, radix). Radix 0/missing
// means 10, unless the string has a 0x/0X prefix, in which case 16.
func globalParseInt(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	s := trimLeadingWS(argString(args, 0))

	// Optional sign.
	sign := 1
	if len(s) > 0 && (s[0] == '+' || s[0] == '-') {
		if s[0] == '-' {
			sign = -1
		}
		s = s[1:]
	}

	radix := 10
	if len(args) >= 2 {
		r := intArg(args, 1, 0)
		if r != 0 {
			radix = r
		}
	}
	if len(args) < 2 {
		radix = 0 // marker: "decide from prefix"
	}

	// 0x/0X prefix → hex (unless radix was forced to a non-16 value).
	stripPrefix := false
	if len(s) >= 2 && s[0] == '0' && (s[1] == 'x' || s[1] == 'X') {
		if radix == 0 || radix == 16 {
			radix = 16
			stripPrefix = true
		}
	}
	if radix == 0 {
		radix = 10
	}
	if stripPrefix {
		s = s[2:]
	}

	if radix < 2 || radix > 36 {
		return value.Number(math.NaN()), nil
	}

	// Eat as many valid digits as we can.
	digits := 0
	var acc float64
	for i := 0; i < len(s); i++ {
		c := s[i]
		var d int
		switch {
		case c >= '0' && c <= '9':
			d = int(c - '0')
		case c >= 'a' && c <= 'z':
			d = int(c-'a') + 10
		case c >= 'A' && c <= 'Z':
			d = int(c-'A') + 10
		default:
			d = radix // sentinel: invalid
		}
		if d >= radix {
			break
		}
		acc = acc*float64(radix) + float64(d)
		digits++
	}
	if digits == 0 {
		return value.Number(math.NaN()), nil
	}
	if sign < 0 {
		acc = -acc
	}
	return value.Number(acc), nil
}

// globalParseFloat eats a leading numeric literal (incl. "Infinity").
// JS doesn't recognise hex floats here.
func globalParseFloat(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	s := trimLeadingWS(argString(args, 0))

	// Capture optional sign for Infinity literal detection.
	signRune := byte(0)
	rest := s
	if len(rest) > 0 && (rest[0] == '+' || rest[0] == '-') {
		signRune = rest[0]
		rest = rest[1:]
	}
	if strings.HasPrefix(rest, "Infinity") {
		if signRune == '-' {
			return value.Number(math.Inf(-1)), nil
		}
		return value.Number(math.Inf(1)), nil
	}

	// Find the longest decimal-number prefix of s. Grammar:
	//   [+-]? ( digits ('.' digits?)? | '.' digits ) ([eE][+-]?digits)?
	i := 0
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		i++
	}
	start := i
	digitsBeforeDot := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
		digitsBeforeDot++
	}
	digitsAfterDot := 0
	if i < len(s) && s[i] == '.' {
		i++
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
			digitsAfterDot++
		}
	}
	if digitsBeforeDot == 0 && digitsAfterDot == 0 {
		return value.Number(math.NaN()), nil
	}
	// Optional exponent — only consume if followed by [+-]?digit.
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
	if i == start {
		return value.Number(math.NaN()), nil
	}
	f, err := strconv.ParseFloat(s[:i], 64)
	if err != nil {
		return value.Number(math.NaN()), nil
	}
	return value.Number(f), nil
}

// --- isNaN / isFinite ---
//
// Note: these are the legacy globals which ToNumber-coerce first.
// Number.isNaN / Number.isFinite do not coerce.

// toNumber implements ES ToNumber for the legacy isNaN/isFinite
// globals. value.AsNumber() returns the raw float slot, which is 0
// for any non-Number variant — that gives "isNaN('hello') === false"
// (wrong). Do the coercion ourselves.
func toNumber(args []value.Value, i int) float64 {
	if i >= len(args) {
		return math.NaN()
	}
	v := args[i]
	switch v.Type() {
	case value.TypeUndefined:
		return math.NaN()
	case value.TypeNull:
		return 0
	case value.TypeBool:
		if v.AsBool() {
			return 1
		}
		return 0
	case value.TypeNumber:
		return v.AsNumber()
	case value.TypeString:
		s := strings.TrimSpace(v.AsString())
		if s == "" {
			return 0
		}
		// JS literal "Infinity" support.
		switch s {
		case "Infinity", "+Infinity":
			return math.Inf(1)
		case "-Infinity":
			return math.Inf(-1)
		}
		// Hex prefix (no exponent allowed).
		if len(s) > 2 && s[0] == '0' && (s[1] == 'x' || s[1] == 'X') {
			n, err := strconv.ParseInt(s[2:], 16, 64)
			if err != nil {
				return math.NaN()
			}
			return float64(n)
		}
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return math.NaN()
		}
		return f
	}
	// Objects / Arrays / Functions: ToPrimitive then ToNumber. We
	// don't model valueOf yet — treat them as NaN.
	return math.NaN()
}

func globalIsNaN(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	f := toNumber(args, 0)
	return value.Bool(f != f), nil
}

func globalIsFinite(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	f := toNumber(args, 0)
	return value.Bool(!math.IsNaN(f) && !math.IsInf(f, 0)), nil
}

// --- encodeURIComponent / decodeURIComponent ---

// uriUnreserved reports whether c is left unencoded by
// encodeURIComponent. Per ES spec: A-Z a-z 0-9 - _ . ! ~ * ' ( ).
func uriUnreserved(c byte) bool {
	switch {
	case c >= 'A' && c <= 'Z':
		return true
	case c >= 'a' && c <= 'z':
		return true
	case c >= '0' && c <= '9':
		return true
	}
	switch c {
	case '-', '_', '.', '!', '~', '*', '\'', '(', ')':
		return true
	}
	return false
}

func globalEncodeURIComponent(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	s := argString(args, 0)
	var b strings.Builder
	const hex = "0123456789ABCDEF"
	for i := 0; i < len(s); i++ {
		c := s[i]
		if uriUnreserved(c) {
			b.WriteByte(c)
		} else {
			b.WriteByte('%')
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&0xf])
		}
	}
	return value.String(b.String()), nil
}

func globalDecodeURIComponent(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	s := argString(args, 0)
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		c := s[i]
		if c != '%' {
			b.WriteByte(c)
			i++
			continue
		}
		if i+2 >= len(s) {
			// Malformed sequence — JS would throw URIError. We have
			// no try/catch yet so surface as a Go error.
			return value.Value{}, &typeError{msg: "decodeURIComponent: malformed URI"}
		}
		hi, ok1 := hexDigit(s[i+1])
		lo, ok2 := hexDigit(s[i+2])
		if !ok1 || !ok2 {
			return value.Value{}, &typeError{msg: "decodeURIComponent: malformed URI"}
		}
		b.WriteByte(byte(hi<<4 | lo))
		i += 3
	}
	return value.String(b.String()), nil
}

func hexDigit(c byte) (int, bool) {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0'), true
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10, true
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10, true
	}
	return 0, false
}
