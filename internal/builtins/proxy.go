// Proxy — wraps a target with a handler that intercepts property
// operations. We implement the most-used traps:
//   get(target, prop, receiver)
//   set(target, prop, value, receiver)
//   has(target, prop)
//   deleteProperty(target, prop)
// Other traps (apply / construct / ownKeys / etc.) fall back to the
// target — see vm/vm.go for the dispatch hooks.

package builtins

import (
	"github.com/vimt/goquickjs/internal/value"
)

func installProxy(globals map[string]value.Value) {
	fn := &value.Function{Name: "Proxy", Arity: 2, Native: proxyConstruct}
	globals["Proxy"] = value.FunctionVal(fn)
}

func proxyConstruct(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	target := argOrUndef(args, 0)
	handler := argOrUndef(args, 1)
	if target.Type() != value.TypeObject {
		return value.Value{}, &value.JSThrow{Val: makeError("TypeError", "Proxy: target must be an object")}
	}
	if handler.Type() != value.TypeObject {
		return value.Value{}, &value.JSThrow{Val: makeError("TypeError", "Proxy: handler must be an object")}
	}
	p := value.NewBareObject()
	p.Proxy = &value.ProxyMeta{
		Target:  target.AsObject(),
		Handler: handler.AsObject(),
	}
	return value.ObjectVal(p), nil
}
