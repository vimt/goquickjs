// Reflect — a small set of meta-operations that mirror existing
// behaviour. We expose the most-used surface: get / set / has /
// deleteProperty / ownKeys / getPrototypeOf / setPrototypeOf /
// defineProperty / apply / construct. Everything else delegates to
// the underlying Object/Array machinery.

package builtins

import (
	"github.com/vimt/goquickjs/internal/value"
)

func installReflect(globals map[string]value.Value) {
	o := value.NewObject()
	o.Set("get", nativeFn("get", 2, reflectGet))
	o.Set("set", nativeFn("set", 3, reflectSet))
	o.Set("has", nativeFn("has", 2, reflectHas))
	o.Set("deleteProperty", nativeFn("deleteProperty", 2, reflectDelete))
	o.Set("ownKeys", nativeFn("ownKeys", 1, reflectOwnKeys))
	o.Set("getPrototypeOf", nativeFn("getPrototypeOf", 1, reflectGetProto))
	o.Set("setPrototypeOf", nativeFn("setPrototypeOf", 2, reflectSetProto))
	o.Set("defineProperty", nativeFn("defineProperty", 3, reflectDefineProperty))
	o.Set("apply", nativeFn("apply", 3, reflectApply))
	o.Set("construct", nativeFn("construct", 2, reflectConstruct))
	globals["Reflect"] = value.ObjectVal(o)
}

func reflectGet(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	target := argOrUndef(args, 0)
	key := argString(args, 1)
	if target.Type() != value.TypeObject {
		return value.Undefined(), nil
	}
	v, _ := target.AsObject().Get(key)
	return v, nil
}

func reflectSet(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	target := argOrUndef(args, 0)
	if target.Type() != value.TypeObject {
		return value.Bool(false), nil
	}
	target.AsObject().Set(argString(args, 1), argOrUndef(args, 2))
	return value.Bool(true), nil
}

func reflectHas(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	target := argOrUndef(args, 0)
	if target.Type() != value.TypeObject {
		return value.Bool(false), nil
	}
	_, ok := target.AsObject().Get(argString(args, 1))
	return value.Bool(ok), nil
}

func reflectDelete(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	target := argOrUndef(args, 0)
	if target.Type() != value.TypeObject {
		return value.Bool(false), nil
	}
	return value.Bool(target.AsObject().Delete(argString(args, 1))), nil
}

func reflectOwnKeys(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	out := value.NewArray()
	target := argOrUndef(args, 0)
	if target.Type() != value.TypeObject {
		return value.ArrayVal(out), nil
	}
	for _, n := range target.AsObject().PropNames() {
		out.Push(value.String(n))
	}
	return value.ArrayVal(out), nil
}

func reflectGetProto(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	target := argOrUndef(args, 0)
	if target.Type() != value.TypeObject {
		return value.Null(), nil
	}
	p := target.AsObject().Proto()
	if p == nil {
		return value.Null(), nil
	}
	return value.ObjectVal(p), nil
}

func reflectSetProto(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	target := argOrUndef(args, 0)
	proto := argOrUndef(args, 1)
	if target.Type() != value.TypeObject {
		return value.Bool(false), nil
	}
	switch proto.Type() {
	case value.TypeObject:
		target.AsObject().SetProto(proto.AsObject())
	case value.TypeNull:
		target.AsObject().SetProto(nil)
	default:
		return value.Bool(false), nil
	}
	return value.Bool(true), nil
}

func reflectDefineProperty(c value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	_, err := objectDefineProperty(c, value.Undefined(), args)
	if err != nil {
		return value.Bool(false), nil
	}
	return value.Bool(true), nil
}

func reflectApply(caller value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	fn := argOrUndef(args, 0)
	thisArg := argOrUndef(args, 1)
	argsArr := argOrUndef(args, 2)
	if fn.Type() != value.TypeFunction {
		return value.Value{}, &value.JSThrow{Val: makeError("TypeError", "Reflect.apply: target must be callable")}
	}
	var rest []value.Value
	if argsArr.Type() == value.TypeArray {
		a := argsArr.AsArray()
		rest = make([]value.Value, a.Length())
		for i := 0; i < a.Length(); i++ {
			rest[i] = a.Get(i)
		}
	}
	return caller.Call(fn.AsFunction(), thisArg, rest)
}

func reflectConstruct(caller value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	fn := argOrUndef(args, 0)
	argsArr := argOrUndef(args, 1)
	if fn.Type() != value.TypeFunction {
		return value.Value{}, &value.JSThrow{Val: makeError("TypeError", "Reflect.construct: target must be callable")}
	}
	f := fn.AsFunction()
	inst := value.NewObject()
	protoV := value.FunctionGetProp(f, "prototype")
	if protoV.Type() == value.TypeObject {
		inst.SetProto(protoV.AsObject())
	}
	var rest []value.Value
	if argsArr.Type() == value.TypeArray {
		a := argsArr.AsArray()
		rest = make([]value.Value, a.Length())
		for i := 0; i < a.Length(); i++ {
			rest[i] = a.Get(i)
		}
	}
	thisVal := value.ObjectVal(inst)
	ret, err := caller.Call(f, thisVal, rest)
	if err != nil {
		return value.Value{}, err
	}
	if ret.Type() == value.TypeObject {
		return ret, nil
	}
	return thisVal, nil
}
