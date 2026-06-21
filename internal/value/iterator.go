package value

// MakeArrayIterator returns a JS iterator object over arr — an object
// with a `next()` method that produces {value, done} per spec. State
// (current index) lives in a Go closure captured by the native fn,
// so each call to MakeArrayIterator is independent. The object's
// only own property is `next`; downstream for-of code keys off that.
func MakeArrayIterator(arr *Array) Value {
	idx := 0
	obj := NewObject()
	obj.Set("next", FunctionVal(&Function{
		Name:  "next",
		Arity: 0,
		Native: func(_ Caller, _ Value, _ []Value) (Value, error) {
			result := NewObject()
			if idx >= arr.Length() {
				result.Set("value", Undefined())
				result.Set("done", Bool(true))
			} else {
				result.Set("value", arr.Get(idx))
				result.Set("done", Bool(false))
				idx++
			}
			return ObjectVal(result), nil
		},
	}))
	return ObjectVal(obj)
}

// MakeStringIterator iterates code units (ASCII fast path; full UTF-16
// support waits for the day a corpus actually needs it).
func MakeStringIterator(s string) Value {
	idx := 0
	obj := NewObject()
	obj.Set("next", FunctionVal(&Function{
		Name:  "next",
		Arity: 0,
		Native: func(_ Caller, _ Value, _ []Value) (Value, error) {
			result := NewObject()
			if idx >= len(s) {
				result.Set("value", Undefined())
				result.Set("done", Bool(true))
			} else {
				result.Set("value", String(string(s[idx])))
				result.Set("done", Bool(false))
				idx++
			}
			return ObjectVal(result), nil
		},
	}))
	return ObjectVal(obj)
}
