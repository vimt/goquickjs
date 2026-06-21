// RegExp constructor and prototype methods. Backed by Go's regexp
// package (RE2), which differs from ECMAScript regex syntax in
// places — lookbehind / lookahead / backreferences aren't supported.
// Patterns that fit RE2 cover the vast majority of real-world use.
//
// JS flags handled:
//
//	g — global (.exec advances internal lastIndex; String.replace
//	    replaces all matches).
//	i — case-insensitive (compiled into the pattern).
//	m — multiline (^ / $ match per-line).
//	s — dotall (. matches newline).
//	u/y — accepted but ignored; emit no warning.
package builtins

import (
	"regexp"
	"strings"

	"github.com/vimt/goquickjs/internal/value"
)

func installRegExp(globals map[string]value.Value) {
	ctor := value.NewObject()
	native := nativeFn("RegExp", 2, regexpConstruct)
	// new RegExp(...) walks the .prototype slot for its proto; expose
	// it so test / exec resolve in user code.
	fn := native.AsFunction()
	if fn.Props == nil {
		fn.Props = value.NewBareObject()
	}
	proto := value.NewObject()
	proto.Set("test", nativeFn("test", 1, regexpTest))
	proto.Set("exec", nativeFn("exec", 1, regexpExec))
	proto.Set("toString", nativeFn("toString", 0, regexpToString))
	fn.Props.Set("prototype", value.ObjectVal(proto))
	globals["RegExp"] = native
	_ = ctor
}

// regexpConstruct: `new RegExp(pat, flags)` or `RegExp(pat, flags)`.
// Always returns a fresh wrapper object with internal _re slot.
// `this` (when called via `new`) is the empty instance whose proto
// already points at RegExp.prototype; we just decorate it.
func regexpConstruct(_ value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	pat := argString(args, 0)
	flags := argString(args, 1)
	re, err := compileJSRegex(pat, flags)
	if err != nil {
		return value.Value{}, &value.JSThrow{Val: makeError("SyntaxError", "RegExp: "+err.Error())}
	}
	var inst *value.Object
	if this.Type() == value.TypeObject {
		inst = this.AsObject()
	} else {
		inst = value.NewObject()
	}
	inst.Set("source", value.String(pat))
	inst.Set("flags", value.String(flags))
	inst.Set("global", value.Bool(strings.ContainsRune(flags, 'g')))
	inst.Set("ignoreCase", value.Bool(strings.ContainsRune(flags, 'i')))
	inst.Set("multiline", value.Bool(strings.ContainsRune(flags, 'm')))
	inst.Set("lastIndex", value.Number(0))
	// Store the compiled regex via a closure-bearing native fn. This
	// is the slot test/exec read.
	inst.Set("__re", value.FunctionVal(&value.Function{
		Name: "__re",
		Native: func(_ value.Caller, _ value.Value, _ []value.Value) (value.Value, error) {
			return value.Undefined(), nil
		},
	}))
	storeRegex(inst, re)
	return value.ObjectVal(inst), nil
}

// goRegexCache hangs the compiled *regexp.Regexp off the wrapper
// using a sidecar map keyed by *Object identity. Going through a
// sidecar instead of an opaque value-typed slot avoids leaking the
// Go pointer through user-facing properties.
var goRegexCache = map[*value.Object]*regexp.Regexp{}

func storeRegex(o *value.Object, re *regexp.Regexp) { goRegexCache[o] = re }
func loadRegex(o *value.Object) *regexp.Regexp     { return goRegexCache[o] }

// compileJSRegex translates JS flag string into a Go RE2 pattern
// prefix and compiles it. Unsupported JS-only syntax surfaces as the
// Go regexp error (caller wraps as SyntaxError).
func compileJSRegex(pat, flags string) (*regexp.Regexp, error) {
	var pre strings.Builder
	pre.WriteString("(?")
	wrote := false
	if strings.ContainsRune(flags, 'i') {
		pre.WriteByte('i')
		wrote = true
	}
	if strings.ContainsRune(flags, 'm') {
		pre.WriteByte('m')
		wrote = true
	}
	if strings.ContainsRune(flags, 's') {
		pre.WriteByte('s')
		wrote = true
	}
	pre.WriteByte(')')
	full := pat
	if wrote {
		full = pre.String() + pat
	}
	return regexp.Compile(full)
}

func regexpTest(_ value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	if this.Type() != value.TypeObject {
		return value.Value{}, badThis("RegExp.prototype.test", "RegExp")
	}
	re := loadRegex(this.AsObject())
	if re == nil {
		return value.Bool(false), nil
	}
	return value.Bool(re.MatchString(argString(args, 0))), nil
}

func regexpExec(_ value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	if this.Type() != value.TypeObject {
		return value.Value{}, badThis("RegExp.prototype.exec", "RegExp")
	}
	inst := this.AsObject()
	re := loadRegex(inst)
	if re == nil {
		return value.Null(), nil
	}
	s := argString(args, 0)
	start := 0
	if g, _ := inst.GetOwn("global"); g.Type() == value.TypeBool && g.AsBool() {
		if li, _ := inst.GetOwn("lastIndex"); li.Type() == value.TypeNumber {
			start = int(li.AsNumber())
			if start < 0 || start > len(s) {
				inst.Set("lastIndex", value.Number(0))
				return value.Null(), nil
			}
		}
	}
	loc := re.FindStringSubmatchIndex(s[start:])
	if loc == nil {
		// For global, spec resets lastIndex to 0 on miss.
		if g, _ := inst.GetOwn("global"); g.Type() == value.TypeBool && g.AsBool() {
			inst.Set("lastIndex", value.Number(0))
		}
		return value.Null(), nil
	}
	matchStart := start + loc[0]
	matchEnd := start + loc[1]
	if g, _ := inst.GetOwn("global"); g.Type() == value.TypeBool && g.AsBool() {
		inst.Set("lastIndex", value.Number(float64(matchEnd)))
	}
	out := value.NewArray()
	out.Push(value.String(s[matchStart:matchEnd]))
	for i := 1; i*2 < len(loc); i++ {
		gs, ge := loc[i*2], loc[i*2+1]
		if gs == -1 {
			out.Push(value.Undefined())
		} else {
			out.Push(value.String(s[start+gs : start+ge]))
		}
	}
	return value.ArrayVal(out), nil
}

func regexpToString(_ value.Caller, this value.Value, _ []value.Value) (value.Value, error) {
	if this.Type() != value.TypeObject {
		return value.Value{}, badThis("RegExp.prototype.toString", "RegExp")
	}
	o := this.AsObject()
	srcV, _ := o.GetOwn("source")
	flagsV, _ := o.GetOwn("flags")
	return value.String("/" + srcV.String() + "/" + flagsV.String()), nil
}
