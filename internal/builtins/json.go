// JSON namespace — parse and stringify.
//
// JSON.parse(text)            — tokenise + recursive-descent parse a
//                                JSON document into a JS value tree.
// JSON.stringify(value, repl, space?) — recursively render a value tree
//                                as a JSON document. The optional space
//                                argument (number 1..10, or string up
//                                to 10 chars) triggers pretty-printed
//                                output; otherwise the result is the
//                                same compact form Value.String()
//                                already produces for objects/arrays.
//
// Replacer is parked: the function form needs a Caller round-trip and
// the array form needs key-filtering; neither buys us much corpus yet.
// Reviver on parse is parked for the same reason.

package builtins

import (
	"math"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/vimt/goquickjs/internal/value"
)

func installJSON(globals map[string]value.Value) {
	ns := value.NewObject()
	ns.Set("parse", nativeFn("parse", 2, jsonParseFn))
	ns.Set("stringify", nativeFn("stringify", 3, jsonStringifyFn))
	globals["JSON"] = value.ObjectVal(ns)
}

// --- JSON.parse ----------------------------------------------------------

func jsonParseFn(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	src := argString(args, 0)
	p := &jsonParser{src: src}
	p.skipWS()
	v, err := p.parseValue()
	if err != nil {
		return value.Value{}, &value.JSThrow{Val: makeError("Error", "JSON parse: "+err.Error())}
	}
	p.skipWS()
	if p.pos != len(p.src) {
		return value.Value{}, &value.JSThrow{Val: makeError("Error", "JSON parse: trailing input")}
	}
	return v, nil
}

// jsonParser is a tiny recursive-descent parser. We carry the cursor in
// pos and return a Go error (string only) on malformed input; the
// caller wraps it as a thrown JS Error.
type jsonParser struct {
	src string
	pos int
}

type jsonParseErr string

func (e jsonParseErr) Error() string { return string(e) }

func (p *jsonParser) skipWS() {
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			p.pos++
			continue
		}
		break
	}
}

func (p *jsonParser) parseValue() (value.Value, error) {
	if p.pos >= len(p.src) {
		return value.Value{}, jsonParseErr("unexpected end of input")
	}
	c := p.src[p.pos]
	switch {
	case c == '{':
		return p.parseObject()
	case c == '[':
		return p.parseArray()
	case c == '"':
		s, err := p.parseString()
		if err != nil {
			return value.Value{}, err
		}
		return value.String(s), nil
	case c == 't' || c == 'f':
		return p.parseBool()
	case c == 'n':
		return p.parseNull()
	case c == '-' || (c >= '0' && c <= '9'):
		return p.parseNumber()
	}
	return value.Value{}, jsonParseErr("unexpected character")
}

func (p *jsonParser) parseObject() (value.Value, error) {
	p.pos++ // consume '{'
	obj := value.NewObject()
	p.skipWS()
	if p.pos < len(p.src) && p.src[p.pos] == '}' {
		p.pos++
		return value.ObjectVal(obj), nil
	}
	for {
		p.skipWS()
		if p.pos >= len(p.src) || p.src[p.pos] != '"' {
			return value.Value{}, jsonParseErr("expected string key")
		}
		key, err := p.parseString()
		if err != nil {
			return value.Value{}, err
		}
		p.skipWS()
		if p.pos >= len(p.src) || p.src[p.pos] != ':' {
			return value.Value{}, jsonParseErr("expected ':' after key")
		}
		p.pos++
		p.skipWS()
		val, err := p.parseValue()
		if err != nil {
			return value.Value{}, err
		}
		obj.Set(key, val)
		p.skipWS()
		if p.pos >= len(p.src) {
			return value.Value{}, jsonParseErr("unterminated object")
		}
		if p.src[p.pos] == ',' {
			p.pos++
			continue
		}
		if p.src[p.pos] == '}' {
			p.pos++
			return value.ObjectVal(obj), nil
		}
		return value.Value{}, jsonParseErr("expected ',' or '}' in object")
	}
}

func (p *jsonParser) parseArray() (value.Value, error) {
	p.pos++ // consume '['
	arr := value.NewArray()
	p.skipWS()
	if p.pos < len(p.src) && p.src[p.pos] == ']' {
		p.pos++
		return value.ArrayVal(arr), nil
	}
	for {
		p.skipWS()
		val, err := p.parseValue()
		if err != nil {
			return value.Value{}, err
		}
		arr.Push(val)
		p.skipWS()
		if p.pos >= len(p.src) {
			return value.Value{}, jsonParseErr("unterminated array")
		}
		if p.src[p.pos] == ',' {
			p.pos++
			continue
		}
		if p.src[p.pos] == ']' {
			p.pos++
			return value.ArrayVal(arr), nil
		}
		return value.Value{}, jsonParseErr("expected ',' or ']' in array")
	}
}

func (p *jsonParser) parseString() (string, error) {
	// caller positioned at the opening quote.
	p.pos++
	var b strings.Builder
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		if c == '"' {
			p.pos++
			return b.String(), nil
		}
		if c == '\\' {
			p.pos++
			if p.pos >= len(p.src) {
				return "", jsonParseErr("bad escape")
			}
			esc := p.src[p.pos]
			p.pos++
			switch esc {
			case '"':
				b.WriteByte('"')
			case '\\':
				b.WriteByte('\\')
			case '/':
				b.WriteByte('/')
			case 'b':
				b.WriteByte('\b')
			case 'f':
				b.WriteByte('\f')
			case 'n':
				b.WriteByte('\n')
			case 'r':
				b.WriteByte('\r')
			case 't':
				b.WriteByte('\t')
			case 'u':
				if p.pos+4 > len(p.src) {
					return "", jsonParseErr("bad \\u escape")
				}
				hex := p.src[p.pos : p.pos+4]
				p.pos += 4
				n, err := strconv.ParseUint(hex, 16, 32)
				if err != nil {
					return "", jsonParseErr("bad \\u escape")
				}
				r := rune(n)
				// Surrogate pair handling.
				if utf16.IsSurrogate(r) && p.pos+6 <= len(p.src) &&
					p.src[p.pos] == '\\' && p.src[p.pos+1] == 'u' {
					hex2 := p.src[p.pos+2 : p.pos+6]
					if n2, err2 := strconv.ParseUint(hex2, 16, 32); err2 == nil {
						r2 := rune(n2)
						if utf16.IsSurrogate(r2) {
							combined := utf16.DecodeRune(r, r2)
							if combined != utf8.RuneError {
								p.pos += 6
								b.WriteRune(combined)
								continue
							}
						}
					}
				}
				b.WriteRune(r)
			default:
				return "", jsonParseErr("bad escape")
			}
			continue
		}
		if c < 0x20 {
			return "", jsonParseErr("control char in string")
		}
		b.WriteByte(c)
		p.pos++
	}
	return "", jsonParseErr("unterminated string")
}

func (p *jsonParser) parseBool() (value.Value, error) {
	if strings.HasPrefix(p.src[p.pos:], "true") {
		p.pos += 4
		return value.Bool(true), nil
	}
	if strings.HasPrefix(p.src[p.pos:], "false") {
		p.pos += 5
		return value.Bool(false), nil
	}
	return value.Value{}, jsonParseErr("invalid literal")
}

func (p *jsonParser) parseNull() (value.Value, error) {
	if strings.HasPrefix(p.src[p.pos:], "null") {
		p.pos += 4
		return value.Null(), nil
	}
	return value.Value{}, jsonParseErr("invalid literal")
}

func (p *jsonParser) parseNumber() (value.Value, error) {
	start := p.pos
	if p.src[p.pos] == '-' {
		p.pos++
	}
	if p.pos >= len(p.src) {
		return value.Value{}, jsonParseErr("bad number")
	}
	// Integer part.
	if p.src[p.pos] == '0' {
		p.pos++
	} else if p.src[p.pos] >= '1' && p.src[p.pos] <= '9' {
		for p.pos < len(p.src) && p.src[p.pos] >= '0' && p.src[p.pos] <= '9' {
			p.pos++
		}
	} else {
		return value.Value{}, jsonParseErr("bad number")
	}
	// Fractional part.
	if p.pos < len(p.src) && p.src[p.pos] == '.' {
		p.pos++
		hasDigits := false
		for p.pos < len(p.src) && p.src[p.pos] >= '0' && p.src[p.pos] <= '9' {
			p.pos++
			hasDigits = true
		}
		if !hasDigits {
			return value.Value{}, jsonParseErr("bad number")
		}
	}
	// Exponent.
	if p.pos < len(p.src) && (p.src[p.pos] == 'e' || p.src[p.pos] == 'E') {
		p.pos++
		if p.pos < len(p.src) && (p.src[p.pos] == '+' || p.src[p.pos] == '-') {
			p.pos++
		}
		hasDigits := false
		for p.pos < len(p.src) && p.src[p.pos] >= '0' && p.src[p.pos] <= '9' {
			p.pos++
			hasDigits = true
		}
		if !hasDigits {
			return value.Value{}, jsonParseErr("bad number")
		}
	}
	f, err := strconv.ParseFloat(p.src[start:p.pos], 64)
	if err != nil {
		return value.Value{}, jsonParseErr("bad number")
	}
	return value.Number(f), nil
}

// --- JSON.stringify ------------------------------------------------------

func jsonStringifyFn(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	v := argOrUndef(args, 0)
	// Replacer (args[1]) is ignored — function/array forms are parked.
	indent := ""
	if len(args) >= 3 {
		sp := args[2]
		switch sp.Type() {
		case value.TypeNumber:
			n := int(sp.AsNumber())
			if n > 10 {
				n = 10
			}
			if n > 0 {
				indent = strings.Repeat(" ", n)
			}
		case value.TypeString:
			s := sp.AsString()
			if len(s) > 10 {
				s = s[:10]
			}
			indent = s
		}
	}
	seen := map[*value.Object]bool{}
	seenA := map[*value.Array]bool{}
	s, omit, err := jsonSerialize(v, indent, "", seen, seenA)
	if err != nil {
		return value.Value{}, err
	}
	if omit {
		return value.Undefined(), nil
	}
	return value.String(s), nil
}

// jsonSerialize renders v at the given indentation depth. Returns
// (output, omit). When omit is true the parent should treat this value
// as "drop me" — top-level callers convert that to undefined, object
// callers skip the key, array callers substitute null.
func jsonSerialize(v value.Value, indent, curIndent string, seenObj map[*value.Object]bool, seenArr map[*value.Array]bool) (string, bool, error) {
	switch v.Type() {
	case value.TypeUndefined:
		return "", true, nil
	case value.TypeNull:
		return "null", false, nil
	case value.TypeBool:
		if v.AsBool() {
			return "true", false, nil
		}
		return "false", false, nil
	case value.TypeNumber:
		return jsonNumber(v.AsNumber()), false, nil
	case value.TypeString:
		return jsonQuote(v.AsString()), false, nil
	case value.TypeFunction:
		return "", true, nil
	case value.TypeArray:
		a := v.AsArray()
		if seenArr[a] {
			return "", false, &value.JSThrow{Val: makeError("TypeError", "Converting circular structure to JSON")}
		}
		seenArr[a] = true
		defer delete(seenArr, a)
		s, err := jsonArray(a, indent, curIndent, seenObj, seenArr)
		if err != nil {
			return "", false, err
		}
		return s, false, nil
	case value.TypeObject:
		o := v.AsObject()
		if seenObj[o] {
			return "", false, &value.JSThrow{Val: makeError("TypeError", "Converting circular structure to JSON")}
		}
		seenObj[o] = true
		defer delete(seenObj, o)
		s, err := jsonObject(o, indent, curIndent, seenObj, seenArr)
		if err != nil {
			return "", false, err
		}
		return s, false, nil
	}
	return "", true, nil
}

// jsonNumber matches the spec ToString for finite numbers and emits
// "null" for NaN/Inf. -0 is rendered as "0" (the JSON variant of
// formatNumber — Value.String() prints "-0" for the REPL).
func jsonNumber(f float64) string {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return "null"
	}
	if f == 0 {
		return "0"
	}
	const safeInt = 9.007199254740992e15
	if f >= -safeInt && f <= safeInt && f == float64(int64(f)) {
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'g', -1, 64)
}

// jsonQuote wraps s in double quotes with JSON escaping (delegates to
// the shared jsonEscape via Value.stringifyForJSON, but we need raw
// access from inside builtins — duplicate the small logic here).
func jsonQuote(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte('"')
	const hex = "0123456789abcdef"
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
				b.WriteString(`\u00`)
				b.WriteByte(hex[c>>4])
				b.WriteByte(hex[c&0xf])
			} else {
				b.WriteByte(c)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

func jsonArray(a *value.Array, indent, curIndent string, seenObj map[*value.Object]bool, seenArr map[*value.Array]bool) (string, error) {
	if a.Length() == 0 {
		return "[]", nil
	}
	pretty := indent != ""
	newIndent := curIndent + indent
	var b strings.Builder
	b.WriteByte('[')
	for i := 0; i < a.Length(); i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		if pretty {
			b.WriteByte('\n')
			b.WriteString(newIndent)
		}
		s, omit, err := jsonSerialize(a.Get(i), indent, newIndent, seenObj, seenArr)
		if err != nil {
			return "", err
		}
		if omit {
			b.WriteString("null")
		} else {
			b.WriteString(s)
		}
	}
	if pretty {
		b.WriteByte('\n')
		b.WriteString(curIndent)
	}
	b.WriteByte(']')
	return b.String(), nil
}

func jsonObject(o *value.Object, indent, curIndent string, seenObj map[*value.Object]bool, seenArr map[*value.Array]bool) (string, error) {
	names := o.PropNames()
	type kv struct {
		k, v string
	}
	pairs := make([]kv, 0, len(names))
	newIndent := curIndent + indent
	for _, name := range names {
		val, _ := o.GetOwn(name)
		s, omit, err := jsonSerialize(val, indent, newIndent, seenObj, seenArr)
		if err != nil {
			return "", err
		}
		if omit {
			continue
		}
		pairs = append(pairs, kv{k: name, v: s})
	}
	if len(pairs) == 0 {
		return "{}", nil
	}
	pretty := indent != ""
	var b strings.Builder
	b.WriteByte('{')
	sep := ":"
	if pretty {
		sep = ": "
	}
	for i, p := range pairs {
		if i > 0 {
			b.WriteByte(',')
		}
		if pretty {
			b.WriteByte('\n')
			b.WriteString(newIndent)
		}
		b.WriteString(jsonQuote(p.k))
		b.WriteString(sep)
		b.WriteString(p.v)
	}
	if pretty {
		b.WriteByte('\n')
		b.WriteString(curIndent)
	}
	b.WriteByte('}')
	return b.String(), nil
}
