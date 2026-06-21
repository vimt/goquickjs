package value

// StringProp dispatches a property on a string primitive. .length
// is computed; method names are looked up in StringProto.
func StringProp(s, name string) Value {
	if name == "length" {
		return Number(float64(len(s)))
	}
	if fn, ok := StringProto[name]; ok {
		return FunctionVal(fn)
	}
	return Undefined()
}

// StringProto is String.prototype's method table; see ArrayProto.
var StringProto = map[string]*Function{}

// NumberProp dispatches a property on a number primitive.
func NumberProp(name string) Value {
	if fn, ok := NumberProto[name]; ok {
		return FunctionVal(fn)
	}
	return Undefined()
}

// NumberProto is Number.prototype's method table; see ArrayProto.
var NumberProto = map[string]*Function{}
