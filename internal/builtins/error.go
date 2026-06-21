// Error / TypeError / RangeError constructors.
//
// Each is a JS-callable constructor that returns a fresh object
// whose [[Prototype]] is a shared per-class prototype object. The
// prototypes carry a default toString that renders "<name>: <message>"
// — matching the canonical QuickJS behaviour for uncaught errors.

package builtins

import (
	"github.com/vimt/goquickjs/internal/value"
)

var (
	errorProto       *value.Object
	typeErrorProto   *value.Object
	rangeErrorProto  *value.Object
	syntaxErrorProto *value.Object
	uriErrorProto    *value.Object
	evalErrorProto   *value.Object
)

func init() {
	// Install the VM-level error factory hook so leaf packages
	// (the VM, builtins themselves) can throw real Error instances
	// without forming an import cycle back through builtins.
	value.MakeError = func(kind, msg string) value.Value {
		// Lazy: protos may not be set yet on the first call (Eval
		// runs Install before any code), so synthesise minimal ones
		// on the fly if needed.
		switch kind {
		case "TypeError":
			if typeErrorProto != nil {
				return makeError(kind, msg)
			}
		case "RangeError":
			if rangeErrorProto != nil {
				return makeError(kind, msg)
			}
		case "SyntaxError":
			if syntaxErrorProto != nil {
				return makeError(kind, msg)
			}
		case "URIError":
			if uriErrorProto != nil {
				return makeError(kind, msg)
			}
		case "EvalError":
			if evalErrorProto != nil {
				return makeError(kind, msg)
			}
		default:
			if errorProto != nil {
				return makeError(kind, msg)
			}
		}
		return value.String(msg)
	}
}

func installError(globals map[string]value.Value) {
	errorProto = makeProto("Error")
	typeErrorProto = makeProto("TypeError")
	rangeErrorProto = makeProto("RangeError")
	syntaxErrorProto = makeProto("SyntaxError")
	uriErrorProto = makeProto("URIError")
	evalErrorProto = makeProto("EvalError")
	typeErrorProto.SetProto(errorProto)
	rangeErrorProto.SetProto(errorProto)
	syntaxErrorProto.SetProto(errorProto)
	uriErrorProto.SetProto(errorProto)
	evalErrorProto.SetProto(errorProto)

	globals["Error"] = makeErrorCtor("Error", errorProto)
	globals["TypeError"] = makeErrorCtor("TypeError", typeErrorProto)
	globals["RangeError"] = makeErrorCtor("RangeError", rangeErrorProto)
	globals["SyntaxError"] = makeErrorCtor("SyntaxError", syntaxErrorProto)
	globals["URIError"] = makeErrorCtor("URIError", uriErrorProto)
	globals["EvalError"] = makeErrorCtor("EvalError", evalErrorProto)
}

func makeProto(name string) *value.Object {
	p := value.NewObject()
	p.Set("name", value.String(name))
	p.Set("message", value.String(""))
	p.Set("toString", value.FunctionVal(&value.Function{Name: "toString", Arity: 0, Native: errorToString}))
	return p
}

// makeErrorCtor returns the constructor function value. It works as
// both `new TypeError("msg")` (via OpNew binding this to a fresh obj)
// and `TypeError("msg")` (returning a fresh obj).
func makeErrorCtor(name string, proto *value.Object) value.Value {
	ctor := &value.Function{
		Name:  name,
		Arity: 1,
		Native: func(_ value.Caller, this value.Value, args []value.Value) (value.Value, error) {
			var obj *value.Object
			if this.Type() == value.TypeObject {
				// Called via `new` — this is the fresh instance.
				obj = this.AsObject()
				obj.SetProto(proto)
			} else {
				obj = value.NewObject()
				obj.SetProto(proto)
			}
			obj.Set("message", value.String(argString(args, 0)))
			return value.ObjectVal(obj), nil
		},
	}
	// Wire the public `prototype` so `Error.prototype.foo = ...` works
	// the same way it does for user constructors.
	ctor.Props = value.NewObject()
	ctor.Props.Set("prototype", value.ObjectVal(proto))
	return value.FunctionVal(ctor)
}

// errorToString renders the canonical "<name>: <message>" string,
// falling back when either prop is missing.
func errorToString(_ value.Caller, this value.Value, _ []value.Value) (value.Value, error) {
	if this.Type() != value.TypeObject {
		return value.Value{}, badThis("Error.prototype.toString", "object")
	}
	o := this.AsObject()
	name := "Error"
	if nv, ok := o.Get("name"); ok || nv.Type() == value.TypeString {
		if nv.Type() == value.TypeString {
			name = nv.AsString()
		}
	}
	msg := ""
	if mv, ok := o.Get("message"); ok || mv.Type() == value.TypeString {
		if mv.Type() == value.TypeString {
			msg = mv.AsString()
		}
	}
	if msg == "" {
		return value.String(name), nil
	}
	return value.String(name + ": " + msg), nil
}

// makeError builds a thrown Error-like value from inside the runtime
// (e.g. badThis). The instance's prototype is the right per-class
// proto so `e instanceof TypeError` works.
func makeError(kind, msg string) value.Value {
	var proto *value.Object
	switch kind {
	case "TypeError":
		proto = typeErrorProto
	case "RangeError":
		proto = rangeErrorProto
	case "SyntaxError":
		proto = syntaxErrorProto
	case "URIError":
		proto = uriErrorProto
	case "EvalError":
		proto = evalErrorProto
	default:
		proto = errorProto
	}
	obj := value.NewObject()
	obj.SetProto(proto)
	obj.Set("message", value.String(msg))
	return value.ObjectVal(obj)
}
