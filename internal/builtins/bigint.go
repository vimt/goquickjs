// BigInt — global constructor. Used as `BigInt(value)` (no `new`
// allowed by spec; we accept either). Accepts numbers (integer
// truncation), strings, booleans, and existing BigInts.

package builtins

import (
	"math/big"

	"github.com/vimt/goquickjs/internal/value"
)

func installBigInt(globals map[string]value.Value) {
	fn := &value.Function{Name: "BigInt", Arity: 1, Native: bigIntConstruct}
	globals["BigInt"] = value.FunctionVal(fn)
}

func bigIntConstruct(_ value.Caller, _ value.Value, args []value.Value) (value.Value, error) {
	a := argOrUndef(args, 0)
	switch a.Type() {
	case value.TypeBigInt:
		return a, nil
	case value.TypeNumber:
		f := a.AsNumber()
		if f != float64(int64(f)) || f != f {
			return value.Value{}, &value.JSThrow{Val: makeError("RangeError", "BigInt: number must be an integer")}
		}
		return value.BigIntVal(&value.BigInt{I: big.NewInt(int64(f))}), nil
	case value.TypeBool:
		v := int64(0)
		if a.AsBool() {
			v = 1
		}
		return value.BigIntVal(&value.BigInt{I: big.NewInt(v)}), nil
	case value.TypeString:
		bi := new(big.Int)
		if _, ok := bi.SetString(a.AsString(), 10); !ok {
			return value.Value{}, &value.JSThrow{Val: makeError("SyntaxError", "BigInt: cannot parse string")}
		}
		return value.BigIntVal(&value.BigInt{I: bi}), nil
	}
	return value.Value{}, &value.JSThrow{Val: makeError("TypeError", "BigInt: unsupported argument type")}
}
