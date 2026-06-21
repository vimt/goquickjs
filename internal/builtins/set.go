// Set constructor and Set.prototype methods.
//
// A Set instance is a plain Object whose [[Prototype]] points at
// setProto. The internal storage is an Array stashed in the own
// property `__values__` — order = insertion order. Duplicate keys
// (SameValueZero) are rejected on add. We stay within the existing
// value types instead of inventing a new variant because the corpus
// only needs functional behavior, not a fancy fmt rendering of
// `Set(3){1,2,3}`.

package builtins

import (
	"math"

	"github.com/vimt/goquickjs/internal/value"
)

var setProto *value.Object

func installSet(globals map[string]value.Value) {
	setProto = value.NewObject()
	setProto.Set("add", nativeFn("add", 1, setAdd))
	setProto.Set("has", nativeFn("has", 1, setHas))
	setProto.Set("delete", nativeFn("delete", 1, setDelete))
	setProto.Set("clear", nativeFn("clear", 0, setClear))
	setProto.Set("forEach", nativeFn("forEach", 1, setForEach))
	setProto.Set("values", nativeFn("values", 0, setValues))
	setProto.Set("keys", nativeFn("keys", 0, setValues))

	ctor := &value.Function{
		Name:  "Set",
		Arity: 1,
		Native: func(_ value.Caller, this value.Value, args []value.Value) (value.Value, error) {
			// Set instance backing object.
			var obj *value.Object
			if this.Type() == value.TypeObject {
				obj = this.AsObject()
				obj.SetProto(setProto)
			} else {
				obj = value.NewObject()
				obj.SetProto(setProto)
			}
			storage := value.NewArray()
			obj.Set("__values__", value.ArrayVal(storage))
			obj.Set("size", value.Number(0))

			// Optional iterable: today we accept Array only (no
			// iterator protocol yet).
			if len(args) >= 1 {
				init := args[0]
				if init.Type() == value.TypeArray {
					src := init.AsArray()
					for i := 0; i < src.Length(); i++ {
						setInsert(obj, storage, src.Get(i))
					}
				} else if init.Type() != value.TypeUndefined && init.Type() != value.TypeNull {
					return value.Value{}, &value.JSThrow{Val: makeError("TypeError", "Set: iterable required")}
				}
			}
			return value.ObjectVal(obj), nil
		},
	}
	ctor.Props = value.NewObject()
	ctor.Props.Set("prototype", value.ObjectVal(setProto))
	globals["Set"] = value.FunctionVal(ctor)
}

// setStorage extracts the (*Array) backing a Set instance plus the
// instance object itself. Returns ok=false when this is not a valid
// Set (no __values__ slot).
func setStorage(this value.Value) (*value.Object, *value.Array, bool) {
	if this.Type() != value.TypeObject {
		return nil, nil, false
	}
	o := this.AsObject()
	sv, ok := o.GetOwn("__values__")
	if !ok || sv.Type() != value.TypeArray {
		return nil, nil, false
	}
	return o, sv.AsArray(), true
}

// setIndex returns the slot of v in arr under SameValueZero, or -1.
func setIndex(arr *value.Array, v value.Value) int {
	for i := 0; i < arr.Length(); i++ {
		if setSameValueZero(arr.Get(i), v) {
			return i
		}
	}
	return -1
}

// setInsert adds v if not already present and bumps `size` on the
// owning instance object.
func setInsert(o *value.Object, arr *value.Array, v value.Value) {
	if setIndex(arr, v) >= 0 {
		return
	}
	arr.Push(v)
	o.Set("size", value.Number(float64(arr.Length())))
}

// setSameValueZero compares two values for Set membership. Like ===
// for everything except NaN (NaN equals NaN here) and -0/+0 (equal).
// Reference types compare by pointer identity.
func setSameValueZero(a, b value.Value) bool {
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
	case value.TypeObject:
		return a.AsObject() == b.AsObject()
	case value.TypeArray:
		return a.AsArray() == b.AsArray()
	case value.TypeFunction:
		return a.AsFunction() == b.AsFunction()
	}
	return false
}

func setAdd(_ value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	o, arr, ok := setStorage(this)
	if !ok {
		return value.Value{}, badThis("Set.prototype.add", "Set")
	}
	setInsert(o, arr, argOrUndef(args, 0))
	return this, nil
}

func setHas(_ value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	_, arr, ok := setStorage(this)
	if !ok {
		return value.Value{}, badThis("Set.prototype.has", "Set")
	}
	return value.Bool(setIndex(arr, argOrUndef(args, 0)) >= 0), nil
}

func setDelete(_ value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	o, arr, ok := setStorage(this)
	if !ok {
		return value.Value{}, badThis("Set.prototype.delete", "Set")
	}
	idx := setIndex(arr, argOrUndef(args, 0))
	if idx < 0 {
		return value.Bool(false), nil
	}
	// Shift left then truncate. Mirrors Array.prototype.shift internals.
	n := arr.Length()
	for i := idx + 1; i < n; i++ {
		arr.Set(i-1, arr.Get(i))
	}
	arr.Truncate(n - 1)
	o.Set("size", value.Number(float64(arr.Length())))
	return value.Bool(true), nil
}

func setClear(_ value.Caller, this value.Value, _ []value.Value) (value.Value, error) {
	o, arr, ok := setStorage(this)
	if !ok {
		return value.Value{}, badThis("Set.prototype.clear", "Set")
	}
	arr.Truncate(0)
	o.Set("size", value.Number(0))
	return value.Undefined(), nil
}

// setForEach iterates the storage and invokes callback(value, value, set).
// thisArg is optional. Snapshot length first so an add() inside the
// callback doesn't extend the iteration (matches spec's "set was
// already populated when forEach started" only loosely, but covers
// the corpus we ship).
func setForEach(caller value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	_, arr, ok := setStorage(this)
	if !ok {
		return value.Value{}, badThis("Set.prototype.forEach", "Set")
	}
	cb := argOrUndef(args, 0)
	if cb.Type() != value.TypeFunction {
		return value.Value{}, badThis("Set.prototype.forEach", "function callback")
	}
	thisArg := value.Undefined()
	if len(args) >= 2 {
		thisArg = args[1]
	}
	fn := cb.AsFunction()
	n := arr.Length()
	for i := 0; i < n; i++ {
		v := arr.Get(i)
		if _, err := caller.Call(fn, thisArg, []value.Value{v, v, this}); err != nil {
			return value.Value{}, err
		}
	}
	return value.Undefined(), nil
}

// setValues returns a snapshot Array. Spec returns a SetIterator;
// we lack iterators so an array is the closest analogue. The corpus
// just indexes into it.
func setValues(_ value.Caller, this value.Value, _ []value.Value) (value.Value, error) {
	_, arr, ok := setStorage(this)
	if !ok {
		return value.Value{}, badThis("Set.prototype.values", "Set")
	}
	out := value.NewArray()
	for i := 0; i < arr.Length(); i++ {
		out.Push(arr.Get(i))
	}
	return value.ArrayVal(out), nil
}
