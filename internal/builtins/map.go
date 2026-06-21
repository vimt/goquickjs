// Map constructor + Map.prototype methods.
//
// A Map instance is a plain Object whose [[Prototype]] is mapProto.
// Internal state lives on two own slots:
//
//   __entries__  : a value.Array storing [k1, v1, k2, v2, ...]
//                  in insertion order. We keep keys and values
//                  interleaved (rather than two parallel arrays)
//                  so we never have to keep two Array refs in sync.
//   size         : a number maintained on every set/delete/clear,
//                  so user code can read `m.size` directly without
//                  the engine having to model real ES getters.
//
// Key equality is SameValueZero: it's strict equality with the one
// concession that NaN equals NaN, matching the real Map spec.
//
// Iterator methods (keys / values / entries) return plain Arrays
// instead of proper Iterator objects — we have no iterator protocol
// yet, but `forEach` and `.size` cover the common use cases.

package builtins

import (
	"math"

	"github.com/vimt/goquickjs/internal/value"
)

var mapProto *value.Object

func installMap(globals map[string]value.Value) {
	mapProto = value.NewObject()
	mapProto.Set("set", value.FunctionVal(&value.Function{Name: "set", Arity: 2, Native: mapSet}))
	mapProto.Set("get", value.FunctionVal(&value.Function{Name: "get", Arity: 1, Native: mapGet}))
	mapProto.Set("has", value.FunctionVal(&value.Function{Name: "has", Arity: 1, Native: mapHas}))
	mapProto.Set("delete", value.FunctionVal(&value.Function{Name: "delete", Arity: 1, Native: mapDelete}))
	mapProto.Set("clear", value.FunctionVal(&value.Function{Name: "clear", Arity: 0, Native: mapClear}))
	mapProto.Set("forEach", value.FunctionVal(&value.Function{Name: "forEach", Arity: 1, Native: mapForEach}))
	mapProto.Set("keys", value.FunctionVal(&value.Function{Name: "keys", Arity: 0, Native: mapKeys}))
	mapProto.Set("values", value.FunctionVal(&value.Function{Name: "values", Arity: 0, Native: mapValues}))
	mapProto.Set("entries", value.FunctionVal(&value.Function{Name: "entries", Arity: 0, Native: mapEntries}))

	ctor := &value.Function{
		Name:   "Map",
		Arity:  0,
		Native: mapConstructor,
	}
	ctor.Props = value.NewObject()
	ctor.Props.Set("prototype", value.ObjectVal(mapProto))
	globals["Map"] = value.FunctionVal(ctor)
}

// mapConstructor implements `new Map(iterable?)` and the call-form
// `Map(iterable?)`. iterable is a JS array of [k, v] pairs.
func mapConstructor(_ value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	var obj *value.Object
	if this.Type() == value.TypeObject {
		obj = this.AsObject()
		obj.SetProto(mapProto)
	} else {
		obj = value.NewObject()
		obj.SetProto(mapProto)
	}
	// Always allocate a fresh entries array so two Maps never alias.
	obj.Set("__entries__", value.ArrayVal(value.NewArray()))
	obj.Set("size", value.Number(0))

	// Optional iterable: must be an array of two-element arrays.
	src := argOrUndef(args, 0)
	if src.Type() == value.TypeArray {
		arr := src.AsArray()
		for i := 0; i < arr.Length(); i++ {
			entry := arr.Get(i)
			if entry.Type() != value.TypeArray {
				continue
			}
			ea := entry.AsArray()
			k := ea.Get(0)
			v := ea.Get(1)
			mapInternalSet(obj, k, v)
		}
	}
	return value.ObjectVal(obj), nil
}

// mapEntriesOf retrieves the internal flat-entries array. Returns nil
// if the receiver is missing/wrong-shaped (caller has already checked
// that this is the right kind of object).
func mapEntriesOf(o *value.Object) *value.Array {
	v, ok := o.GetOwn("__entries__")
	if !ok || v.Type() != value.TypeArray {
		return nil
	}
	return v.AsArray()
}

// mapKeyMatch implements SameValueZero — strict equality, with NaN
// matching NaN. Used by every key lookup.
func mapKeyMatch(a, b value.Value) bool {
	if a.Type() != b.Type() {
		return false
	}
	switch a.Type() {
	case value.TypeUndefined, value.TypeNull:
		return true
	case value.TypeBool:
		return a.AsBool() == b.AsBool()
	case value.TypeNumber:
		af, bf := a.AsNumber(), b.AsNumber()
		if math.IsNaN(af) && math.IsNaN(bf) {
			return true
		}
		return af == bf
	case value.TypeString:
		return a.AsString() == b.AsString()
	}
	// Reference types: identity — same pointer is the only match. We
	// have no direct identity check on Value, so compare via the
	// per-type pointer.
	switch a.Type() {
	case value.TypeObject:
		return a.AsObject() == b.AsObject()
	case value.TypeArray:
		return a.AsArray() == b.AsArray()
	case value.TypeFunction:
		return a.AsFunction() == b.AsFunction()
	}
	return false
}

// mapIndexOfKey returns the flat-array index of the first occurrence
// of key (i.e. the even slot 0, 2, 4, ...) or -1 if not present.
func mapIndexOfKey(entries *value.Array, key value.Value) int {
	n := entries.Length()
	for i := 0; i < n; i += 2 {
		if mapKeyMatch(entries.Get(i), key) {
			return i
		}
	}
	return -1
}

// mapInternalSet upserts (k, v) into the entries array on obj and
// updates the size property. Shared by the constructor and set().
func mapInternalSet(obj *value.Object, k, v value.Value) {
	entries := mapEntriesOf(obj)
	if entries == nil {
		return
	}
	if i := mapIndexOfKey(entries, k); i >= 0 {
		entries.Set(i+1, v)
		return
	}
	entries.Push(k)
	entries.Push(v)
	obj.Set("size", value.Number(float64(entries.Length()/2)))
}

// mapReceiver pulls the entries array off this, returning a JS
// TypeError if the receiver does not look like a Map.
func mapReceiver(method string, this value.Value) (*value.Object, *value.Array, error) {
	if this.Type() != value.TypeObject {
		return nil, nil, &value.JSThrow{Val: makeError("TypeError", "Map.prototype."+method+": this is not a Map")}
	}
	obj := this.AsObject()
	entries := mapEntriesOf(obj)
	if entries == nil {
		return nil, nil, &value.JSThrow{Val: makeError("TypeError", "Map.prototype."+method+": this is not a Map")}
	}
	return obj, entries, nil
}

func mapSet(_ value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	_, _, err := mapReceiver("set", this)
	if err != nil {
		return value.Value{}, err
	}
	k := argOrUndef(args, 0)
	v := argOrUndef(args, 1)
	mapInternalSet(this.AsObject(), k, v)
	return this, nil
}

func mapGet(_ value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	_, entries, err := mapReceiver("get", this)
	if err != nil {
		return value.Value{}, err
	}
	k := argOrUndef(args, 0)
	if i := mapIndexOfKey(entries, k); i >= 0 {
		return entries.Get(i + 1), nil
	}
	return value.Undefined(), nil
}

func mapHas(_ value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	_, entries, err := mapReceiver("has", this)
	if err != nil {
		return value.Value{}, err
	}
	k := argOrUndef(args, 0)
	return value.Bool(mapIndexOfKey(entries, k) >= 0), nil
}

// mapDelete removes the (k, v) pair by sliding everything after it
// down by 2 and truncating. Returns true iff the key existed.
func mapDelete(_ value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	obj, entries, err := mapReceiver("delete", this)
	if err != nil {
		return value.Value{}, err
	}
	k := argOrUndef(args, 0)
	i := mapIndexOfKey(entries, k)
	if i < 0 {
		return value.Bool(false), nil
	}
	n := entries.Length()
	for j := i; j+2 < n; j++ {
		entries.Set(j, entries.Get(j+2))
	}
	entries.Truncate(n - 2)
	obj.Set("size", value.Number(float64(entries.Length()/2)))
	return value.Bool(true), nil
}

func mapClear(_ value.Caller, this value.Value, _ []value.Value) (value.Value, error) {
	obj, entries, err := mapReceiver("clear", this)
	if err != nil {
		return value.Value{}, err
	}
	entries.Truncate(0)
	obj.Set("size", value.Number(0))
	return value.Undefined(), nil
}

// mapForEach invokes cb(value, key, map) for each entry. thisArg is
// the receiver passed to cb; defaults to undefined when omitted.
func mapForEach(caller value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	_, entries, err := mapReceiver("forEach", this)
	if err != nil {
		return value.Value{}, err
	}
	cb := argOrUndef(args, 0)
	if cb.Type() != value.TypeFunction {
		return value.Value{}, &value.JSThrow{Val: makeError("TypeError", "Map.prototype.forEach: callback is not a function")}
	}
	fn := cb.AsFunction()
	thisArg := value.Undefined()
	if len(args) >= 2 {
		thisArg = args[1]
	}
	// Snapshot the length so a callback that calls set() during
	// iteration doesn't lengthen the loop indefinitely.
	n := entries.Length()
	for i := 0; i < n; i += 2 {
		k := entries.Get(i)
		v := entries.Get(i + 1)
		if _, err := caller.Call(fn, thisArg, []value.Value{v, k, this}); err != nil {
			return value.Value{}, err
		}
	}
	return value.Undefined(), nil
}

func mapKeys(_ value.Caller, this value.Value, _ []value.Value) (value.Value, error) {
	_, entries, err := mapReceiver("keys", this)
	if err != nil {
		return value.Value{}, err
	}
	out := value.NewArray()
	n := entries.Length()
	for i := 0; i < n; i += 2 {
		out.Push(entries.Get(i))
	}
	return value.ArrayVal(out), nil
}

func mapValues(_ value.Caller, this value.Value, _ []value.Value) (value.Value, error) {
	_, entries, err := mapReceiver("values", this)
	if err != nil {
		return value.Value{}, err
	}
	out := value.NewArray()
	n := entries.Length()
	for i := 1; i < n; i += 2 {
		out.Push(entries.Get(i))
	}
	return value.ArrayVal(out), nil
}

func mapEntries(_ value.Caller, this value.Value, _ []value.Value) (value.Value, error) {
	_, entries, err := mapReceiver("entries", this)
	if err != nil {
		return value.Value{}, err
	}
	out := value.NewArray()
	n := entries.Length()
	for i := 0; i < n; i += 2 {
		pair := value.NewArray()
		pair.Push(entries.Get(i))
		pair.Push(entries.Get(i + 1))
		out.Push(value.ArrayVal(pair))
	}
	return value.ArrayVal(out), nil
}
