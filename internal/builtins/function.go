// Function.prototype methods: call / apply / bind.
//
// Function.prototype lives in value.FunctionProto, populated by
// registerFunctionPrototype at init. Bound functions are constructed
// as native closures so the chain (bound → bind → ... → target) is
// transparent to the rest of the VM — each call simply re-enters
// through caller.Call.

package builtins

import (
	"github.com/vimt/goquickjs/internal/value"
)

func registerFunctionPrototype() {
	value.FunctionProto["call"] = &value.Function{Name: "call", Arity: 1, Native: functionCall}
	value.FunctionProto["apply"] = &value.Function{Name: "apply", Arity: 2, Native: functionApply}
	value.FunctionProto["bind"] = &value.Function{Name: "bind", Arity: 1, Native: functionBind}
}

// functionCall: this.call(thisArg, ...args)
func functionCall(caller value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	if this.Type() != value.TypeFunction {
		return value.Value{}, badThis("Function.prototype.call", "function")
	}
	var thisArg value.Value
	var rest []value.Value
	if len(args) > 0 {
		thisArg = args[0]
		rest = args[1:]
	} else {
		thisArg = value.Undefined()
	}
	// Copy rest because caller is allowed to retain args; cheap.
	cp := make([]value.Value, len(rest))
	copy(cp, rest)
	return caller.Call(this.AsFunction(), thisArg, cp)
}

// functionApply: this.apply(thisArg, argsArray)
func functionApply(caller value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	if this.Type() != value.TypeFunction {
		return value.Value{}, badThis("Function.prototype.apply", "function")
	}
	thisArg := argOrUndef(args, 0)
	argsV := argOrUndef(args, 1)
	var rest []value.Value
	switch argsV.Type() {
	case value.TypeUndefined, value.TypeNull:
		// Spec: undefined/null → no args.
	case value.TypeArray:
		arr := argsV.AsArray()
		rest = make([]value.Value, arr.Length())
		for i := range rest {
			rest[i] = arr.Get(i)
		}
	default:
		return value.Value{}, &typeError{msg: "Function.prototype.apply: argsArray is not an array"}
	}
	return caller.Call(this.AsFunction(), thisArg, rest)
}

// functionBind: this.bind(thisArg, ...preArgs) returns a NEW function
// whose Native, when invoked, prepends preArgs to its args and calls
// the target with thisArg bound. Each subsequent .bind() chains.
func functionBind(caller value.Caller, this value.Value, args []value.Value) (value.Value, error) {
	if this.Type() != value.TypeFunction {
		return value.Value{}, badThis("Function.prototype.bind", "function")
	}
	target := this.AsFunction()
	var boundThis value.Value
	var preArgs []value.Value
	if len(args) > 0 {
		boundThis = args[0]
		preArgs = append([]value.Value{}, args[1:]...)
	} else {
		boundThis = value.Undefined()
	}
	_ = caller // Captured caller is not used; bound fn uses its own caller per call.

	arity := target.Arity - len(preArgs)
	if arity < 0 {
		arity = 0
	}
	bound := &value.Function{
		Name:  "bound " + target.Name,
		Arity: arity,
		Native: func(c value.Caller, _ value.Value, callArgs []value.Value) (value.Value, error) {
			full := make([]value.Value, 0, len(preArgs)+len(callArgs))
			full = append(full, preArgs...)
			full = append(full, callArgs...)
			return c.Call(target, boundThis, full)
		},
	}
	return value.FunctionVal(bound), nil
}
