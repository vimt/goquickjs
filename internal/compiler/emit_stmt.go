package compiler

import (
	"fmt"

	"github.com/vimt/goquickjs/internal/bytecode"
	"github.com/vimt/goquickjs/internal/jserrors"
	"github.com/vimt/goquickjs/internal/parser"
	"github.com/vimt/goquickjs/internal/value"
)

// emitStmt emits a statement. When keepLastExpr is true and the
// statement yields a value (ExprStmt or if-else with valued branch),
// that value is left on the stack as the program's completion;
// otherwise an explicit undefined is pushed for non-expression
// statements so completion remains well-defined.
func (c *compiler) emitStmt(n parser.Node, keepLastExpr bool) error {
	switch x := n.(type) {
	case *parser.ExprStmt:
		if err := c.emit(x.X); err != nil {
			return err
		}
		if !keepLastExpr {
			c.chunk.Emit(bytecode.OpPop)
		}
		return nil

	case *parser.MultiVarDecl:
		for _, d := range x.Decls {
			if err := c.emitStmt(d, false); err != nil {
				return err
			}
		}
		if keepLastExpr {
			c.chunk.Emit(bytecode.OpConstUndefined)
		}
		return nil

	case *parser.LetDecl:
		ref, err := c.declare(x.Name)
		if err != nil {
			return err
		}
		if x.Init != nil {
			if err := c.emit(x.Init); err != nil {
				return err
			}
		} else {
			c.chunk.Emit(bytecode.OpConstUndefined)
		}
		if err := c.emitStore(ref, x.Name); err != nil {
			return err
		}
		if keepLastExpr {
			c.chunk.Emit(bytecode.OpConstUndefined)
		}
		return nil

	case *parser.DestructureDecl:
		// Stash the source into a fresh hidden binding. At top level
		// this becomes a global-by-name entry; the unique tempName
		// keeps it from colliding with anything user-visible.
		srcName := c.tempName()
		srcRef, err := c.declare(srcName)
		if err != nil {
			return err
		}
		if err := c.emit(x.Init); err != nil {
			return err
		}
		if err := c.emitStore(srcRef, srcName); err != nil {
			return err
		}
		if err := c.emitDestructure(x.Pattern, srcRef, srcName); err != nil {
			return err
		}
		if keepLastExpr {
			c.chunk.Emit(bytecode.OpConstUndefined)
		}
		return nil

	case *parser.Block:
		c.enterScope()
		for _, s := range x.Body {
			if err := c.emitStmt(s, false); err != nil {
				c.exitScope()
				return err
			}
		}
		c.exitScope()
		if keepLastExpr {
			c.chunk.Emit(bytecode.OpConstUndefined)
		}
		return nil

	case *parser.ForStmt:
		if err := c.emitFor(x); err != nil {
			return err
		}
		if keepLastExpr {
			c.chunk.Emit(bytecode.OpConstUndefined)
		}
		return nil

	case *parser.ForInStmt:
		if err := c.emitForIn(x); err != nil {
			return err
		}
		if keepLastExpr {
			c.chunk.Emit(bytecode.OpConstUndefined)
		}
		return nil

	case *parser.ForOfStmt:
		if err := c.emitForOf(x); err != nil {
			return err
		}
		if keepLastExpr {
			c.chunk.Emit(bytecode.OpConstUndefined)
		}
		return nil

	case *parser.IfStmt:
		return c.emitIf(x, keepLastExpr)

	case *parser.ReturnStmt:
		if x.Arg != nil {
			if err := c.emit(x.Arg); err != nil {
				return err
			}
		} else {
			c.chunk.Emit(bytecode.OpConstUndefined)
		}
		c.chunk.Emit(bytecode.OpReturn)
		return nil

	case *parser.WhileStmt:
		if err := c.emitWhile(x); err != nil {
			return err
		}
		if keepLastExpr {
			c.chunk.Emit(bytecode.OpConstUndefined)
		}
		return nil

	case *parser.DoWhileStmt:
		if err := c.emitDoWhile(x); err != nil {
			return err
		}
		if keepLastExpr {
			c.chunk.Emit(bytecode.OpConstUndefined)
		}
		return nil

	case *parser.SwitchStmt:
		if err := c.emitSwitch(x); err != nil {
			return err
		}
		if keepLastExpr {
			c.chunk.Emit(bytecode.OpConstUndefined)
		}
		return nil

	case *parser.BreakStmt:
		if len(c.loops) == 0 {
			return fmt.Errorf("compiler: 'break' outside of loop/switch")
		}
		var target *loopFrame
		if x.Label != "" {
			for i := len(c.loops) - 1; i >= 0; i-- {
				if c.loops[i].label == x.Label {
					target = c.loops[i]
					break
				}
			}
			if target == nil {
				return fmt.Errorf("compiler: break label %q not found", x.Label)
			}
		} else {
			target = c.loops[len(c.loops)-1]
		}
		target.breakJumps = append(target.breakJumps, c.chunk.EmitJump(bytecode.OpJump))
		return nil

	case *parser.ContinueStmt:
		if x.Label != "" {
			for i := len(c.loops) - 1; i >= 0; i-- {
				if c.loops[i].label == x.Label && c.loops[i].catchContinue {
					c.loops[i].continueJumps = append(c.loops[i].continueJumps, c.chunk.EmitJump(bytecode.OpJump))
					return nil
				}
			}
			return fmt.Errorf("compiler: continue label %q not found on a loop", x.Label)
		}
		// continue belongs to the innermost loop that catches it
		// (i.e. skip enclosing switch frames).
		for i := len(c.loops) - 1; i >= 0; i-- {
			if c.loops[i].catchContinue {
				c.loops[i].continueJumps = append(c.loops[i].continueJumps, c.chunk.EmitJump(bytecode.OpJump))
				return nil
			}
		}
		return fmt.Errorf("compiler: 'continue' outside of loop")

	case *parser.LabeledStmt:
		// Stash the label so the next loop/switch frame picks it up,
		// then emit the body. We use a one-slot field on compiler;
		// the loop emit routines consume it during enterScope-equiv.
		c.pendingLabel = x.Label
		if err := c.emitStmt(x.Body, keepLastExpr); err != nil {
			return err
		}
		c.pendingLabel = ""
		return nil

	case *parser.ThrowStmt:
		if err := c.emit(x.Arg); err != nil {
			return err
		}
		c.chunk.Emit(bytecode.OpThrow)
		return nil

	case *parser.TryStmt:
		startPC := len(c.chunk.Code)
		c.enterScope()
		if err := c.emitStmtsKeepLast(x.Body.Body, keepLastExpr); err != nil {
			c.exitScope()
			return err
		}
		c.exitScope()
		endPC := len(c.chunk.Code)
		jumpEnd := c.chunk.EmitJump(bytecode.OpJump)
		// Catch arm — present if x.CatchBody != nil. When absent we
		// register the handler anyway and immediately rethrow so a
		// finally clause still runs.
		handlerPC := len(c.chunk.Code)
		// Sum the operand-stack slots enclosing constructs leave live
		// across the try body — for-of/for-in iterators, switch
		// discriminants. The VM truncates to base+depth on throw.
		depth := 0
		for _, l := range c.loops {
			depth += l.stackItems
		}
		c.chunk.AddHandler(startPC, endPC, handlerPC, depth)
		if x.CatchBody != nil {
			c.enterScope()
			if x.CatchParam != "" {
				catchRef, err := c.declare(x.CatchParam)
				if err != nil {
					c.exitScope()
					return err
				}
				if err := c.emitStore(catchRef, x.CatchParam); err != nil {
					c.exitScope()
					return err
				}
			} else {
				c.chunk.Emit(bytecode.OpPop) // discard the thrown value
			}
			if err := c.emitStmtsKeepLast(x.CatchBody.Body, keepLastExpr); err != nil {
				c.exitScope()
				return err
			}
			c.exitScope()
		} else {
			// No catch — rethrow whatever's on top so the next outer
			// handler (or the program boundary) sees it. The pending
			// finally still runs because we patch jumpEnd to fall
			// through to it.
			c.chunk.Emit(bytecode.OpThrow)
		}
		if err := c.chunk.PatchJump(jumpEnd); err != nil {
			return err
		}
		// finally — runs on both the normal-exit and catch-exit
		// paths. We don't yet handle the case where the catch body
		// itself throws (the new throw skips the finally), but for
		// the common "cleanup after success or after a caught error"
		// it's correct.
		if x.FinallyBody != nil {
			c.enterScope()
			if err := c.emitStmtsKeepLast(x.FinallyBody.Body, false); err != nil {
				c.exitScope()
				return err
			}
			c.exitScope()
		}
		return nil

	case *parser.ClassDecl:
		return c.emitClassDecl(x, keepLastExpr)

	case *parser.FunctionDecl:
		fn, err := c.compileFunction(x.Name, x.Params, x.Body)
		if err != nil {
			return err
		}
		fn.IsAsync = x.IsAsync
		fn.IsGenerator = x.IsGenerator
		constIdx := c.chunk.AddConstant(value.FunctionVal(fn))
		// OpClosure (not OpConstK): instantiates the proto with
		// upvalues captured from the current frame.
		c.chunk.EmitU16(bytecode.OpClosure, constIdx)
		ref, err := c.declare(x.Name)
		if err != nil {
			return err
		}
		if err := c.emitStore(ref, x.Name); err != nil {
			return err
		}
		if keepLastExpr {
			c.chunk.Emit(bytecode.OpConstUndefined)
		}
		return nil
	}
	return fmt.Errorf("compiler: stmt %T: %w", n, jserrors.ErrNotImplemented)
}

// emitStmtsKeepLast emits a list of statements; only the last one
// gets keepLast=true (mirroring JS Block completion semantics).
// Used by Block, try-body, and catch-body so the completion value
// of `try { ...; expr }` is `expr`.
func (c *compiler) emitStmtsKeepLast(stmts []parser.Node, keepLast bool) error {
	if len(stmts) == 0 {
		if keepLast {
			c.chunk.Emit(bytecode.OpConstUndefined)
		}
		return nil
	}
	for i, s := range stmts {
		last := i == len(stmts)-1
		if err := c.emitStmt(s, last && keepLast); err != nil {
			return err
		}
	}
	return nil
}

func (c *compiler) emitIf(x *parser.IfStmt, keepLastExpr bool) error {
	if err := c.emit(x.Test); err != nil {
		return err
	}
	elseJump := c.chunk.EmitJump(bytecode.OpJumpIfFalse)
	if err := c.emitStmt(x.Cons, keepLastExpr); err != nil {
		return err
	}
	if x.Alt != nil {
		endJump := c.chunk.EmitJump(bytecode.OpJump)
		if err := c.chunk.PatchJump(elseJump); err != nil {
			return err
		}
		if err := c.emitStmt(x.Alt, keepLastExpr); err != nil {
			return err
		}
		return c.chunk.PatchJump(endJump)
	}
	// No else. If keepLastExpr and the test was false, completion
	// should be undefined — push it in the skipped branch.
	if keepLastExpr {
		endJump := c.chunk.EmitJump(bytecode.OpJump)
		if err := c.chunk.PatchJump(elseJump); err != nil {
			return err
		}
		c.chunk.Emit(bytecode.OpConstUndefined)
		return c.chunk.PatchJump(endJump)
	}
	return c.chunk.PatchJump(elseJump)
}

func (c *compiler) emitFor(x *parser.ForStmt) error {
	c.enterScope()
	defer c.exitScope()

	if x.Init != nil {
		if err := c.emitStmt(x.Init, false); err != nil {
			return err
		}
	}

	lf := &loopFrame{catchContinue: true, label: c.pendingLabel}; c.pendingLabel = ""
	c.loops = append(c.loops, lf)

	loopStart := len(c.chunk.Code)
	endPatch := -1
	if x.Test != nil {
		if err := c.emit(x.Test); err != nil {
			c.loops = c.loops[:len(c.loops)-1]
			return err
		}
		endPatch = c.chunk.EmitJump(bytecode.OpJumpIfFalse)
	}
	if x.Body != nil {
		if err := c.emitStmt(x.Body, false); err != nil {
			c.loops = c.loops[:len(c.loops)-1]
			return err
		}
	}
	// continue lands here so the update expression still runs.
	continueTarget := len(c.chunk.Code)
	if x.Update != nil {
		if err := c.emit(x.Update); err != nil {
			c.loops = c.loops[:len(c.loops)-1]
			return err
		}
		c.chunk.Emit(bytecode.OpPop)
	}
	if err := c.chunk.EmitBackJump(bytecode.OpJump, loopStart); err != nil {
		c.loops = c.loops[:len(c.loops)-1]
		return err
	}
	if endPatch >= 0 {
		if err := c.chunk.PatchJump(endPatch); err != nil {
			c.loops = c.loops[:len(c.loops)-1]
			return err
		}
	}
	breakTarget := len(c.chunk.Code)
	for _, p := range lf.continueJumps {
		if err := c.chunk.PatchJumpTo(p, continueTarget); err != nil {
			c.loops = c.loops[:len(c.loops)-1]
			return err
		}
	}
	for _, p := range lf.breakJumps {
		if err := c.chunk.PatchJumpTo(p, breakTarget); err != nil {
			c.loops = c.loops[:len(c.loops)-1]
			return err
		}
	}
	c.loops = c.loops[:len(c.loops)-1]
	return nil
}

func (c *compiler) emitDoWhile(x *parser.DoWhileStmt) error {
	lf := &loopFrame{catchContinue: true, label: c.pendingLabel}; c.pendingLabel = ""
	c.loops = append(c.loops, lf)

	loopStart := len(c.chunk.Code)
	if err := c.emitStmt(x.Body, false); err != nil {
		c.loops = c.loops[:len(c.loops)-1]
		return err
	}
	// continue lands on the test, so the next iteration's condition
	// is re-evaluated rather than the body re-running unconditionally.
	continueTarget := len(c.chunk.Code)
	if err := c.emit(x.Test); err != nil {
		c.loops = c.loops[:len(c.loops)-1]
		return err
	}
	if err := c.chunk.EmitBackJump(bytecode.OpJumpIfTrue, loopStart); err != nil {
		c.loops = c.loops[:len(c.loops)-1]
		return err
	}
	breakTarget := len(c.chunk.Code)
	for _, p := range lf.continueJumps {
		if err := c.chunk.PatchJumpTo(p, continueTarget); err != nil {
			c.loops = c.loops[:len(c.loops)-1]
			return err
		}
	}
	for _, p := range lf.breakJumps {
		if err := c.chunk.PatchJumpTo(p, breakTarget); err != nil {
			c.loops = c.loops[:len(c.loops)-1]
			return err
		}
	}
	c.loops = c.loops[:len(c.loops)-1]
	return nil
}

func (c *compiler) emitWhile(x *parser.WhileStmt) error {
	lf := &loopFrame{catchContinue: true, label: c.pendingLabel}; c.pendingLabel = ""
	c.loops = append(c.loops, lf)

	loopStart := len(c.chunk.Code)
	if err := c.emit(x.Test); err != nil {
		c.loops = c.loops[:len(c.loops)-1]
		return err
	}
	endPatch := c.chunk.EmitJump(bytecode.OpJumpIfFalse)
	if err := c.emitStmt(x.Body, false); err != nil {
		c.loops = c.loops[:len(c.loops)-1]
		return err
	}
	if err := c.chunk.EmitBackJump(bytecode.OpJump, loopStart); err != nil {
		c.loops = c.loops[:len(c.loops)-1]
		return err
	}
	if err := c.chunk.PatchJump(endPatch); err != nil {
		c.loops = c.loops[:len(c.loops)-1]
		return err
	}
	breakTarget := len(c.chunk.Code)
	for _, p := range lf.continueJumps {
		if err := c.chunk.PatchJumpTo(p, loopStart); err != nil {
			c.loops = c.loops[:len(c.loops)-1]
			return err
		}
	}
	for _, p := range lf.breakJumps {
		if err := c.chunk.PatchJumpTo(p, breakTarget); err != nil {
			c.loops = c.loops[:len(c.loops)-1]
			return err
		}
	}
	c.loops = c.loops[:len(c.loops)-1]
	return nil
}

// emitForOf compiles `for (let x of iterable) body`. Lays out the
// stack as [iter] across the loop; each iteration grows it to
// [iter, result] then to [iter, value] which we store and pop down
// to [iter]; the cleanup at the end pops both the result (on the
// done-branch) and the iterator.
//
// Stack contract:
//
//	loopStart:  [iter]
//	            Dup; GetProp "next"; CallMethod 0     → [iter, result]
//	            Dup; GetProp "done"; JumpIfTrue end   → [iter, result]
//	            GetProp "value"; Store x              → [iter]
//	            body                                   → [iter]
//	            Jump loopStart
//	end:        [iter, result] → Pop; Pop             → []
//
// Break leaves the body with [iter] on the stack and jumps to the
// "popIter" label (skipping the result-pop that the done-path uses).
// emitForIn shares the loop scaffold with emitForOf — the only
// difference is the iterator source: for-in pushes the obj's keys
// array, then iterates that. Lowering keeps the loop bytecode
// identical so break / continue / scope semantics carry over.
func (c *compiler) emitForIn(x *parser.ForInStmt) error {
	if err := c.emit(x.Obj); err != nil {
		return err
	}
	c.chunk.Emit(bytecode.OpForInKeys)
	c.chunk.Emit(bytecode.OpGetIterator)
	return c.emitIterLoop(x.Name, x.AssignTo, x.Body)
}

func (c *compiler) emitForOf(x *parser.ForOfStmt) error {
	if err := c.emit(x.Iterable); err != nil {
		return err
	}
	c.chunk.Emit(bytecode.OpGetIterator)
	return c.emitIterLoop(x.Name, x.AssignTo, x.Body)
}

// emitIterLoop assumes the iterator value is on top of the stack
// and emits the same `while (!result.done) bind = result.value`
// loop body for both for-of and for-in callers. Exactly one of
// `name` (declaration form) and `assignTo` (assignment form) is set.
func (c *compiler) emitIterLoop(name string, assignTo parser.Node, body parser.Node) error {

	c.enterScope()
	defer c.exitScope()

	var ref symRef
	if name != "" {
		r, err := c.declare(name)
		if err != nil {
			return err
		}
		ref = r
	}

	// stackItems=1 — emitForOf/emitForIn leave the iterator on the
	// operand stack across the whole loop body, so a try/catch inside
	// the body must preserve that slot when it unwinds.
	lf := &loopFrame{catchContinue: true, label: c.pendingLabel, stackItems: 1}; c.pendingLabel = ""
	c.loops = append(c.loops, lf)
	defer func() { c.loops = c.loops[:len(c.loops)-1] }()

	loopStart := len(c.chunk.Code)

	c.chunk.Emit(bytecode.OpDup)
	c.chunk.Emit(bytecode.OpDup)
	nextIdx := c.chunk.AddConstant(value.String("next"))
	c.chunk.EmitGetProp(nextIdx)
	c.chunk.EmitU8(bytecode.OpCallMethod, 0)

	c.chunk.Emit(bytecode.OpDup)
	doneIdx := c.chunk.AddConstant(value.String("done"))
	c.chunk.EmitGetProp(doneIdx)
	endJump := c.chunk.EmitJump(bytecode.OpJumpIfTrue)

	valueIdx := c.chunk.AddConstant(value.String("value"))
	c.chunk.EmitGetProp(valueIdx)
	// Stack: [iter, value]. Consume the value and bind it.
	if name != "" {
		if err := c.emitStore(ref, name); err != nil {
			return err
		}
	} else {
		if err := c.emitForBind(assignTo); err != nil {
			return err
		}
	}

	if err := c.emitStmt(body, false); err != nil {
		return err
	}

	// Back to loopStart for the next iteration.
	if err := c.chunk.EmitBackJump(bytecode.OpJump, loopStart); err != nil {
		return err
	}

	// Done branch arrives with [iter, result] on the stack; pop result.
	if err := c.chunk.PatchJump(endJump); err != nil {
		return err
	}
	c.chunk.Emit(bytecode.OpPop) // pop result

	// Break-target / iter-pop: both fall-through-from-done and an
	// explicit break arrive here with just [iter] to clean up.
	breakTarget := len(c.chunk.Code)
	for _, p := range lf.breakJumps {
		if err := c.chunk.PatchJumpTo(p, breakTarget); err != nil {
			return err
		}
	}
	c.chunk.Emit(bytecode.OpPop) // pop iter

	// Continue jumps back to loopStart (re-runs iter.next()).
	for _, p := range lf.continueJumps {
		if err := c.chunk.PatchJumpTo(p, loopStart); err != nil {
			return err
		}
	}
	return nil
}

// emitSwitch lays out the discriminant on the stack, emits each
// case's strict-equality probe with a JumpIfTrue placeholder, then
// emits all case bodies sequentially with fall-through. Break inside
// any body lands after the final OpPop that discards the discriminant.
func (c *compiler) emitSwitch(x *parser.SwitchStmt) error {
	if err := c.emit(x.Discriminant); err != nil {
		return err
	}
	// stackItems=1 — switch keeps the discriminant on the stack
	// across all case bodies (it's popped at the end), so a try
	// inside a case must preserve it.
	lf := &loopFrame{catchContinue: false, label: c.pendingLabel, stackItems: 1}; c.pendingLabel = ""
	c.loops = append(c.loops, lf)

	// Phase 1: probes. We dup the discriminant once per case (the
	// per-probe OpStrictEq pops both copies; a successful match's
	// OpJumpIfTrue pops the bool but the discriminant remains because
	// we DUPed it before evaluating the test).
	type probe struct {
		idx  int // case index
		jump int // placeholder
	}
	var probes []probe
	defaultIdx := -1
	for i, cs := range x.Cases {
		if cs.Test == nil {
			if defaultIdx >= 0 {
				c.loops = c.loops[:len(c.loops)-1]
				return fmt.Errorf("compiler: multiple default clauses in switch")
			}
			defaultIdx = i
			continue
		}
		c.chunk.Emit(bytecode.OpDup)
		if err := c.emit(cs.Test); err != nil {
			c.loops = c.loops[:len(c.loops)-1]
			return err
		}
		c.chunk.Emit(bytecode.OpStrictEq)
		probes = append(probes, probe{idx: i, jump: c.chunk.EmitJump(bytecode.OpJumpIfTrue)})
	}
	// Fallback: if no test matched, jump to default body (or past
	// all bodies to the Pop).
	fallbackJump := c.chunk.EmitJump(bytecode.OpJump)

	// Phase 2: emit bodies in declaration order, recording their
	// start PCs so we can wire up the probe targets.
	bodyStart := make([]int, len(x.Cases))
	for i, cs := range x.Cases {
		bodyStart[i] = len(c.chunk.Code)
		for _, s := range cs.Body {
			if err := c.emitStmt(s, false); err != nil {
				c.loops = c.loops[:len(c.loops)-1]
				return err
			}
		}
	}
	// Phase 3: patch probes.
	for _, pr := range probes {
		if err := c.chunk.PatchJumpTo(pr.jump, bodyStart[pr.idx]); err != nil {
			c.loops = c.loops[:len(c.loops)-1]
			return err
		}
	}
	// Phase 4: patch fallback.
	if defaultIdx >= 0 {
		if err := c.chunk.PatchJumpTo(fallbackJump, bodyStart[defaultIdx]); err != nil {
			c.loops = c.loops[:len(c.loops)-1]
			return err
		}
	} else {
		if err := c.chunk.PatchJump(fallbackJump); err != nil {
			c.loops = c.loops[:len(c.loops)-1]
			return err
		}
	}
	// Phase 5: patch breaks to here (BEFORE the OpPop) and then
	// emit the discriminant Pop. Both natural fall-through and
	// break arrive with the discriminant still on stack.
	breakTarget := len(c.chunk.Code)
	for _, p := range lf.breakJumps {
		if err := c.chunk.PatchJumpTo(p, breakTarget); err != nil {
			c.loops = c.loops[:len(c.loops)-1]
			return err
		}
	}
	c.chunk.Emit(bytecode.OpPop)
	c.loops = c.loops[:len(c.loops)-1]
	return nil
}
