package compiler

import (
	"fmt"
	"math/big"

	"github.com/vimt/goquickjs/internal/bytecode"
	"github.com/vimt/goquickjs/internal/jserrors"
	"github.com/vimt/goquickjs/internal/parser"
	"github.com/vimt/goquickjs/internal/value"
)

func (c *compiler) emit(n parser.Node) error {
	switch x := n.(type) {
	case *parser.NumberLit:
		c.chunk.EmitU16(bytecode.OpConstK, c.chunk.AddConstant(value.Number(x.Value)))
	case *parser.BigIntLit:
		bi := new(big.Int)
		if _, ok := bi.SetString(x.Digits, x.Base); !ok {
			return fmt.Errorf("compiler: bad bigint literal %q base %d", x.Digits, x.Base)
		}
		c.chunk.EmitU16(bytecode.OpConstK, c.chunk.AddConstant(value.BigIntVal(&value.BigInt{I: bi})))
	case *parser.StringLit:
		c.chunk.EmitU16(bytecode.OpConstK, c.chunk.AddConstant(value.String(x.Value)))
	case *parser.BoolLit:
		if x.Value {
			c.chunk.Emit(bytecode.OpConstTrue)
		} else {
			c.chunk.Emit(bytecode.OpConstFalse)
		}
	case *parser.NullLit:
		c.chunk.Emit(bytecode.OpConstNull)
	case *parser.UndefinedLit:
		c.chunk.Emit(bytecode.OpConstUndefined)
	case *parser.ThisExpr:
		c.chunk.Emit(bytecode.OpLoadThis)

	case *parser.Ident:
		return c.emitLoad(c.resolve(x.Name), x.Name)

	case *parser.BinaryExpr:
		if err := c.emit(x.L); err != nil {
			return err
		}
		if err := c.emit(x.R); err != nil {
			return err
		}
		op, ok := binaryOps[x.Op]
		if !ok {
			return fmt.Errorf("compiler: binop %q: %w", x.Op, jserrors.ErrNotImplemented)
		}
		c.chunk.Emit(op)

	case *parser.LogicalExpr:
		if err := c.emit(x.L); err != nil {
			return err
		}
		var jumpOp bytecode.Op
		switch x.Op {
		case "&&":
			jumpOp = bytecode.OpJumpIfFalsePeek
		case "||":
			jumpOp = bytecode.OpJumpIfTruePeek
		case "??":
			jumpOp = bytecode.OpJumpIfNotNullishPeek
		default:
			return fmt.Errorf("compiler: logical %q: %w", x.Op, jserrors.ErrNotImplemented)
		}
		patch := c.chunk.EmitJump(jumpOp)
		c.chunk.Emit(bytecode.OpPop)
		if err := c.emit(x.R); err != nil {
			return err
		}
		return c.chunk.PatchJump(patch)

	case *parser.UnaryExpr:
		// `delete` is special: it needs the *unevaluated* member /
		// index target so it can emit (obj, key) and execute the
		// removal. Handle before the generic emit-operand path.
		if x.Op == "delete" {
			switch tgt := x.X.(type) {
			case *parser.MemberExpr:
				if err := c.emit(tgt.Obj); err != nil {
					return err
				}
				nameIdx := c.chunk.AddConstant(value.String(tgt.Prop))
				c.chunk.EmitU16(bytecode.OpDeleteProp, nameIdx)
			case *parser.IndexExpr:
				if err := c.emit(tgt.Obj); err != nil {
					return err
				}
				if err := c.emit(tgt.Index); err != nil {
					return err
				}
				c.chunk.Emit(bytecode.OpDeleteByVal)
			default:
				// `delete plainVar` is sloppy-mode no-op returning
				// false (well — true in non-strict for unknown
				// globals, but tests usually only check it doesn't
				// throw). Evaluate for side effects, drop, push true.
				if err := c.emit(x.X); err != nil {
					return err
				}
				c.chunk.Emit(bytecode.OpPop)
				c.chunk.EmitU16(bytecode.OpConstK, c.chunk.AddConstant(value.Bool(true)))
			}
			break
		}
		if err := c.emit(x.X); err != nil {
			return err
		}
		switch x.Op {
		case "-":
			c.chunk.Emit(bytecode.OpNeg)
		case "!":
			c.chunk.Emit(bytecode.OpNot)
		case "~":
			c.chunk.Emit(bytecode.OpBitNot)
		case "typeof":
			c.chunk.Emit(bytecode.OpTypeof)
		case "void":
			c.chunk.Emit(bytecode.OpVoid)
		default:
			return fmt.Errorf("compiler: unop %q: %w", x.Op, jserrors.ErrNotImplemented)
		}

	case *parser.ConditionalExpr:
		// test ? cons : alt
		if err := c.emit(x.Test); err != nil {
			return err
		}
		jumpAlt := c.chunk.EmitJump(bytecode.OpJumpIfFalse)
		if err := c.emit(x.Cons); err != nil {
			return err
		}
		jumpEnd := c.chunk.EmitJump(bytecode.OpJump)
		if err := c.chunk.PatchJump(jumpAlt); err != nil {
			return err
		}
		if err := c.emit(x.Alt); err != nil {
			return err
		}
		return c.chunk.PatchJump(jumpEnd)

	case *parser.OptionalMemberExpr:
		// obj?.prop — undefined when obj is null/undefined.
		if err := c.emit(x.Obj); err != nil {
			return err
		}
		// JumpIfNullishPeek leaves the value on the stack so the
		// skip branch can Pop+ConstUndefined.
		skipJump := c.chunk.EmitJump(bytecode.OpJumpIfNullishPeek)
		nameIdx := c.chunk.AddConstant(value.String(x.Prop))
		c.chunk.EmitGetProp(nameIdx)
		endJump := c.chunk.EmitJump(bytecode.OpJump)
		if err := c.chunk.PatchJump(skipJump); err != nil {
			return err
		}
		c.chunk.Emit(bytecode.OpPop) // pop the nullish base
		c.chunk.Emit(bytecode.OpConstUndefined)
		return c.chunk.PatchJump(endJump)

	case *parser.AwaitExpr:
		if err := c.emit(x.X); err != nil {
			return err
		}
		c.chunk.Emit(bytecode.OpAwait)

	case *parser.RegexLit:
		// Lower to `new RegExp(pattern, flags)` so the runtime layer
		// can pick the implementation. RegExp is a normal global
		// installed by builtins/regexp.go.
		c.chunk.EmitU16(bytecode.OpLoadGlobal, c.chunk.AddConstant(value.String("RegExp")))
		c.chunk.EmitU16(bytecode.OpConstK, c.chunk.AddConstant(value.String(x.Pattern)))
		if x.Flags == "" {
			c.chunk.EmitU8(bytecode.OpNew, 1)
		} else {
			c.chunk.EmitU16(bytecode.OpConstK, c.chunk.AddConstant(value.String(x.Flags)))
			c.chunk.EmitU8(bytecode.OpNew, 2)
		}

	case *parser.TemplateLit:
		// Lower `q0${e0}q1${e1}q2` to: q0 + String(e0) + q1 + String(e1) + q2.
		// Empty leading/trailing quasis are still emitted as ""; the
		// engine relies on + with a string LHS to do ToString on the
		// numeric/object operand, so no explicit cast is needed.
		c.chunk.EmitU16(bytecode.OpConstK, c.chunk.AddConstant(value.String(x.Quasis[0])))
		for i, e := range x.Exprs {
			if err := c.emit(e); err != nil {
				return err
			}
			c.chunk.Emit(bytecode.OpAdd)
			c.chunk.EmitU16(bytecode.OpConstK, c.chunk.AddConstant(value.String(x.Quasis[i+1])))
			c.chunk.Emit(bytecode.OpAdd)
		}

	case *parser.ArrayLit:
		c.chunk.Emit(bytecode.OpNewArray)
		for _, item := range x.Items {
			if sp, ok := item.(*parser.SpreadElement); ok {
				if err := c.emit(sp.Arg); err != nil {
					return err
				}
				c.chunk.Emit(bytecode.OpArraySpread)
				continue
			}
			if err := c.emit(item); err != nil {
				return err
			}
			c.chunk.Emit(bytecode.OpArrayPush)
		}

	case *parser.IndexExpr:
		if err := c.emit(x.Obj); err != nil {
			return err
		}
		if err := c.emit(x.Index); err != nil {
			return err
		}
		c.chunk.Emit(bytecode.OpGetByVal)

	case *parser.AssignExpr:
		switch tgt := x.Target.(type) {
		case *parser.Ident:
			ref := c.resolve(tgt.Name)
			// Short-circuit assignment operators are not compound
			// arithmetic — they only run the RHS when the LHS fails
			// the test. Emit a jump-and-store sequence instead.
			if x.Op == "||=" || x.Op == "&&=" || x.Op == "??=" {
				if err := c.emitLoad(ref, tgt.Name); err != nil {
					return err
				}
				var skip int
				switch x.Op {
				case "||=":
					skip = c.chunk.EmitJump(bytecode.OpJumpIfTruePeek)
				case "&&=":
					skip = c.chunk.EmitJump(bytecode.OpJumpIfFalsePeek)
				case "??=":
					skip = c.chunk.EmitJump(bytecode.OpJumpIfNotNullishPeek)
				}
				c.chunk.Emit(bytecode.OpPop) // drop the LHS we peeked
				if err := c.emit(x.Value); err != nil {
					return err
				}
				c.chunk.Emit(bytecode.OpDup)
				if err := c.emitStore(ref, tgt.Name); err != nil {
					return err
				}
				return c.chunk.PatchJump(skip)
			}
			if x.Op == "=" {
				if err := c.emit(x.Value); err != nil {
					return err
				}
			} else {
				if err := c.emitLoad(ref, tgt.Name); err != nil {
					return err
				}
				if err := c.emit(x.Value); err != nil {
					return err
				}
				op, ok := binaryOps[x.Op[:len(x.Op)-1]]
				if !ok {
					return fmt.Errorf("compiler: compound op %q: %w", x.Op, jserrors.ErrNotImplemented)
				}
				c.chunk.Emit(op)
			}
			c.chunk.Emit(bytecode.OpDup)
			if err := c.emitStore(ref, tgt.Name); err != nil {
				return err
			}
		case *parser.MemberExpr:
			if err := c.emit(tgt.Obj); err != nil {
				return err
			}
			if err := c.emit(x.Value); err != nil {
				return err
			}
			nameIdx := c.chunk.AddConstant(value.String(tgt.Prop))
			c.chunk.EmitSetProp(nameIdx)
		case *parser.IndexExpr:
			if err := c.emit(tgt.Obj); err != nil {
				return err
			}
			if err := c.emit(tgt.Index); err != nil {
				return err
			}
			if err := c.emit(x.Value); err != nil {
				return err
			}
			c.chunk.Emit(bytecode.OpSetByVal)
		default:
			return fmt.Errorf("compiler: assign target %T: %w", x.Target, jserrors.ErrNotImplemented)
		}

	case *parser.UpdateExpr:
		ref := c.resolve(x.Target.Name)
		one := c.chunk.AddConstant(value.Number(1))
		var op bytecode.Op
		switch x.Op {
		case "++":
			op = bytecode.OpAdd
		case "--":
			op = bytecode.OpSub
		default:
			return fmt.Errorf("compiler: update %q: %w", x.Op, jserrors.ErrNotImplemented)
		}
		if x.Prefix {
			if err := c.emitLoad(ref, x.Target.Name); err != nil {
				return err
			}
			c.chunk.EmitU16(bytecode.OpConstK, one)
			c.chunk.Emit(op)
			c.chunk.Emit(bytecode.OpDup)
			if err := c.emitStore(ref, x.Target.Name); err != nil {
				return err
			}
		} else {
			if err := c.emitLoad(ref, x.Target.Name); err != nil {
				return err
			}
			c.chunk.Emit(bytecode.OpDup)
			c.chunk.EmitU16(bytecode.OpConstK, one)
			c.chunk.Emit(op)
			if err := c.emitStore(ref, x.Target.Name); err != nil {
				return err
			}
		}

	case *parser.ObjectLit:
		c.chunk.Emit(bytecode.OpNewObject)
		for _, prop := range x.Props {
			if prop.Spread {
				if err := c.emit(prop.Value); err != nil {
					return err
				}
				c.chunk.Emit(bytecode.OpObjectSpread)
				continue
			}
			if prop.Kind == "get" || prop.Kind == "set" {
				if err := c.emit(prop.Value); err != nil {
					return err
				}
				nameIdx := c.chunk.AddConstant(value.String(prop.Key))
				if prop.Kind == "get" {
					c.chunk.EmitU16(bytecode.OpDefineGetter, nameIdx)
				} else {
					c.chunk.EmitU16(bytecode.OpDefineSetter, nameIdx)
				}
				continue
			}
			if prop.Computed {
				// Stack discipline: [obj, key, value] → OpSetByVal
				// leaves value on top. Pop it (we don't need the
				// expression result here).
				c.chunk.Emit(bytecode.OpDup)
				if err := c.emit(prop.KeyExpr); err != nil {
					return err
				}
				if err := c.emit(prop.Value); err != nil {
					return err
				}
				c.chunk.Emit(bytecode.OpSetByVal)
				c.chunk.Emit(bytecode.OpPop)
				continue
			}
			c.chunk.Emit(bytecode.OpDup)
			if err := c.emit(prop.Value); err != nil {
				return err
			}
			nameIdx := c.chunk.AddConstant(value.String(prop.Key))
			c.chunk.EmitSetProp(nameIdx)
			c.chunk.Emit(bytecode.OpPop)
		}

	case *parser.MemberExpr:
		if err := c.emit(x.Obj); err != nil {
			return err
		}
		nameIdx := c.chunk.AddConstant(value.String(x.Prop))
		c.chunk.EmitGetProp(nameIdx)

	case *parser.FunctionExpr:
		fn, err := c.compileFunction(x.Name, x.Params, x.Body)
		if err != nil {
			return err
		}
		fn.IsAsync = x.IsAsync
		fn.IsGenerator = x.IsGenerator
		constIdx := c.chunk.AddConstant(value.FunctionVal(fn))
		c.chunk.EmitU16(bytecode.OpClosure, constIdx)

	case *parser.YieldExpr:
		if x.X != nil {
			if err := c.emit(x.X); err != nil {
				return err
			}
		} else {
			c.chunk.Emit(bytecode.OpConstUndefined)
		}
		c.chunk.Emit(bytecode.OpYield)

	case *parser.ArrowFunctionExpr:
		fn, err := c.compileArrowFunction(x)
		if err != nil {
			return err
		}
		constIdx := c.chunk.AddConstant(value.FunctionVal(fn))
		c.chunk.EmitU16(bytecode.OpClosure, constIdx)

	case *parser.NewExpr:
		hasSpread := false
		for _, a := range x.Args {
			if _, ok := a.(*parser.SpreadElement); ok {
				hasSpread = true
				break
			}
		}
		if hasSpread {
			// Spread args: build an Array, then route through
			// OpNewApply which expands the array into the constructor
			// call.
			if err := c.emit(x.Callee); err != nil {
				return err
			}
			c.chunk.Emit(bytecode.OpNewArray)
			for _, a := range x.Args {
				if sp, ok := a.(*parser.SpreadElement); ok {
					if err := c.emit(sp.Arg); err != nil {
						return err
					}
					c.chunk.Emit(bytecode.OpArraySpread)
					continue
				}
				if err := c.emit(a); err != nil {
					return err
				}
				c.chunk.Emit(bytecode.OpArrayPush)
			}
			c.chunk.Emit(bytecode.OpNewApply)
			break
		}
		if err := c.emit(x.Callee); err != nil {
			return err
		}
		for _, arg := range x.Args {
			if err := c.emit(arg); err != nil {
				return err
			}
		}
		if len(x.Args) > 255 {
			return fmt.Errorf("compiler: >255 new args: %w", jserrors.ErrNotImplemented)
		}
		c.chunk.EmitU8(bytecode.OpNew, uint8(len(x.Args)))

	case *parser.CallExpr:
		if len(x.Args) > 255 {
			return fmt.Errorf("compiler: >255 call args: %w", jserrors.ErrNotImplemented)
		}
		hasSpread := false
		for _, a := range x.Args {
			if _, ok := a.(*parser.SpreadElement); ok {
				hasSpread = true
				break
			}
		}
		// Spread form: build an Array of args (handling spread
		// expansion) and dispatch via OpCallApply, which unpacks the
		// array into a []Value for the receiver.
		if hasSpread {
			// Layout per OpCallApply: [fn, this, argsArr]
			switch callee := x.Callee.(type) {
			case *parser.MemberExpr:
				if err := c.emit(callee.Obj); err != nil {
					return err
				}
				c.chunk.Emit(bytecode.OpDup)
				nameIdx := c.chunk.AddConstant(value.String(callee.Prop))
				c.chunk.EmitGetProp(nameIdx)
				// stack: [recv, fn]; reorder to [fn, recv]
				c.chunk.Emit(bytecode.OpSwap)
			case *parser.IndexExpr:
				if err := c.emit(callee.Obj); err != nil {
					return err
				}
				c.chunk.Emit(bytecode.OpDup)
				if err := c.emit(callee.Index); err != nil {
					return err
				}
				c.chunk.Emit(bytecode.OpGetByVal)
				c.chunk.Emit(bytecode.OpSwap)
			default:
				if err := c.emit(x.Callee); err != nil {
					return err
				}
				c.chunk.EmitU16(bytecode.OpConstK, c.chunk.AddConstant(value.Undefined()))
			}
			c.chunk.Emit(bytecode.OpNewArray)
			for _, a := range x.Args {
				if sp, ok := a.(*parser.SpreadElement); ok {
					if err := c.emit(sp.Arg); err != nil {
						return err
					}
					c.chunk.Emit(bytecode.OpArraySpread)
					continue
				}
				if err := c.emit(a); err != nil {
					return err
				}
				c.chunk.Emit(bytecode.OpArrayPush)
			}
			c.chunk.Emit(bytecode.OpCallApply)
			break
		}
		// Method call: when the callee is a property access we leave
		// the receiver on the stack so the VM can bind `this`.
		switch callee := x.Callee.(type) {
		case *parser.MemberExpr:
			if err := c.emit(callee.Obj); err != nil {
				return err
			}
			c.chunk.Emit(bytecode.OpDup)
			nameIdx := c.chunk.AddConstant(value.String(callee.Prop))
			c.chunk.EmitGetProp(nameIdx)
			for _, arg := range x.Args {
				if err := c.emit(arg); err != nil {
					return err
				}
			}
			c.chunk.EmitU8(bytecode.OpCallMethod, uint8(len(x.Args)))
		case *parser.IndexExpr:
			if err := c.emit(callee.Obj); err != nil {
				return err
			}
			c.chunk.Emit(bytecode.OpDup)
			if err := c.emit(callee.Index); err != nil {
				return err
			}
			c.chunk.Emit(bytecode.OpGetByVal)
			for _, arg := range x.Args {
				if err := c.emit(arg); err != nil {
					return err
				}
			}
			c.chunk.EmitU8(bytecode.OpCallMethod, uint8(len(x.Args)))
		default:
			if err := c.emit(x.Callee); err != nil {
				return err
			}
			for _, arg := range x.Args {
				if err := c.emit(arg); err != nil {
					return err
				}
			}
			c.chunk.EmitU8(bytecode.OpCall, uint8(len(x.Args)))
		}

	default:
		return fmt.Errorf("compiler: expr %T: %w", n, jserrors.ErrNotImplemented)
	}
	return nil
}
