package value

import "strings"

// Array backs JS arrays. We use a dense Go slice; sparse arrays
// would fall back to a side map but no corpus exercises them yet.
type Array struct {
	items []Value
}

func NewArray() *Array { return &Array{} }

func NewArrayWithCap(n int) *Array {
	return &Array{items: make([]Value, 0, n)}
}

func (a *Array) Length() int { return len(a.items) }

// Get returns the element at i or Undefined if out of bounds.
func (a *Array) Get(i int) Value {
	if i < 0 || i >= len(a.items) {
		return Undefined()
	}
	return a.items[i]
}

// Set assigns the element at i, extending the array with undefined
// holes when i is past the end (the JS `a[100] = ...` behaviour).
func (a *Array) Set(i int, v Value) {
	if i < 0 {
		return
	}
	if i < len(a.items) {
		a.items[i] = v
		return
	}
	for len(a.items) < i {
		a.items = append(a.items, Undefined())
	}
	a.items = append(a.items, v)
}

// Push appends v and returns the new length.
func (a *Array) Push(v Value) int {
	a.items = append(a.items, v)
	return len(a.items)
}

// Truncate shrinks the array to length n. Used by pop / shift / etc.
// in the builtins package — the only mutators outside this file that
// need to reduce length. n is clamped: < 0 → 0, > current → no-op.
// Trailing slots are zeroed so reference payloads become collectable.
func (a *Array) Truncate(n int) {
	if n < 0 {
		n = 0
	}
	if n >= len(a.items) {
		return
	}
	for i := n; i < len(a.items); i++ {
		a.items[i] = Value{}
	}
	a.items = a.items[:n]
}

// Prop dispatches a property name on an array. .length is computed;
// method names are looked up in ArrayProto (populated by the
// builtins package at init time). Anything else is undefined.
func (a *Array) Prop(name string) Value {
	if name == "length" {
		return Number(float64(a.Length()))
	}
	if fn, ok := ArrayProto[name]; ok {
		return FunctionVal(fn)
	}
	return Undefined()
}

func (a *Array) stringify() string {
	if len(a.items) == 0 {
		return "[]"
	}
	var b strings.Builder
	b.WriteByte('[')
	for i, it := range a.items {
		if i > 0 {
			b.WriteByte(',')
		}
		if it.Type() == TypeUndefined {
			b.WriteString("null")
		} else {
			b.WriteString(it.stringifyForJSON())
		}
	}
	b.WriteByte(']')
	return b.String()
}

// ArrayProto is Array.prototype's method table. Empty at package
// init; the builtins package populates it. Keeping the slot here
// (instead of inside builtins) lets value/ stay a leaf — VM
// dispatch and stringify need no upward import.
var ArrayProto = map[string]*Function{}
