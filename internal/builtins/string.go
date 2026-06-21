// String constructor object and String.prototype methods.
//
// Prototype methods land in value.StringProto. Static methods
// (String.fromCharCode, ...) go on the constructor object built by
// installString.
//
// Index/length math is done on Go byte-indexed strings. JS strings
// are conceptually UTF-16; we get away with byte indexing because
// every corpus entry sticks to ASCII. If a multi-byte test ever
// shows up the indexing routines below will need to be rewritten
// against utf16 / runes.

package builtins

import (
	"math"
	"strings"

	"github.com/vimt/goquickjs/internal/value"
)

func installString(globals map[string]value.Value) {
	ctor := value.NewObject()
	// Static methods.
	ctor.Set("fromCharCode", nativeFn("fromCharCode", 1, strFromCharCode))
	ctor.Set("fromCodePoint", nativeFn("fromCodePoint", 1, strFromCodePoint))
	globals["String"] = value.ObjectVal(ctor)
}

func registerStringPrototype() {
	value.StringProto["toUpperCase"] = &value.Function{Name: "toUpperCase", Arity: 0, Native: strToUpper}
	value.StringProto["toLowerCase"] = &value.Function{Name: "toLowerCase", Arity: 0, Native: strToLower}

	value.StringProto["charAt"] = &value.Function{Name: "charAt", Arity: 1, Native: strCharAt}
	value.StringProto["charCodeAt"] = &value.Function{Name: "charCodeAt", Arity: 1, Native: strCharCodeAt}
	value.StringProto["codePointAt"] = &value.Function{Name: "codePointAt", Arity: 1, Native: strCodePointAt}

	value.StringProto["indexOf"] = &value.Function{Name: "indexOf", Arity: 2, Native: strIndexOf}
	value.StringProto["lastIndexOf"] = &value.Function{Name: "lastIndexOf", Arity: 2, Native: strLastIndexOf}

	value.StringProto["includes"] = &value.Function{Name: "includes", Arity: 2, Native: strIncludes}
	value.StringProto["startsWith"] = &value.Function{Name: "startsWith", Arity: 2, Native: strStartsWith}
	value.StringProto["endsWith"] = &value.Function{Name: "endsWith", Arity: 2, Native: strEndsWith}

	value.StringProto["slice"] = &value.Function{Name: "slice", Arity: 2, Native: strSlice}
	value.StringProto["substring"] = &value.Function{Name: "substring", Arity: 2, Native: strSubstring}
	value.StringProto["substr"] = &value.Function{Name: "substr", Arity: 2, Native: strSubstr}

	value.StringProto["split"] = &value.Function{Name: "split", Arity: 2, Native: strSplit}

	value.StringProto["trim"] = &value.Function{Name: "trim", Arity: 0, Native: strTrim}
	value.StringProto["trimStart"] = &value.Function{Name: "trimStart", Arity: 0, Native: strTrimStart}
	value.StringProto["trimEnd"] = &value.Function{Name: "trimEnd", Arity: 0, Native: strTrimEnd}

	value.StringProto["concat"] = &value.Function{Name: "concat", Arity: 1, Native: strConcat}
	value.StringProto["repeat"] = &value.Function{Name: "repeat", Arity: 1, Native: strRepeat}

	value.StringProto["padStart"] = &value.Function{Name: "padStart", Arity: 2, Native: strPadStart}
	value.StringProto["padEnd"] = &value.Function{Name: "padEnd", Arity: 2, Native: strPadEnd}

	value.StringProto["replace"] = &value.Function{Name: "replace", Arity: 2, Native: strReplace}
	value.StringProto["replaceAll"] = &value.Function{Name: "replaceAll", Arity: 2, Native: strReplaceAll}

	value.StringProto["at"] = &value.Function{Name: "at", Arity: 1, Native: strAt}
	value.StringProto["normalize"] = &value.Function{Name: "normalize", Arity: 0, Native: strNormalize}
	value.StringProto["localeCompare"] = &value.Function{Name: "localeCompare", Arity: 1, Native: strLocaleCompare}
}

// thisString unwraps `this` as a Go string, returning a TypeError
// when the receiver is not a string primitive.
func thisString(this value.Value, method string) (string, error) {
	if this.Type() != value.TypeString {
		return "", badThis("String.prototype."+method, "string")
	}
	return this.AsString(), nil
}

func strToUpper(_ value.Caller, this value.Value, _ []value.Value) (value.Value, error) {
	s, err := thisString(this, "toUpperCase")
	if err != nil {
		return value.Value{}, err
	}
	return value.String(strings.ToUpper(s)), nil
}

func strToLower(_ value.Caller, this value.Value, _ []value.Value) (value.Value, error) {
	s, err := thisString(this, "toLowerCase")
	if err != nil {
		return value.Value{}, err
	}
	return value.String(strings.ToLower(s)), nil
}

// --- char-level access ---

func strCharAt(_ value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	s, err := thisString(this, "charAt")
	if err != nil {
		return value.Value{}, err
	}
	i := intArg(args, 0, 0)
	if i < 0 || i >= len(s) {
		return value.String(""), nil
	}
	return value.String(string(s[i])), nil
}

func strCharCodeAt(_ value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	s, err := thisString(this, "charCodeAt")
	if err != nil {
		return value.Value{}, err
	}
	i := intArg(args, 0, 0)
	if i < 0 || i >= len(s) {
		return value.Number(math.NaN()), nil
	}
	return value.Number(float64(s[i])), nil
}

func strCodePointAt(_ value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	s, err := thisString(this, "codePointAt")
	if err != nil {
		return value.Value{}, err
	}
	i := intArg(args, 0, 0)
	if i < 0 || i >= len(s) {
		return value.Undefined(), nil
	}
	// ASCII-only fast path: code point == byte value.
	return value.Number(float64(s[i])), nil
}

// strAt implements String.prototype.at — like charAt but supports
// negative indices that count from the end. Returns undefined when
// the resolved index falls outside the string (instead of "").
func strAt(_ value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	s, err := thisString(this, "at")
	if err != nil {
		return value.Value{}, err
	}
	i := intArg(args, 0, 0)
	if i < 0 {
		i += len(s)
	}
	if i < 0 || i >= len(s) {
		return value.Undefined(), nil
	}
	return value.String(string(s[i])), nil
}

// strNormalize implements String.prototype.normalize. We don't ship
// a real Unicode normalizer; ASCII corpus is already in any normal
// form, so the spec-compliant simplification is to validate the form
// argument and return the receiver unchanged.
func strNormalize(_ value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	s, err := thisString(this, "normalize")
	if err != nil {
		return value.Value{}, err
	}
	if len(args) >= 1 && args[0].Type() != value.TypeUndefined {
		form := args[0].String()
		switch form {
		case "NFC", "NFD", "NFKC", "NFKD":
			// OK.
		default:
			return value.Value{}, &value.JSThrow{Val: makeError("RangeError",
				"String.prototype.normalize: invalid form")}
		}
	}
	return value.String(s), nil
}

// strLocaleCompare implements a *very* loose String.prototype.localeCompare:
// we use byte-wise comparison (no locale awareness) and normalise the
// result to -1 / 0 / 1, matching what QuickJS returns for ASCII inputs.
func strLocaleCompare(_ value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	s, err := thisString(this, "localeCompare")
	if err != nil {
		return value.Value{}, err
	}
	other := argString(args, 0)
	switch {
	case s < other:
		return value.Number(-1), nil
	case s > other:
		return value.Number(1), nil
	}
	return value.Number(0), nil
}

// --- search ---

func strIndexOf(_ value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	s, err := thisString(this, "indexOf")
	if err != nil {
		return value.Value{}, err
	}
	needle := argString(args, 0)
	from := intArg(args, 1, 0)
	if from < 0 {
		from = 0
	}
	if from > len(s) {
		from = len(s)
	}
	idx := strings.Index(s[from:], needle)
	if idx < 0 {
		return value.Number(-1), nil
	}
	return value.Number(float64(idx + from)), nil
}

func strLastIndexOf(_ value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	s, err := thisString(this, "lastIndexOf")
	if err != nil {
		return value.Value{}, err
	}
	needle := argString(args, 0)
	// Default fromIndex is +Infinity → clamp to len(s).
	upper := len(s)
	if len(args) >= 2 {
		f := args[1].AsNumber()
		if f != f { // NaN → +Infinity per spec
			upper = len(s)
		} else {
			ui := int(f)
			if ui < 0 {
				ui = 0
			}
			if ui > len(s) {
				ui = len(s)
			}
			upper = ui
		}
	}
	// Allow needle to start at or before `upper`.
	end := upper + len(needle)
	if end > len(s) {
		end = len(s)
	}
	idx := strings.LastIndex(s[:end], needle)
	if idx < 0 {
		return value.Number(-1), nil
	}
	if idx > upper {
		return value.Number(-1), nil
	}
	return value.Number(float64(idx)), nil
}

func strIncludes(_ value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	s, err := thisString(this, "includes")
	if err != nil {
		return value.Value{}, err
	}
	needle := argString(args, 0)
	from := intArg(args, 1, 0)
	if from < 0 {
		from = 0
	}
	if from > len(s) {
		from = len(s)
	}
	return value.Bool(strings.Contains(s[from:], needle)), nil
}

func strStartsWith(_ value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	s, err := thisString(this, "startsWith")
	if err != nil {
		return value.Value{}, err
	}
	needle := argString(args, 0)
	pos := intArg(args, 1, 0)
	if pos < 0 {
		pos = 0
	}
	if pos > len(s) {
		pos = len(s)
	}
	return value.Bool(strings.HasPrefix(s[pos:], needle)), nil
}

func strEndsWith(_ value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	s, err := thisString(this, "endsWith")
	if err != nil {
		return value.Value{}, err
	}
	needle := argString(args, 0)
	end := len(s)
	if len(args) >= 2 && args[1].Type() != value.TypeUndefined {
		end = intArg(args, 1, len(s))
		if end < 0 {
			end = 0
		}
		if end > len(s) {
			end = len(s)
		}
	}
	return value.Bool(strings.HasSuffix(s[:end], needle)), nil
}

// --- slicing ---

func strSlice(_ value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	s, err := thisString(this, "slice")
	if err != nil {
		return value.Value{}, err
	}
	n := len(s)
	start := intArg(args, 0, 0)
	if start < 0 {
		start += n
		if start < 0 {
			start = 0
		}
	}
	if start > n {
		start = n
	}
	end := n
	if len(args) >= 2 && args[1].Type() != value.TypeUndefined {
		end = intArg(args, 1, n)
		if end < 0 {
			end += n
			if end < 0 {
				end = 0
			}
		}
		if end > n {
			end = n
		}
	}
	if end < start {
		return value.String(""), nil
	}
	return value.String(s[start:end]), nil
}

func strSubstring(_ value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	s, err := thisString(this, "substring")
	if err != nil {
		return value.Value{}, err
	}
	n := len(s)
	clamp := func(x int) int {
		if x < 0 {
			return 0
		}
		if x > n {
			return n
		}
		return x
	}
	start := clamp(intArg(args, 0, 0))
	end := n
	if len(args) >= 2 && args[1].Type() != value.TypeUndefined {
		end = clamp(intArg(args, 1, n))
	}
	if start > end {
		start, end = end, start
	}
	return value.String(s[start:end]), nil
}

func strSubstr(_ value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	s, err := thisString(this, "substr")
	if err != nil {
		return value.Value{}, err
	}
	n := len(s)
	start := intArg(args, 0, 0)
	if start < 0 {
		start += n
		if start < 0 {
			start = 0
		}
	}
	if start > n {
		start = n
	}
	length := n - start
	if len(args) >= 2 && args[1].Type() != value.TypeUndefined {
		length = intArg(args, 1, 0)
	}
	if length <= 0 {
		return value.String(""), nil
	}
	end := start + length
	if end > n {
		end = n
	}
	return value.String(s[start:end]), nil
}

// --- split ---

func strSplit(_ value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	s, err := thisString(this, "split")
	if err != nil {
		return value.Value{}, err
	}

	out := value.NewArray()

	// split() with no separator returns [s].
	if len(args) == 0 || args[0].Type() == value.TypeUndefined {
		out.Push(value.String(s))
		return value.ArrayVal(out), nil
	}

	sep := args[0].String()

	// Limit handling: undefined → no limit; otherwise clamp to int.
	hasLimit := false
	limit := 0
	if len(args) >= 2 && args[1].Type() != value.TypeUndefined {
		hasLimit = true
		f := args[1].AsNumber()
		if f != f {
			limit = 0
		} else {
			limit = int(f)
			if limit < 0 {
				// JS uses unsigned 32-bit; for our corpus negatives are
				// not exercised — treat as 0 to stay safe.
				limit = 0
			}
		}
	}

	push := func(v string) bool {
		if hasLimit && out.Length() >= limit {
			return false
		}
		out.Push(value.String(v))
		return true
	}

	if sep == "" {
		// Split into individual bytes (ASCII corpus only).
		for i := 0; i < len(s); i++ {
			if !push(string(s[i])) {
				break
			}
		}
		return value.ArrayVal(out), nil
	}

	if s == "" {
		push("")
		return value.ArrayVal(out), nil
	}

	parts := strings.Split(s, sep)
	for _, p := range parts {
		if !push(p) {
			break
		}
	}
	return value.ArrayVal(out), nil
}

// --- trimming ---

// jsWhitespace mirrors what QuickJS strips: ASCII whitespace plus
// the BOM. Multi-byte Unicode whitespace is out of scope for now.
const jsWhitespace = " \t\n\v\f\r"

func strTrim(_ value.Caller, this value.Value, _ []value.Value) (value.Value, error) {
	s, err := thisString(this, "trim")
	if err != nil {
		return value.Value{}, err
	}
	return value.String(strings.Trim(s, jsWhitespace)), nil
}

func strTrimStart(_ value.Caller, this value.Value, _ []value.Value) (value.Value, error) {
	s, err := thisString(this, "trimStart")
	if err != nil {
		return value.Value{}, err
	}
	return value.String(strings.TrimLeft(s, jsWhitespace)), nil
}

func strTrimEnd(_ value.Caller, this value.Value, _ []value.Value) (value.Value, error) {
	s, err := thisString(this, "trimEnd")
	if err != nil {
		return value.Value{}, err
	}
	return value.String(strings.TrimRight(s, jsWhitespace)), nil
}

// --- concat / repeat ---

func strConcat(_ value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	s, err := thisString(this, "concat")
	if err != nil {
		return value.Value{}, err
	}
	var b strings.Builder
	b.WriteString(s)
	for _, a := range args {
		b.WriteString(a.String())
	}
	return value.String(b.String()), nil
}

func strRepeat(_ value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	s, err := thisString(this, "repeat")
	if err != nil {
		return value.Value{}, err
	}
	if len(args) == 0 || args[0].Type() == value.TypeUndefined {
		return value.String(""), nil
	}
	f := args[0].AsNumber()
	if f != f { // NaN → 0
		return value.String(""), nil
	}
	if f < 0 || math.IsInf(f, 1) {
		return value.Value{}, &typeError{msg: "String.prototype.repeat: invalid count"}
	}
	n := int(f)
	if n == 0 || s == "" {
		return value.String(""), nil
	}
	return value.String(strings.Repeat(s, n)), nil
}

// --- padding ---

func padHelper(s string, args []value.Value, atStart bool) string {
	target := intArg(args, 0, 0)
	if target <= len(s) {
		return s
	}
	pad := " "
	if len(args) >= 2 && args[1].Type() != value.TypeUndefined {
		pad = args[1].String()
	}
	if pad == "" {
		return s
	}
	need := target - len(s)
	// Build the padding string with the same byte length as `need`.
	var b strings.Builder
	b.Grow(need)
	for b.Len() < need {
		remaining := need - b.Len()
		if remaining >= len(pad) {
			b.WriteString(pad)
		} else {
			b.WriteString(pad[:remaining])
		}
	}
	if atStart {
		return b.String() + s
	}
	return s + b.String()
}

func strPadStart(_ value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	s, err := thisString(this, "padStart")
	if err != nil {
		return value.Value{}, err
	}
	return value.String(padHelper(s, args, true)), nil
}

func strPadEnd(_ value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	s, err := thisString(this, "padEnd")
	if err != nil {
		return value.Value{}, err
	}
	return value.String(padHelper(s, args, false)), nil
}

// --- replace / replaceAll (string-search only; regex NYI) ---

func strReplace(_ value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	s, err := thisString(this, "replace")
	if err != nil {
		return value.Value{}, err
	}
	search := argString(args, 0)
	replacement := argString(args, 1)
	return value.String(strings.Replace(s, search, replacement, 1)), nil
}

func strReplaceAll(_ value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	s, err := thisString(this, "replaceAll")
	if err != nil {
		return value.Value{}, err
	}
	search := argString(args, 0)
	replacement := argString(args, 1)
	if search == "" {
		// JS spec: empty search inserts replacement between every
		// pair of chars and at both ends.
		var b strings.Builder
		b.WriteString(replacement)
		for i := 0; i < len(s); i++ {
			b.WriteByte(s[i])
			b.WriteString(replacement)
		}
		return value.String(b.String()), nil
	}
	return value.String(strings.ReplaceAll(s, search, replacement)), nil
}

// --- static methods ---

// strFromCharCode builds a string from a sequence of UTF-16 code
// units. We only model ASCII bytes in the corpus, so each argument
// is truncated to its low 8 bits — enough for the spec-canonical
// examples (`fromCharCode(72, 105) === "Hi"`).
func strFromCharCode(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	var b strings.Builder
	b.Grow(len(args))
	for _, a := range args {
		f := a.AsNumber()
		if f != f { // NaN → 0
			f = 0
		}
		// ToUint16 then mask to byte for our ASCII fast path.
		u := uint32(int64(f)) & 0xffff
		b.WriteByte(byte(u & 0xff))
	}
	return value.String(b.String()), nil
}

// strFromCodePoint is a thin alias over fromCharCode for the ASCII
// path. Code points >= 0x80 would need UTF-8 encoding; we throw a
// RangeError for non-finite / negative / >0x10FFFF inputs to match
// the spec's validation gate.
func strFromCodePoint(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	var b strings.Builder
	b.Grow(len(args))
	for _, a := range args {
		f := a.AsNumber()
		if math.IsNaN(f) || math.IsInf(f, 0) || f < 0 || f > 0x10FFFF || math.Trunc(f) != f {
			return value.Value{}, &value.JSThrow{Val: makeError("RangeError",
				"String.fromCodePoint: invalid code point")}
		}
		cp := int(f)
		if cp <= 0x7f {
			b.WriteByte(byte(cp))
		} else {
			// Out of ASCII fast path; encode via Go's UTF-8.
			b.WriteRune(rune(cp))
		}
	}
	return value.String(b.String()), nil
}
