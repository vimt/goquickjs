// Object constructor object and static methods.
//
// Object.prototype lives implicitly on every Object (handled by the
// VM's getProp). Static methods (Object.keys, Object.values, ...)
// go on the constructor object built by installObject — fan-out
// agents add them here.

package builtins

import (
	"math"

	"github.com/vimt/goquickjs/internal/value"
)

func installObject(globals map[string]value.Value) {
	// Hang the canonical Object.prototype methods on the shared
	// singleton; every plain Object literal inherits from it (see
	// value.NewObject).
	proto := value.ObjectPrototype
	proto.Set("hasOwnProperty", nativeFn("hasOwnProperty", 1, objectProtoHasOwnProperty))
	proto.Set("isPrototypeOf", nativeFn("isPrototypeOf", 1, objectProtoIsPrototypeOf))
	proto.Set("propertyIsEnumerable", nativeFn("propertyIsEnumerable", 1, objectProtoPropertyIsEnumerable))
	proto.Set("toString", nativeFn("toString", 0, objectProtoToString))
	proto.Set("valueOf", nativeFn("valueOf", 0, objectProtoValueOf))

	// Object is callable: `Object(x)` returns x unchanged when x is
	// already a non-null/undefined object; null/undefined produce a
	// fresh empty object; primitives are passed through (real spec
	// boxes them into wrapper objects, which goquickjs does not yet
	// distinguish — the corpus pattern that breaks on this is rare).
	fn := &value.Function{Name: "Object", Arity: 1, Native: objectCoerce}
	fn.Props = value.NewObject()
	ctor := fn.Props
	ctor.Set("prototype", value.ObjectVal(proto))
	ctor.Set("keys", nativeFn("keys", 1, objectKeys))
	ctor.Set("values", nativeFn("values", 1, objectValues))
	ctor.Set("entries", nativeFn("entries", 1, objectEntries))
	ctor.Set("fromEntries", nativeFn("fromEntries", 1, objectFromEntries))
	ctor.Set("assign", nativeFn("assign", 2, objectAssign))
	ctor.Set("freeze", nativeFn("freeze", 1, objectFreeze))
	ctor.Set("isFrozen", nativeFn("isFrozen", 1, objectIsFrozen))
	ctor.Set("create", nativeFn("create", 2, objectCreate))
	ctor.Set("getPrototypeOf", nativeFn("getPrototypeOf", 1, objectGetPrototypeOf))
	ctor.Set("setPrototypeOf", nativeFn("setPrototypeOf", 2, objectSetPrototypeOf))
	ctor.Set("getOwnPropertyNames", nativeFn("getOwnPropertyNames", 1, objectGetOwnPropertyNames))
	ctor.Set("hasOwn", nativeFn("hasOwn", 2, objectHasOwn))
	ctor.Set("defineProperty", nativeFn("defineProperty", 3, objectDefineProperty))
	ctor.Set("getOwnPropertyDescriptor", nativeFn("getOwnPropertyDescriptor", 2, objectGetOwnPropertyDescriptor))
	ctor.Set("is", nativeFn("is", 2, objectIs))
	ctor.Set("groupBy", nativeFn("groupBy", 2, objectGroupBy))
	globals["Object"] = value.FunctionVal(fn)
}

// objectCoerce implements `Object(x)`: ToObject for non-null/undefined
// values, a fresh empty Object otherwise. Primitives are returned
// as-is (we don't yet model wrapper objects); the most common corpus
// pattern is `Object(receiver)` as an identity guard.
func objectCoerce(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	if len(args) == 0 {
		return value.ObjectVal(value.NewObject()), nil
	}
	v := args[0]
	if v.Type() == value.TypeUndefined || v.Type() == value.TypeNull {
		return value.ObjectVal(value.NewObject()), nil
	}
	return v, nil
}

// objectKeys returns the own enumerable string keys of obj in
// insertion order. Non-object args yield an empty array (we don't
// box primitives yet).
func objectKeys(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	arr := value.NewArray()
	v := argOrUndef(args, 0)
	if v.Type() != value.TypeObject {
		return value.ArrayVal(arr), nil
	}
	o := v.AsObject()
	for _, name := range o.PropNames() {
		arr.Push(value.String(name))
	}
	return value.ArrayVal(arr), nil
}

// objectValues returns the values paired with PropNames(), same order.
func objectValues(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	arr := value.NewArray()
	v := argOrUndef(args, 0)
	if v.Type() != value.TypeObject {
		return value.ArrayVal(arr), nil
	}
	o := v.AsObject()
	for _, name := range o.PropNames() {
		val, _ := o.Get(name)
		arr.Push(val)
	}
	return value.ArrayVal(arr), nil
}

// objectEntries returns [[k,v], ...] in insertion order.
func objectEntries(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	arr := value.NewArray()
	v := argOrUndef(args, 0)
	if v.Type() != value.TypeObject {
		return value.ArrayVal(arr), nil
	}
	o := v.AsObject()
	for _, name := range o.PropNames() {
		val, _ := o.Get(name)
		pair := value.NewArray()
		pair.Push(value.String(name))
		pair.Push(val)
		arr.Push(value.ArrayVal(pair))
	}
	return value.ArrayVal(arr), nil
}

// objectFromEntries rebuilds an object from [[k,v],...]. Accepts
// arrays of arrays today; iterator protocol is NYI.
func objectFromEntries(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	out := value.NewObject()
	v := argOrUndef(args, 0)
	if v.Type() != value.TypeArray {
		return value.ObjectVal(out), nil
	}
	arr := v.AsArray()
	for i := 0; i < arr.Length(); i++ {
		entry := arr.Get(i)
		if entry.Type() != value.TypeArray {
			continue
		}
		ea := entry.AsArray()
		key := ea.Get(0).String()
		val := ea.Get(1)
		out.Set(key, val)
	}
	return value.ObjectVal(out), nil
}

// objectAssign copies own enumerable props from each source into
// target, returning target. Non-object sources are ignored.
func objectAssign(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	target := argOrUndef(args, 0)
	if target.Type() != value.TypeObject {
		// Real spec coerces to Object; we don't box yet, so just
		// promote undefined/null target to a fresh object.
		target = value.ObjectVal(value.NewObject())
	}
	to := target.AsObject()
	for i := 1; i < len(args); i++ {
		src := args[i]
		if src.Type() != value.TypeObject {
			continue
		}
		so := src.AsObject()
		for _, name := range so.PropNames() {
			val, _ := so.Get(name)
			to.Set(name, val)
		}
	}
	return target, nil
}

// objectFreeze is a no-op simplified shim: returns the arg unchanged
// (we don't yet enforce frozen semantics on mutation).
func objectFreeze(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	return argOrUndef(args, 0), nil
}

// objectIsFrozen always returns false: we have no frozen bit.
func objectIsFrozen(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	_ = args
	return value.Bool(false), nil
}

// objectCreate builds a fresh object whose [[Prototype]] is proto.
// proto must be an Object or null; anything else throws TypeError.
// The optional propertiesObject arg is NYI and ignored.
func objectCreate(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	proto := argOrUndef(args, 0)
	obj := value.NewBareObject()
	switch proto.Type() {
	case value.TypeObject:
		obj.SetProto(proto.AsObject())
	case value.TypeNull:
		// Leave proto nil — explicit no-prototype object.
	default:
		return value.Value{}, &value.JSThrow{Val: makeError("TypeError", "Object.create: prototype must be Object or null")}
	}
	return value.ObjectVal(obj), nil
}

// objectGetPrototypeOf returns the [[Prototype]] of obj, or null when
// the chain is empty. Non-object args throw TypeError (matching the
// spec's ToObject coercion failure for undefined/null).
func objectGetPrototypeOf(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	v := argOrUndef(args, 0)
	switch v.Type() {
	case value.TypeUndefined, value.TypeNull:
		return value.Value{}, &value.JSThrow{Val: makeError("TypeError", "Object.getPrototypeOf: argument is null/undefined")}
	case value.TypeObject:
		p := v.AsObject().Proto()
		if p == nil {
			return value.Null(), nil
		}
		return value.ObjectVal(p), nil
	case value.TypeArray:
		// Real Array.prototype lives in value.ArrayProto's table; the
		// global Array.prototype Object mirror is what user code
		// reaches for typeof/etc — return that when available.
		return primitiveProto("Array"), nil
	case value.TypeString:
		return primitiveProto("String"), nil
	case value.TypeNumber:
		return primitiveProto("Number"), nil
	case value.TypeBool:
		return primitiveProto("Boolean"), nil
	case value.TypeFunction:
		return primitiveProto("Function"), nil
	}
	return value.Null(), nil
}

// primitiveProto fetches the `.prototype` Object that exposeProto set
// on each constructor. Returns null if missing — should only happen
// during very early init.
func primitiveProto(name string) value.Value {
	// The constructors live in the per-Eval globals map, which we
	// can't reach from a built-in without the Caller — but the proto
	// table itself is package-level. Build a one-shot mirror.
	switch name {
	case "Array":
		return value.ObjectVal(mirrorProto(value.ArrayProto))
	case "String":
		return value.ObjectVal(mirrorProto(value.StringProto))
	case "Number":
		return value.ObjectVal(mirrorProto(value.NumberProto))
	case "Function":
		return value.ObjectVal(mirrorProto(value.FunctionProto))
	}
	return value.ObjectVal(value.NewObject())
}

// mirrorProto is a per-call dup of exposeProto for the rare callers
// (getPrototypeOf on a primitive) that don't have the cached ctor in
// hand. Cheap since it's not on the hot path.
func mirrorProto(table map[string]*value.Function) *value.Object {
	o := value.NewObject()
	for name, fn := range table {
		o.Set(name, value.FunctionVal(fn))
	}
	return o
}

// objectSetPrototypeOf rewires obj.[[Prototype]] to proto (Object or
// null) and returns obj. Non-object obj is returned untouched (spec
// says throw on undefined/null, but coerces other primitives back to
// themselves — we mirror the leniency).
func objectSetPrototypeOf(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	v := argOrUndef(args, 0)
	proto := argOrUndef(args, 1)
	if v.Type() != value.TypeObject {
		if v.Type() == value.TypeUndefined || v.Type() == value.TypeNull {
			return value.Value{}, &value.JSThrow{Val: makeError("TypeError", "Object.setPrototypeOf: argument is not an Object")}
		}
		return v, nil
	}
	switch proto.Type() {
	case value.TypeObject:
		v.AsObject().SetProto(proto.AsObject())
	case value.TypeNull:
		v.AsObject().SetProto(nil)
	default:
		return value.Value{}, &value.JSThrow{Val: makeError("TypeError", "Object.setPrototypeOf: prototype must be Object or null")}
	}
	return v, nil
}

// objectGetOwnPropertyNames is the same as keys today since we don't
// distinguish enumerable from non-enumerable own properties.
func objectGetOwnPropertyNames(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	arr := value.NewArray()
	v := argOrUndef(args, 0)
	if v.Type() != value.TypeObject {
		return value.ArrayVal(arr), nil
	}
	o := v.AsObject()
	for _, name := range o.PropNames() {
		arr.Push(value.String(name))
	}
	return value.ArrayVal(arr), nil
}

// objectHasOwn reports whether obj has key as an own property (no
// prototype chain walk).
func objectHasOwn(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	v := argOrUndef(args, 0)
	if v.Type() != value.TypeObject {
		return value.Value{}, &value.JSThrow{Val: makeError("TypeError", "Object.hasOwn: argument is not an Object")}
	}
	key := argString(args, 1)
	_, ok := v.AsObject().GetOwn(key)
	return value.Bool(ok), nil
}

// objectDefineProperty supports both data and accessor descriptors.
// writable/enumerable/configurable bits are accepted but ignored
// (the engine tracks neither yet); a descriptor carrying any of
// get/set installs an accessor pair via Object.SetAccessor.
func objectDefineProperty(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	v := argOrUndef(args, 0)
	if v.Type() != value.TypeObject {
		return value.Value{}, &value.JSThrow{Val: makeError("TypeError", "Object.defineProperty: target is not an Object")}
	}
	key := argString(args, 1)
	desc := argOrUndef(args, 2)
	if desc.Type() != value.TypeObject {
		return value.Value{}, &value.JSThrow{Val: makeError("TypeError", "Object.defineProperty: descriptor is not an Object")}
	}
	d := desc.AsObject()
	getV, hasGet := d.GetOwn("get")
	setV, hasSet := d.GetOwn("set")
	if hasGet || hasSet {
		var getter, setter *value.Function
		if hasGet && getV.Type() == value.TypeFunction {
			getter = getV.AsFunction()
		}
		if hasSet && setV.Type() == value.TypeFunction {
			setter = setV.AsFunction()
		}
		v.AsObject().SetAccessor(key, getter, setter)
		return v, nil
	}
	val, ok := d.GetOwn("value")
	if !ok {
		val = value.Undefined()
	}
	v.AsObject().Set(key, val)
	return v, nil
}

// objectGetOwnPropertyDescriptor returns a fresh descriptor object for
// an own property, or undefined if the property doesn't exist on obj
// itself. All boolean attributes are reported as true (we don't track
// them).
func objectGetOwnPropertyDescriptor(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	v := argOrUndef(args, 0)
	if v.Type() != value.TypeObject {
		return value.Undefined(), nil
	}
	key := argString(args, 1)
	val, ok := v.AsObject().GetOwn(key)
	if !ok {
		return value.Undefined(), nil
	}
	d := value.NewObject()
	d.Set("value", val)
	d.Set("writable", value.Bool(true))
	d.Set("enumerable", value.Bool(true))
	d.Set("configurable", value.Bool(true))
	return value.ObjectVal(d), nil
}

// objectProtoHasOwnProperty reports whether this has key as an own prop.
func objectProtoHasOwnProperty(_ value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	if this.Type() != value.TypeObject {
		return value.Bool(false), nil
	}
	key := argString(args, 0)
	_, ok := this.AsObject().GetOwn(key)
	return value.Bool(ok), nil
}

// objectProtoIsPrototypeOf returns true if this appears anywhere on
// arg's prototype chain (not including arg itself per spec).
func objectProtoIsPrototypeOf(_ value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	if this.Type() != value.TypeObject {
		return value.Bool(false), nil
	}
	target := argOrUndef(args, 0)
	if target.Type() != value.TypeObject {
		return value.Bool(false), nil
	}
	want := this.AsObject()
	for p := target.AsObject().Proto(); p != nil; p = p.Proto() {
		if p == want {
			return value.Bool(true), nil
		}
	}
	return value.Bool(false), nil
}

// objectProtoPropertyIsEnumerable: we don't track an enumerable bit;
// every own data property is treated as enumerable, mirroring the
// behaviour callers actually rely on.
func objectProtoPropertyIsEnumerable(_ value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	if this.Type() != value.TypeObject {
		return value.Bool(false), nil
	}
	key := argString(args, 0)
	_, ok := this.AsObject().GetOwn(key)
	return value.Bool(ok), nil
}

// objectProtoToString returns "[object Object]" for plain objects —
// the spec's default tag derived from %Symbol.toStringTag%. Arrays
// and functions override this on their own prototypes.
func objectProtoToString(_ value.Caller, this value.Value, _ []value.Value) (value.Value, error) {
	switch this.Type() {
	case value.TypeUndefined:
		return value.String("[object Undefined]"), nil
	case value.TypeNull:
		return value.String("[object Null]"), nil
	case value.TypeArray:
		return value.String("[object Array]"), nil
	case value.TypeFunction:
		return value.String("[object Function]"), nil
	}
	return value.String("[object Object]"), nil
}

// objectProtoValueOf: default impl returns this unchanged (Number /
// String / Boolean wrappers would override, but we don't box yet).
func objectProtoValueOf(_ value.Caller, this value.Value, _ []value.Value) (value.Value, error) {
	return this, nil
}

// objectGroupBy (ES2024) partitions an array's items by the string
// each callback returns. Result is a plain Object with one bucket
// per distinct key.
func objectGroupBy(caller value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	src := argOrUndef(args, 0)
	cb := argOrUndef(args, 1)
	if src.Type() != value.TypeArray {
		return value.Value{}, &value.JSThrow{Val: makeError("TypeError", "Object.groupBy: first arg must be an array")}
	}
	if cb.Type() != value.TypeFunction {
		return value.Value{}, &value.JSThrow{Val: makeError("TypeError", "Object.groupBy: callback must be a function")}
	}
	out := value.NewObject()
	arr := src.AsArray()
	fn := cb.AsFunction()
	for i := 0; i < arr.Length(); i++ {
		item := arr.Get(i)
		k, err := caller.Call(fn, value.Undefined(), []value.Value{item, value.Number(float64(i))})
		if err != nil {
			return value.Value{}, err
		}
		key := k.String()
		bucketV, ok := out.GetOwn(key)
		var bucket *value.Array
		if ok && bucketV.Type() == value.TypeArray {
			bucket = bucketV.AsArray()
		} else {
			bucket = value.NewArray()
			out.Set(key, value.ArrayVal(bucket))
		}
		bucket.Push(item)
	}
	return value.ObjectVal(out), nil
}

// objectIs implements the SameValue algorithm: like === but NaN===NaN
// holds and +0 !== -0.
func objectIs(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	a := argOrUndef(args, 0)
	b := argOrUndef(args, 1)
	if a.Type() != b.Type() {
		return value.Bool(false), nil
	}
	switch a.Type() {
	case value.TypeNumber:
		af, bf := a.AsNumber(), b.AsNumber()
		// NaN case: SameValue says NaN equals NaN.
		if af != af && bf != bf {
			return value.Bool(true), nil
		}
		// +0 / -0 distinction.
		if af == 0 && bf == 0 {
			return value.Bool(math.Signbit(af) == math.Signbit(bf)), nil
		}
		return value.Bool(af == bf), nil
	case value.TypeUndefined, value.TypeNull:
		return value.Bool(true), nil
	case value.TypeBool:
		return value.Bool(a.AsBool() == b.AsBool()), nil
	case value.TypeString:
		return value.Bool(a.AsString() == b.AsString()), nil
	case value.TypeObject:
		return value.Bool(a.AsObject() == b.AsObject()), nil
	case value.TypeArray:
		return value.Bool(a.AsArray() == b.AsArray()), nil
	case value.TypeFunction:
		return value.Bool(a.AsFunction() == b.AsFunction()), nil
	}
	return value.Bool(false), nil
}
