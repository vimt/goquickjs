package compiler

import (
	"fmt"

	"github.com/vimt/goquickjs/internal/bytecode"
	"github.com/vimt/goquickjs/internal/parser"
	"github.com/vimt/goquickjs/internal/value"
)

// emitDestructure walks `pat`, reading from src (named srcName so
// global-scope stores work), declaring user bindings for each leaf
// IdentTarget, and recursing into nested patterns. Defaults are
// applied when the read value is exactly undefined.
func (c *compiler) emitDestructure(pat parser.Pattern, src symRef, srcName string) error {
	switch p := pat.(type) {
	case *parser.ObjectPattern:
		consumed := map[string]bool{}
		for _, prop := range p.Props {
			if prop.IsRest {
				// Build {key: src[key] for key not in consumed}.
				c.chunk.Emit(bytecode.OpNewObject)
				// We need to copy own props of src skipping consumed
				// keys. The simplest path: spread the source into a
				// fresh object, then delete consumed keys.
				c.emitLoadRef(src, srcName)
				c.chunk.Emit(bytecode.OpObjectSpread)
				for k := range consumed {
					c.chunk.Emit(bytecode.OpDup)
					nameIdx := c.chunk.AddConstant(value.String(k))
					c.chunk.EmitU16(bytecode.OpDeleteProp, nameIdx)
					c.chunk.Emit(bytecode.OpPop)
				}
				ref, err := c.declare(prop.Target.(*parser.IdentTarget).Name)
				if err != nil {
					return err
				}
				if err := c.emitStore(ref, prop.Target.(*parser.IdentTarget).Name); err != nil {
					return err
				}
				continue
			}
			consumed[prop.Key] = true
			// Push src[key], applying default if undefined.
			c.emitLoadRef(src, srcName)
			nameIdx := c.chunk.AddConstant(value.String(prop.Key))
			c.chunk.EmitU16(bytecode.OpGetProp, nameIdx)
			if prop.Default != nil {
				if err := c.emitDefault(prop.Default); err != nil {
					return err
				}
			}
			if err := c.emitBindTarget(prop.Target); err != nil {
				return err
			}
		}
	case *parser.ArrayPattern:
		for i, elem := range p.Elements {
			if elem.Skip {
				continue
			}
			if elem.IsRest {
				// rest binding consumes all remaining indices. Emit
				// src.slice(i) and bind.
				c.emitLoadRef(src, srcName)
				c.chunk.Emit(bytecode.OpDup)
				nameIdx := c.chunk.AddConstant(value.String("slice"))
				c.chunk.EmitU16(bytecode.OpGetProp, nameIdx)
				c.chunk.EmitU16(bytecode.OpConstK, c.chunk.AddConstant(value.Number(float64(i))))
				c.chunk.EmitU8(bytecode.OpCallMethod, 1)
				if err := c.emitBindTarget(elem.Target); err != nil {
					return err
				}
				return nil
			}
			c.emitLoadRef(src, srcName)
			c.chunk.EmitU16(bytecode.OpConstK, c.chunk.AddConstant(value.Number(float64(i))))
			c.chunk.Emit(bytecode.OpGetByVal)
			if elem.Default != nil {
				if err := c.emitDefault(elem.Default); err != nil {
					return err
				}
			}
			if err := c.emitBindTarget(elem.Target); err != nil {
				return err
			}
		}
	}
	return nil
}

// emitDefault, given a value already on TOS, replaces it with the
// emitted default expression iff TOS is undefined. Leaves a single
// value on the stack either way.
func (c *compiler) emitDefault(def parser.Node) error {
	// Stack: [v]. We need: if v === undefined: pop; emit def. Else
	// leave v.
	// Sequence:
	//   Dup                      → [v, v]
	//   ConstK undefined         → [v, v, undef]
	//   Equal                    → [v, isUndef]
	//   JumpIfFalse skipDefault  → [v]
	//   Pop                      → []
	//   emit def                 → [def]
	// skipDefault:
	c.chunk.Emit(bytecode.OpDup)
	c.chunk.Emit(bytecode.OpConstUndefined)
	c.chunk.Emit(bytecode.OpStrictEq)
	patch := c.chunk.EmitJump(bytecode.OpJumpIfFalse)
	c.chunk.Emit(bytecode.OpPop)
	if err := c.emit(def); err != nil {
		return err
	}
	return c.chunk.PatchJump(patch)
}

// emitBindTarget consumes TOS — the freshly-pulled value — and binds
// it to the pattern target: declare + store for an Ident; recursive
// destructure for a Nested.
func (c *compiler) emitBindTarget(t parser.PatternTarget) error {
	switch tt := t.(type) {
	case *parser.IdentTarget:
		ref, err := c.declare(tt.Name)
		if err != nil {
			return err
		}
		return c.emitStore(ref, tt.Name)
	case *parser.NestedTarget:
		// Stash TOS into a fresh named binding, then recurse.
		name := c.tempName()
		tmp, err := c.declare(name)
		if err != nil {
			return err
		}
		if err := c.emitStore(tmp, name); err != nil {
			return err
		}
		return c.emitDestructure(tt.Pattern, tmp, name)
	}
	return fmt.Errorf("compiler: unknown pattern target %T", t)
}
