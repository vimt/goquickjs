// Symbol — a first-class primitive value whose Type() is TypeSymbol.
//
// Each `Symbol(desc)` call returns a fresh *value.Symbol; SameValue
// is pointer identity. `Symbol.for(key)` deduplicates by description
// string through a package-level registry so two calls with the same
// key produce `===`-equal symbols.
//
// Well-known symbols (Symbol.iterator, Symbol.toPrimitive, ...) are
// exposed as stable singletons. The engine itself does not yet wire
// most of them into its iteration / coercion paths — they exist so
// user code that reads them gets a recognisable value.

package builtins

import (
	"github.com/vimt/goquickjs/internal/value"
)

var symbolRegistry = map[string]*value.Symbol{}
var reverseSymbolRegistry = map[*value.Symbol]string{}

func installSymbol(globals map[string]value.Value) {
	ctor := &value.Function{
		Name:   "Symbol",
		Arity:  1,
		Native: symbolConstructor,
	}
	ctor.Props = value.NewObject()
	ctor.Props.Set("for", nativeFn("for", 1, symbolFor))
	ctor.Props.Set("keyFor", nativeFn("keyFor", 1, symbolKeyFor))

	ctor.Props.Set("iterator", value.SymbolVal(&value.Symbol{Description: "Symbol.iterator"}))
	ctor.Props.Set("asyncIterator", value.SymbolVal(&value.Symbol{Description: "Symbol.asyncIterator"}))
	ctor.Props.Set("toPrimitive", value.SymbolVal(&value.Symbol{Description: "Symbol.toPrimitive"}))
	ctor.Props.Set("hasInstance", value.SymbolVal(&value.Symbol{Description: "Symbol.hasInstance"}))

	globals["Symbol"] = value.FunctionVal(ctor)
}

func symbolConstructor(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	desc := ""
	if d := argOrUndef(args, 0); d.Type() != value.TypeUndefined {
		desc = d.String()
	}
	return value.SymbolVal(&value.Symbol{Description: desc}), nil
}

func symbolFor(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	key := argString(args, 0)
	if existing, ok := symbolRegistry[key]; ok {
		return value.SymbolVal(existing), nil
	}
	s := &value.Symbol{Description: key}
	symbolRegistry[key] = s
	reverseSymbolRegistry[s] = key
	return value.SymbolVal(s), nil
}

func symbolKeyFor(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	v := argOrUndef(args, 0)
	if v.Type() != value.TypeSymbol {
		return value.Undefined(), nil
	}
	if k, ok := reverseSymbolRegistry[v.AsSymbol()]; ok {
		return value.String(k), nil
	}
	return value.Undefined(), nil
}
