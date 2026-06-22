package parser

import "fmt"

func (p *parser) parsePattern() (Pattern, error) {
	switch p.peek().kind {
	case tkLBrace:
		return p.parseObjectPattern()
	case tkLBracket:
		return p.parseArrayPattern()
	}
	return nil, fmt.Errorf("parser: expected pattern at offset %d", p.peek().pos)
}

func (p *parser) parseObjectPattern() (Pattern, error) {
	p.advance() // '{'
	var props []ObjectPatternProp
	for p.peek().kind != tkRBrace && p.peek().kind != tkEOF {
		if len(props) > 0 {
			if _, err := p.expect(tkComma, "','"); err != nil {
				return nil, err
			}
			if p.peek().kind == tkRBrace {
				break
			}
		}
		if p.peek().kind == tkEllipsis {
			p.advance()
			if p.peek().kind != tkIdent {
				return nil, fmt.Errorf("parser: expected identifier after '...' at offset %d", p.peek().pos)
			}
			name := p.advance().text
			props = append(props, ObjectPatternProp{
				Key: name, Target: &IdentTarget{Name: name}, IsRest: true,
			})
			break
		}
		if p.peek().kind != tkIdent && p.peek().kind != tkStr {
			return nil, fmt.Errorf("parser: expected property name at offset %d", p.peek().pos)
		}
		key := p.advance().text
		var target PatternTarget = &IdentTarget{Name: key}
		if p.peek().kind == tkColon {
			p.advance()
			if p.peek().kind == tkLBrace || p.peek().kind == tkLBracket {
				inner, err := p.parsePattern()
				if err != nil {
					return nil, err
				}
				target = &NestedTarget{Pattern: inner}
			} else if p.peek().kind == tkIdent {
				target = &IdentTarget{Name: p.advance().text}
			} else {
				return nil, fmt.Errorf("parser: expected binding name or pattern at offset %d", p.peek().pos)
			}
		}
		var def Node
		if p.peek().kind == tkAssign {
			p.advance()
			d, err := p.parseAssignment()
			if err != nil {
				return nil, err
			}
			def = d
		}
		props = append(props, ObjectPatternProp{
			Key: key, Target: target, Default: def,
		})
	}
	if _, err := p.expect(tkRBrace, "'}'"); err != nil {
		return nil, err
	}
	return &ObjectPattern{Props: props}, nil
}

func (p *parser) parseArrayPattern() (Pattern, error) {
	p.advance() // '['
	var elems []ArrayPatternElem
	for p.peek().kind != tkRBracket && p.peek().kind != tkEOF {
		// Hole: `[a, , b]` — comma directly here means skip slot.
		if p.peek().kind == tkComma {
			p.advance()
			elems = append(elems, ArrayPatternElem{Skip: true})
			continue
		}
		if p.peek().kind == tkEllipsis {
			p.advance()
			if p.peek().kind != tkIdent {
				return nil, fmt.Errorf("parser: expected identifier after '...' at offset %d", p.peek().pos)
			}
			name := p.advance().text
			elems = append(elems, ArrayPatternElem{
				Target: &IdentTarget{Name: name}, IsRest: true,
			})
			break
		}
		var target PatternTarget
		if p.peek().kind == tkLBrace || p.peek().kind == tkLBracket {
			inner, err := p.parsePattern()
			if err != nil {
				return nil, err
			}
			target = &NestedTarget{Pattern: inner}
		} else if p.peek().kind == tkIdent {
			target = &IdentTarget{Name: p.advance().text}
		} else {
			return nil, fmt.Errorf("parser: expected binding or pattern at offset %d", p.peek().pos)
		}
		var def Node
		if p.peek().kind == tkAssign {
			p.advance()
			d, err := p.parseAssignment()
			if err != nil {
				return nil, err
			}
			def = d
		}
		elems = append(elems, ArrayPatternElem{Target: target, Default: def})
		if p.peek().kind == tkComma {
			p.advance()
		}
	}
	if _, err := p.expect(tkRBracket, "']'"); err != nil {
		return nil, err
	}
	return &ArrayPattern{Elements: elems}, nil
}

// coerceExprToPattern reinterprets an expression as a binding pattern
// to support destructuring assignment (`{a, b} = obj`, `[x, y] = arr`):
// the LHS first parses as ObjectLit / ArrayLit and only the trailing
// `=` reveals its real role.
//
// Only the binding subset is accepted — Ident leaves and recursively
// nested Object/Array literals. MemberExpr / IndexExpr leaves
// (`({a: o.x} = ...)`) need a richer assignment-target model and are
// out of scope for now; almost all corpus uses keep simple identifier
// leaves. Defaults inside the literal form (`{a = 1} = ...`) likewise
// fall out because the literal parser doesn't produce that shape.
func coerceExprToPattern(n Node) (Pattern, error) {
	switch e := n.(type) {
	case *ObjectLit:
		var props []ObjectPatternProp
		for _, prop := range e.Props {
			if prop.Spread {
				name, ok := prop.Value.(*Ident)
				if !ok {
					return nil, fmt.Errorf("parser: rest element must be a binding identifier")
				}
				props = append(props, ObjectPatternProp{
					Target: &IdentTarget{Name: name.Name},
					IsRest: true,
				})
				continue
			}
			if prop.Computed || prop.Kind != "" {
				return nil, fmt.Errorf("parser: computed/accessor keys not supported in destructuring assignment")
			}
			t, err := coerceExprToPatternTarget(prop.Value)
			if err != nil {
				return nil, err
			}
			props = append(props, ObjectPatternProp{Key: prop.Key, Target: t})
		}
		return &ObjectPattern{Props: props}, nil
	case *ArrayLit:
		var elems []ArrayPatternElem
		for _, item := range e.Items {
			if item == nil {
				elems = append(elems, ArrayPatternElem{Skip: true})
				continue
			}
			if r, ok := item.(*SpreadElement); ok {
				// In LHS-pattern position spread acts as rest. Only
				// plain identifier targets are supported (no nested
				// patterns inside the rest yet).
				ident, ok := r.Arg.(*Ident)
				if !ok {
					return nil, fmt.Errorf("parser: rest element must be a binding identifier")
				}
				elems = append(elems, ArrayPatternElem{
					Target: &IdentTarget{Name: ident.Name},
					IsRest: true,
				})
				continue
			}
			t, err := coerceExprToPatternTarget(item)
			if err != nil {
				return nil, err
			}
			elems = append(elems, ArrayPatternElem{Target: t})
		}
		return &ArrayPattern{Elements: elems}, nil
	}
	return nil, fmt.Errorf("parser: not a destructuring pattern: %T", n)
}

func coerceExprToPatternTarget(n Node) (PatternTarget, error) {
	switch e := n.(type) {
	case *Ident:
		return &IdentTarget{Name: e.Name}, nil
	case *ObjectLit, *ArrayLit:
		nested, err := coerceExprToPattern(e)
		if err != nil {
			return nil, err
		}
		return &NestedTarget{Pattern: nested}, nil
	}
	return nil, fmt.Errorf("parser: destructuring leaf must be an identifier")
}
