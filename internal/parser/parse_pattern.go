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
