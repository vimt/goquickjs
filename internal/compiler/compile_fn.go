package compiler

import (
	"fmt"
	"unsafe"

	"github.com/vimt/goquickjs/internal/bytecode"
	"github.com/vimt/goquickjs/internal/parser"
	"github.com/vimt/goquickjs/internal/value"
)

// compileFunction lowers a function body into a fresh Chunk wrapped
// in a Function. Parameters carry default initializers, rest
// collection, and destructuring patterns; each is declared as a
// frame local and any default / destructure code is prepended to the
// body.
func (c *compiler) compileFunction(name string, params []parser.Param, body *parser.Block) (*value.Function, error) {
	inner := newCompiler(c)
	hasRest, err := inner.declareParams(params)
	if err != nil {
		return nil, err
	}
	// Reserve a slot for `arguments`. Non-arrow function bodies can
	// reference it; the VM seeds the slot with an Array snapshot of
	// the call's positional args at frame entry.
	argRef, err := inner.declare("arguments")
	if err != nil {
		return nil, err
	}
	for _, stmt := range body.Body {
		if err := inner.emitStmt(stmt, false); err != nil {
			return nil, err
		}
	}
	inner.chunk.Emit(bytecode.OpConstUndefined)
	inner.chunk.Emit(bytecode.OpReturn)
	inner.chunk.MaxLocals = inner.maxSlots
	arity := len(params)
	if hasRest {
		arity--
	}
	return &value.Function{
		Name:          name,
		Arity:         arity,
		HasRest:       hasRest,
		HasArguments:  true,
		ArgumentsSlot: argRef.slot,
		Body:          unsafe.Pointer(inner.chunk),
		UpvalueDescs:  inner.upvalueDescs,
	}, nil
}

// compileArrowFunction handles both block-body and expression-body
// arrow forms; the result carries IsArrow=true so the VM binds the
// captured `this` rather than the caller's.
func (c *compiler) compileArrowFunction(x *parser.ArrowFunctionExpr) (*value.Function, error) {
	inner := newCompiler(c)
	hasRest, err := inner.declareParams(x.Params)
	if err != nil {
		return nil, err
	}
	if x.ExprBody != nil {
		if err := inner.emit(x.ExprBody); err != nil {
			return nil, err
		}
		inner.chunk.Emit(bytecode.OpReturn)
	} else {
		for _, stmt := range x.Body.Body {
			if err := inner.emitStmt(stmt, false); err != nil {
				return nil, err
			}
		}
		inner.chunk.Emit(bytecode.OpConstUndefined)
		inner.chunk.Emit(bytecode.OpReturn)
	}
	inner.chunk.MaxLocals = inner.maxSlots
	arity := len(x.Params)
	if hasRest {
		arity--
	}
	return &value.Function{
		Name:         "",
		Arity:        arity,
		HasRest:      hasRest,
		Body:         unsafe.Pointer(inner.chunk),
		UpvalueDescs: inner.upvalueDescs,
		IsArrow:      true,
	}, nil
}

// declareParams walks the formal-parameter list, allocating a slot
// per parameter and emitting any per-param initialization code:
//   - rest: caller's surplus already lives in the slot (doCall fills it)
//   - default: `if (slot === undefined) slot = <default>`
//   - pattern: destructure from the slot into the pattern's bindings
//
// Returns whether the trailing param was a rest.
func (c *compiler) declareParams(params []parser.Param) (bool, error) {
	hasRest := false
	for i, p := range params {
		if p.Rest {
			hasRest = true
			if i != len(params)-1 {
				return false, fmt.Errorf("compiler: rest param must be last")
			}
			if _, err := c.declare(p.Name); err != nil {
				return false, err
			}
			continue
		}
		// Pattern param: declare a synthetic slot to hold the raw
		// argument, then run destructure logic to bind the user-named
		// pieces.
		if p.Pattern != nil {
			tmpName := c.tempName()
			tmpRef, err := c.declare(tmpName)
			if err != nil {
				return false, err
			}
			if p.Default != nil {
				if err := c.emitParamDefault(tmpRef, tmpName, p.Default); err != nil {
					return false, err
				}
			}
			if err := c.emitDestructure(p.Pattern, tmpRef, tmpName); err != nil {
				return false, err
			}
			continue
		}
		ref, err := c.declare(p.Name)
		if err != nil {
			return false, err
		}
		if p.Default != nil {
			if err := c.emitParamDefault(ref, p.Name, p.Default); err != nil {
				return false, err
			}
		}
	}
	return hasRest, nil
}

// emitParamDefault prepends `if (ref === undefined) ref = default`
// to the function body. Used for both plain and pattern params.
func (c *compiler) emitParamDefault(ref symRef, name string, def parser.Node) error {
	c.emitLoadRef(ref, name)
	c.chunk.Emit(bytecode.OpConstUndefined)
	c.chunk.Emit(bytecode.OpStrictEq)
	patch := c.chunk.EmitJump(bytecode.OpJumpIfFalse)
	if err := c.emit(def); err != nil {
		return err
	}
	if err := c.emitStore(ref, name); err != nil {
		return err
	}
	return c.chunk.PatchJump(patch)
}

// emitClassDecl lowers a ClassDecl into a constructor function plus
// per-method prototype/static assignments. extends is implemented by
// calling Object.setPrototypeOf(ChildProto, ParentProto) at decl
// time; the default ctor for a subclass forwards args via
// Parent.call(this, ...args).
func (c *compiler) emitClassDecl(x *parser.ClassDecl, keepLastExpr bool) error {
	var ctor *parser.ClassMember
	for i := range x.Members {
		m := &x.Members[i]
		if m.IsConstructor {
			ctor = m
			break
		}
	}
	var ctorFn *value.Function
	if ctor != nil {
		fn, err := c.compileFunction(x.Name, ctor.Params, ctor.Body)
		if err != nil {
			return err
		}
		ctorFn = fn
	} else if x.Parent != nil {
		body := &parser.Block{Body: []parser.Node{
			&parser.ExprStmt{X: &parser.CallExpr{
				Callee: &parser.MemberExpr{Obj: x.Parent, Prop: "call"},
				Args: []parser.Node{
					&parser.ThisExpr{},
					&parser.SpreadElement{Arg: &parser.Ident{Name: "args"}},
				},
			}},
		}}
		fn, err := c.compileFunction(x.Name, []parser.Param{{Name: "args", Rest: true}}, body)
		if err != nil {
			return err
		}
		ctorFn = fn
	} else {
		fn, err := c.compileFunction(x.Name, nil, &parser.Block{Body: nil})
		if err != nil {
			return err
		}
		ctorFn = fn
	}

	classRef, err := c.declare(x.Name)
	if err != nil {
		return err
	}
	c.chunk.EmitU16(bytecode.OpClosure, c.chunk.AddConstant(value.FunctionVal(ctorFn)))
	if err := c.emitStore(classRef, x.Name); err != nil {
		return err
	}

	if x.Parent != nil {
		c.chunk.EmitU16(bytecode.OpLoadGlobal, c.chunk.AddConstant(value.String("Object")))
		c.chunk.Emit(bytecode.OpDup)
		c.chunk.EmitU16(bytecode.OpGetProp, c.chunk.AddConstant(value.String("setPrototypeOf")))
		c.emitLoadRef(classRef, x.Name)
		protoName := c.chunk.AddConstant(value.String("prototype"))
		c.chunk.EmitU16(bytecode.OpGetProp, protoName)
		if err := c.emit(x.Parent); err != nil {
			return err
		}
		c.chunk.EmitU16(bytecode.OpGetProp, protoName)
		c.chunk.EmitU8(bytecode.OpCallMethod, 2)
		c.chunk.Emit(bytecode.OpPop)
	}

	for i := range x.Members {
		m := &x.Members[i]
		if m.IsConstructor {
			continue
		}
		methodFn, err := c.compileFunction(m.Name, m.Params, m.Body)
		if err != nil {
			return err
		}
		c.emitLoadRef(classRef, x.Name)
		if !m.IsStatic {
			c.chunk.EmitU16(bytecode.OpGetProp, c.chunk.AddConstant(value.String("prototype")))
		}
		c.chunk.EmitU16(bytecode.OpClosure, c.chunk.AddConstant(value.FunctionVal(methodFn)))
		nameIdx := c.chunk.AddConstant(value.String(m.Name))
		switch m.Kind {
		case "get":
			c.chunk.EmitU16(bytecode.OpDefineGetter, nameIdx)
		case "set":
			c.chunk.EmitU16(bytecode.OpDefineSetter, nameIdx)
		default:
			c.chunk.EmitU16(bytecode.OpSetProp, nameIdx)
		}
		c.chunk.Emit(bytecode.OpPop)
	}

	if keepLastExpr {
		c.chunk.Emit(bytecode.OpConstUndefined)
	}
	return nil
}
