// Date constructor + Date.prototype methods.
//
// A Date instance is just an Object whose [[Prototype]] is the shared
// datePrototype, plus one own slot "__ms__" holding the ms-since-epoch
// as a Number (matches the spec's [[DateValue]] internal slot). All
// getter methods read __ms__ off `this` and decode UTC fields off it
// — we deliberately avoid the host's local timezone because the
// differ runs against an oracle that may sit in a different zone.
//
// Date.now() is excluded from the corpus because both engines return
// real wall-clock time and would never diff-match.

package builtins

import (
	"math"
	"strconv"
	"time"

	"github.com/vimt/goquickjs/internal/value"
)

var datePrototype *value.Object

func installDate(globals map[string]value.Value) {
	datePrototype = value.NewObject()

	// Prototype methods — every one expects `this` to be a Date instance
	// (an object carrying __ms__). For a "this is NaN" instance the
	// getters return NaN to match spec; toISOString throws RangeError.
	datePrototype.Set("getTime", nativeFn("getTime", 0, dateGetTime))
	datePrototype.Set("valueOf", nativeFn("valueOf", 0, dateGetTime))
	datePrototype.Set("getFullYear", nativeFn("getFullYear", 0, dateGetUTCFullYear))
	datePrototype.Set("getMonth", nativeFn("getMonth", 0, dateGetUTCMonth))
	datePrototype.Set("getDate", nativeFn("getDate", 0, dateGetUTCDate))
	datePrototype.Set("getDay", nativeFn("getDay", 0, dateGetUTCDay))
	datePrototype.Set("getHours", nativeFn("getHours", 0, dateGetUTCHours))
	datePrototype.Set("getMinutes", nativeFn("getMinutes", 0, dateGetUTCMinutes))
	datePrototype.Set("getSeconds", nativeFn("getSeconds", 0, dateGetUTCSeconds))
	datePrototype.Set("getMilliseconds", nativeFn("getMilliseconds", 0, dateGetUTCMilliseconds))

	datePrototype.Set("getUTCFullYear", nativeFn("getUTCFullYear", 0, dateGetUTCFullYear))
	datePrototype.Set("getUTCMonth", nativeFn("getUTCMonth", 0, dateGetUTCMonth))
	datePrototype.Set("getUTCDate", nativeFn("getUTCDate", 0, dateGetUTCDate))
	datePrototype.Set("getUTCDay", nativeFn("getUTCDay", 0, dateGetUTCDay))
	datePrototype.Set("getUTCHours", nativeFn("getUTCHours", 0, dateGetUTCHours))
	datePrototype.Set("getUTCMinutes", nativeFn("getUTCMinutes", 0, dateGetUTCMinutes))
	datePrototype.Set("getUTCSeconds", nativeFn("getUTCSeconds", 0, dateGetUTCSeconds))
	datePrototype.Set("getUTCMilliseconds", nativeFn("getUTCMilliseconds", 0, dateGetUTCMilliseconds))

	datePrototype.Set("getTimezoneOffset", nativeFn("getTimezoneOffset", 0, dateGetTimezoneOffset))

	datePrototype.Set("toISOString", nativeFn("toISOString", 0, dateToISOString))
	datePrototype.Set("toJSON", nativeFn("toJSON", 0, dateToISOString))
	datePrototype.Set("toString", nativeFn("toString", 0, dateToString))

	// Constructor function.
	ctor := &value.Function{
		Name:   "Date",
		Arity:  7,
		Native: dateConstructor,
	}
	ctor.Props = value.NewObject()
	ctor.Props.Set("prototype", value.ObjectVal(datePrototype))
	ctor.Props.Set("now", nativeFn("now", 0, dateNow))
	ctor.Props.Set("parse", nativeFn("parse", 1, dateParse))
	ctor.Props.Set("UTC", nativeFn("UTC", 7, dateUTC))

	globals["Date"] = value.FunctionVal(ctor)
}

// --- internal helpers ---

// dateMS reads the __ms__ slot of `this`. Returns (NaN, false) when
// `this` is not a Date-shaped object — the caller decides whether to
// throw or just propagate NaN.
func dateMS(this value.Value) (float64, bool) {
	if this.Type() != value.TypeObject {
		return math.NaN(), false
	}
	v, _ := this.AsObject().Get("__ms__")
	if v.Type() != value.TypeNumber {
		return math.NaN(), false
	}
	return v.AsNumber(), true
}

// toUTC turns ms-since-epoch into a Go time.Time in UTC. Caller
// must guard against NaN first.
func toUTC(ms float64) time.Time {
	// Split into whole seconds + nanos to avoid float64 precision
	// loss on the ns axis for very large ms.
	sec := int64(math.Floor(ms / 1000))
	nano := int64(math.Floor(ms-float64(sec)*1000)) * 1_000_000
	return time.Unix(sec, nano).UTC()
}

// --- Date constructor ---

func dateConstructor(_ value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	// When called as `new Date(...)` `this` is the freshly-allocated
	// instance; when called as `Date(...)` (no new) we still return an
	// instance — both engines just stash __ms__ on a fresh object.
	var obj *value.Object
	if this.Type() == value.TypeObject {
		obj = this.AsObject()
	} else {
		obj = value.NewObject()
	}
	obj.SetProto(datePrototype)

	var ms float64
	switch {
	case len(args) == 0:
		ms = float64(time.Now().UnixNano()) / 1e6
	case len(args) == 1:
		a := args[0]
		switch a.Type() {
		case value.TypeNumber:
			ms = a.AsNumber()
		case value.TypeString:
			ms = parseISO(a.AsString())
		default:
			ms = a.AsNumber()
		}
	default:
		// Field constructor: (year, month, day=1, hours=0, ...). The
		// real spec uses local time; we use UTC because it matches the
		// oracle bit-for-bit independent of TZ.
		y := argNumber(args, 0)
		mo := argNumber(args, 1)
		d := 1.0
		if len(args) >= 3 {
			d = argNumber(args, 2)
		}
		h := 0.0
		if len(args) >= 4 {
			h = argNumber(args, 3)
		}
		mi := 0.0
		if len(args) >= 5 {
			mi = argNumber(args, 4)
		}
		s := 0.0
		if len(args) >= 6 {
			s = argNumber(args, 5)
		}
		msArg := 0.0
		if len(args) >= 7 {
			msArg = argNumber(args, 6)
		}
		ms = makeUTC(y, mo, d, h, mi, s, msArg)
	}

	obj.Set("__ms__", value.Number(ms))
	return value.ObjectVal(obj), nil
}

// dateNow is excluded from the differ corpus (real time) but still
// implemented for runtime completeness.
func dateNow(_ value.Caller, _ value.Value, _ []value.Value) (value.Value, error) {
	return value.Number(float64(time.Now().UnixNano()) / 1e6), nil
}

func dateParse(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	return value.Number(parseISO(argString(args, 0))), nil
}

func dateUTC(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	if len(args) < 2 {
		return value.Number(math.NaN()), nil
	}
	y := argNumber(args, 0)
	mo := argNumber(args, 1)
	d := 1.0
	if len(args) >= 3 {
		d = argNumber(args, 2)
	}
	h := 0.0
	if len(args) >= 4 {
		h = argNumber(args, 3)
	}
	mi := 0.0
	if len(args) >= 5 {
		mi = argNumber(args, 4)
	}
	s := 0.0
	if len(args) >= 6 {
		s = argNumber(args, 5)
	}
	ms := 0.0
	if len(args) >= 7 {
		ms = argNumber(args, 6)
	}
	return value.Number(makeUTC(y, mo, d, h, mi, s, ms)), nil
}

// makeUTC mirrors ES MakeDate(MakeDay(year,month,date), MakeTime(...))
// using time.Date in UTC. Two-digit years 0..99 get the +1900 fixup
// per spec.
func makeUTC(y, mo, d, h, mi, s, ms float64) float64 {
	if isNaNAny(y, mo, d, h, mi, s, ms) {
		return math.NaN()
	}
	year := int(y)
	if year >= 0 && year <= 99 {
		year += 1900
	}
	t := time.Date(year, time.Month(int(mo)+1), int(d), int(h), int(mi), int(s), int(ms)*1_000_000, time.UTC)
	return float64(t.UnixNano()) / 1e6
}

func isNaNAny(xs ...float64) bool {
	for _, x := range xs {
		if x != x {
			return true
		}
	}
	return false
}

// parseISO accepts a subset of ISO 8601: "YYYY-MM-DDTHH:MM:SSZ",
// "YYYY-MM-DDTHH:MM:SS.sssZ", or just "YYYY-MM-DD" (interpreted as
// UTC midnight). On failure returns NaN to mirror Date.parse spec.
func parseISO(s string) float64 {
	layouts := []string{
		"2006-01-02T15:04:05.000Z07:00",
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02T15:04:05.000Z",
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05",
		"2006-01-02",
	}
	for _, lo := range layouts {
		if t, err := time.Parse(lo, s); err == nil {
			return float64(t.UnixNano()) / 1e6
		}
	}
	return math.NaN()
}

// --- prototype methods ---

func dateGetTime(_ value.Caller, this value.Value, _ []value.Value) (value.Value, error) {
	ms, ok := dateMS(this)
	if !ok {
		return value.Value{}, badThis("Date.prototype.getTime", "Date")
	}
	return value.Number(ms), nil
}

func dateGetUTCFullYear(_ value.Caller, this value.Value, _ []value.Value) (value.Value, error) {
	ms, ok := dateMS(this)
	if !ok {
		return value.Value{}, badThis("Date.prototype.getUTCFullYear", "Date")
	}
	if math.IsNaN(ms) {
		return value.Number(math.NaN()), nil
	}
	return value.Number(float64(toUTC(ms).Year())), nil
}

func dateGetUTCMonth(_ value.Caller, this value.Value, _ []value.Value) (value.Value, error) {
	ms, ok := dateMS(this)
	if !ok {
		return value.Value{}, badThis("Date.prototype.getUTCMonth", "Date")
	}
	if math.IsNaN(ms) {
		return value.Number(math.NaN()), nil
	}
	return value.Number(float64(int(toUTC(ms).Month()) - 1)), nil
}

func dateGetUTCDate(_ value.Caller, this value.Value, _ []value.Value) (value.Value, error) {
	ms, ok := dateMS(this)
	if !ok {
		return value.Value{}, badThis("Date.prototype.getUTCDate", "Date")
	}
	if math.IsNaN(ms) {
		return value.Number(math.NaN()), nil
	}
	return value.Number(float64(toUTC(ms).Day())), nil
}

func dateGetUTCDay(_ value.Caller, this value.Value, _ []value.Value) (value.Value, error) {
	ms, ok := dateMS(this)
	if !ok {
		return value.Value{}, badThis("Date.prototype.getUTCDay", "Date")
	}
	if math.IsNaN(ms) {
		return value.Number(math.NaN()), nil
	}
	// Go's Weekday(): Sunday = 0, ..., Saturday = 6 — same as JS.
	return value.Number(float64(int(toUTC(ms).Weekday()))), nil
}

func dateGetUTCHours(_ value.Caller, this value.Value, _ []value.Value) (value.Value, error) {
	ms, ok := dateMS(this)
	if !ok {
		return value.Value{}, badThis("Date.prototype.getUTCHours", "Date")
	}
	if math.IsNaN(ms) {
		return value.Number(math.NaN()), nil
	}
	return value.Number(float64(toUTC(ms).Hour())), nil
}

func dateGetUTCMinutes(_ value.Caller, this value.Value, _ []value.Value) (value.Value, error) {
	ms, ok := dateMS(this)
	if !ok {
		return value.Value{}, badThis("Date.prototype.getUTCMinutes", "Date")
	}
	if math.IsNaN(ms) {
		return value.Number(math.NaN()), nil
	}
	return value.Number(float64(toUTC(ms).Minute())), nil
}

func dateGetUTCSeconds(_ value.Caller, this value.Value, _ []value.Value) (value.Value, error) {
	ms, ok := dateMS(this)
	if !ok {
		return value.Value{}, badThis("Date.prototype.getUTCSeconds", "Date")
	}
	if math.IsNaN(ms) {
		return value.Number(math.NaN()), nil
	}
	return value.Number(float64(toUTC(ms).Second())), nil
}

func dateGetUTCMilliseconds(_ value.Caller, this value.Value, _ []value.Value) (value.Value, error) {
	ms, ok := dateMS(this)
	if !ok {
		return value.Value{}, badThis("Date.prototype.getUTCMilliseconds", "Date")
	}
	if math.IsNaN(ms) {
		return value.Number(math.NaN()), nil
	}
	return value.Number(float64(toUTC(ms).Nanosecond() / 1_000_000)), nil
}

// getTimezoneOffset: we treat Date instances as UTC so the offset is
// always 0. Real JS returns host tz offset which the differ can't
// pin down — the corpus skips it.
func dateGetTimezoneOffset(_ value.Caller, this value.Value, _ []value.Value) (value.Value, error) {
	_, ok := dateMS(this)
	if !ok {
		return value.Value{}, badThis("Date.prototype.getTimezoneOffset", "Date")
	}
	return value.Number(0), nil
}

func dateToISOString(_ value.Caller, this value.Value, _ []value.Value) (value.Value, error) {
	ms, ok := dateMS(this)
	if !ok {
		return value.Value{}, badThis("Date.prototype.toISOString", "Date")
	}
	if math.IsNaN(ms) || math.IsInf(ms, 0) {
		return value.Value{}, &value.JSThrow{Val: makeError("RangeError", "Invalid time value")}
	}
	t := toUTC(ms)
	// "2006-01-02T15:04:05.000Z" — Go's reference layout uses .000 to
	// force three milli digits.
	year := t.Year()
	yearStr := ""
	switch {
	case year >= 0 && year <= 9999:
		yearStr = pad(year, 4)
	case year < 0:
		yearStr = "-" + pad(-year, 6)
	default:
		yearStr = "+" + pad(year, 6)
	}
	out := yearStr + "-" +
		pad(int(t.Month()), 2) + "-" +
		pad(t.Day(), 2) + "T" +
		pad(t.Hour(), 2) + ":" +
		pad(t.Minute(), 2) + ":" +
		pad(t.Second(), 2) + "." +
		pad(t.Nanosecond()/1_000_000, 3) + "Z"
	return value.String(out), nil
}

func dateToString(_ value.Caller, this value.Value, _ []value.Value) (value.Value, error) {
	// Spec says local-time human format. Our Date is UTC-internal so
	// we just punt to toISOString for now; the corpus skips this one.
	return dateToISOString(nil, this, nil)
}

func pad(n, width int) string {
	s := strconv.Itoa(n)
	for len(s) < width {
		s = "0" + s
	}
	return s
}
