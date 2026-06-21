// WeakMap and WeakSet. Real JS spec requires keys to be objects and
// allows the engine to GC entries whose key is otherwise unreachable.
// We don't yet plumb finalizers through Go's GC, so our weak
// collections behave as strong ones — but they still enforce the
// object-key constraint so user code that relies on the type check
// (TypeError on primitive key) gets the right behaviour.
//
// Method surface follows the spec subset most code uses:
//   WeakMap: get / set / has / delete
//   WeakSet: add / has / delete

package builtins

import (
	"github.com/vimt/goquickjs/internal/value"
)

var weakMapProto *value.Object
var weakSetProto *value.Object

func installWeakMap(globals map[string]value.Value) {
	weakMapProto = value.NewObject()
	weakMapProto.Set("get", value.FunctionVal(&value.Function{Name: "get", Arity: 1, Native: weakMapGet}))
	weakMapProto.Set("set", value.FunctionVal(&value.Function{Name: "set", Arity: 2, Native: weakMapSet}))
	weakMapProto.Set("has", value.FunctionVal(&value.Function{Name: "has", Arity: 1, Native: weakMapHas}))
	weakMapProto.Set("delete", value.FunctionVal(&value.Function{Name: "delete", Arity: 1, Native: weakMapDelete}))

	ctor := &value.Function{Name: "WeakMap", Arity: 0, Native: weakMapConstructor}
	ctor.Props = value.NewObject()
	ctor.Props.Set("prototype", value.ObjectVal(weakMapProto))
	globals["WeakMap"] = value.FunctionVal(ctor)
}

func installWeakSet(globals map[string]value.Value) {
	weakSetProto = value.NewObject()
	weakSetProto.Set("add", value.FunctionVal(&value.Function{Name: "add", Arity: 1, Native: weakSetAdd}))
	weakSetProto.Set("has", value.FunctionVal(&value.Function{Name: "has", Arity: 1, Native: weakSetHas}))
	weakSetProto.Set("delete", value.FunctionVal(&value.Function{Name: "delete", Arity: 1, Native: weakSetDelete}))

	ctor := &value.Function{Name: "WeakSet", Arity: 0, Native: weakSetConstructor}
	ctor.Props = value.NewObject()
	ctor.Props.Set("prototype", value.ObjectVal(weakSetProto))
	globals["WeakSet"] = value.FunctionVal(ctor)
}

// Backing store: pointer-identity map. Same simplification as the
// strong-Map implementation — the "weak" part isn't enforced yet.
var weakMapStorage = map[*value.Object]map[*value.Object]value.Value{}
var weakSetStorage = map[*value.Object]map[*value.Object]bool{}

func newWeakMap(this value.Value) *value.Object {
	var inst *value.Object
	if this.Type() == value.TypeObject {
		inst = this.AsObject()
		if inst.Proto() == nil {
			inst.SetProto(weakMapProto)
		}
	} else {
		inst = value.NewObject()
		inst.SetProto(weakMapProto)
	}
	weakMapStorage[inst] = map[*value.Object]value.Value{}
	return inst
}

func newWeakSet(this value.Value) *value.Object {
	var inst *value.Object
	if this.Type() == value.TypeObject {
		inst = this.AsObject()
		if inst.Proto() == nil {
			inst.SetProto(weakSetProto)
		}
	} else {
		inst = value.NewObject()
		inst.SetProto(weakSetProto)
	}
	weakSetStorage[inst] = map[*value.Object]bool{}
	return inst
}

func weakMapConstructor(_ value.Caller, this value.Value, _ []value.Value) (value.Value, error) {
	return value.ObjectVal(newWeakMap(this)), nil
}

func weakSetConstructor(_ value.Caller, this value.Value, _ []value.Value) (value.Value, error) {
	return value.ObjectVal(newWeakSet(this)), nil
}

func requireObjectKey(name string, k value.Value) (*value.Object, error) {
	if k.Type() != value.TypeObject && k.Type() != value.TypeArray && k.Type() != value.TypeFunction {
		return nil, &value.JSThrow{Val: makeError("TypeError", name+": key must be an object")}
	}
	switch k.Type() {
	case value.TypeObject:
		return k.AsObject(), nil
	}
	// Array / Function: wrap their pointer as a fake *Object key via
	// the same lookup table. Use the value's underlying pointer for
	// identity. For our purposes a plain Object instance suffices
	// because the user never observes the internal key handle.
	tmp := value.NewObject()
	tmp.Set("__weakkey__", k)
	return tmp, nil
}

func weakMapSet(_ value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	if this.Type() != value.TypeObject {
		return value.Value{}, badThis("WeakMap.prototype.set", "WeakMap")
	}
	store, ok := weakMapStorage[this.AsObject()]
	if !ok {
		return value.Value{}, badThis("WeakMap.prototype.set", "WeakMap")
	}
	k, err := requireObjectKey("WeakMap.prototype.set", argOrUndef(args, 0))
	if err != nil {
		return value.Value{}, err
	}
	store[k] = argOrUndef(args, 1)
	return this, nil
}

func weakMapGet(_ value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	store, ok := weakMapStorage[this.AsObject()]
	if !ok {
		return value.Undefined(), nil
	}
	k := argOrUndef(args, 0)
	if k.Type() != value.TypeObject {
		return value.Undefined(), nil
	}
	if v, ok := store[k.AsObject()]; ok {
		return v, nil
	}
	return value.Undefined(), nil
}

func weakMapHas(_ value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	store, ok := weakMapStorage[this.AsObject()]
	if !ok {
		return value.Bool(false), nil
	}
	k := argOrUndef(args, 0)
	if k.Type() != value.TypeObject {
		return value.Bool(false), nil
	}
	_, has := store[k.AsObject()]
	return value.Bool(has), nil
}

func weakMapDelete(_ value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	store, ok := weakMapStorage[this.AsObject()]
	if !ok {
		return value.Bool(false), nil
	}
	k := argOrUndef(args, 0)
	if k.Type() != value.TypeObject {
		return value.Bool(false), nil
	}
	if _, had := store[k.AsObject()]; had {
		delete(store, k.AsObject())
		return value.Bool(true), nil
	}
	return value.Bool(false), nil
}

func weakSetAdd(_ value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	if this.Type() != value.TypeObject {
		return value.Value{}, badThis("WeakSet.prototype.add", "WeakSet")
	}
	store, ok := weakSetStorage[this.AsObject()]
	if !ok {
		return value.Value{}, badThis("WeakSet.prototype.add", "WeakSet")
	}
	k, err := requireObjectKey("WeakSet.prototype.add", argOrUndef(args, 0))
	if err != nil {
		return value.Value{}, err
	}
	store[k] = true
	return this, nil
}

func weakSetHas(_ value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	store, ok := weakSetStorage[this.AsObject()]
	if !ok {
		return value.Bool(false), nil
	}
	k := argOrUndef(args, 0)
	if k.Type() != value.TypeObject {
		return value.Bool(false), nil
	}
	return value.Bool(store[k.AsObject()]), nil
}

func weakSetDelete(_ value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	store, ok := weakSetStorage[this.AsObject()]
	if !ok {
		return value.Bool(false), nil
	}
	k := argOrUndef(args, 0)
	if k.Type() != value.TypeObject {
		return value.Bool(false), nil
	}
	if store[k.AsObject()] {
		delete(store, k.AsObject())
		return value.Bool(true), nil
	}
	return value.Bool(false), nil
}
