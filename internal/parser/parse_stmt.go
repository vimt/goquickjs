package parser

import (
	"fmt"
)

func (p *parser) parseStatement() (Node, error) {
	switch p.peek().kind {
	case tkIdent:
		switch p.peek().text {
		case "let", "var", "const":
			// `var` is treated as `let` for purposes of binding. Real
			// JS hoists var to function scope and allows redeclaration;
			// nothing in our current corpus depends on either, and
			// any divergence the differ catches we'll address then.
			n, err := p.parseLetDeclNoSemi()
			if err != nil {
				return nil, err
			}
			p.consumeSemi()
			return n, nil
		case "for":
			return p.parseForStmt()
		case "if":
			return p.parseIfStmt()
		case "return":
			return p.parseReturnStmt()
		case "function":
			return p.parseFunctionDecl()
		case "class":
			return p.parseClassDecl()
		case "async":
			// `async function name(...) {}` — peek the next token to
			// distinguish from a plain ident named "async".
			if p.pos+1 < len(p.toks) && p.toks[p.pos+1].kind == tkIdent && p.toks[p.pos+1].text == "function" {
				p.advance() // 'async'
				n, err := p.parseFunctionDecl()
				if err != nil {
					return nil, err
				}
				n.(*FunctionDecl).IsAsync = true
				return n, nil
			}
		case "throw":
			return p.parseThrowStmt()
		case "try":
			return p.parseTryStmt()
		case "while":
			return p.parseWhileStmt()
		case "do":
			return p.parseDoWhileStmt()
		case "switch":
			return p.parseSwitchStmt()
		case "break":
			p.advance()
			label := ""
			if p.peek().kind == tkIdent && !isReserved(p.peek().text) {
				label = p.advance().text
			}
			p.consumeSemi()
			return &BreakStmt{Label: label}, nil
		case "continue":
			p.advance()
			label := ""
			if p.peek().kind == tkIdent && !isReserved(p.peek().text) {
				label = p.advance().text
			}
			p.consumeSemi()
			return &ContinueStmt{Label: label}, nil
		}
		// LabeledStmt: `name: stmt`. Detect by peeking past the ident
		// for a colon and ensuring the name isn't a known statement
		// keyword we already routed above.
		if p.pos+1 < len(p.toks) && p.toks[p.pos+1].kind == tkColon && !isReserved(p.peek().text) {
			label := p.advance().text
			p.advance() // ':'
			body, err := p.parseStatement()
			if err != nil {
				return nil, err
			}
			return &LabeledStmt{Label: label, Body: body}, nil
		}
	case tkLBrace:
		return p.parseBlock()
	}
	e, err := p.parseExpression()
	if err != nil {
		return nil, err
	}
	p.consumeSemi()
	return &ExprStmt{X: e}, nil
}

func (p *parser) parseIfStmt() (Node, error) {
	p.advance() // 'if'
	if _, err := p.expect(tkLParen, "'(' after 'if'"); err != nil {
		return nil, err
	}
	test, err := p.parseExpression()
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(tkRParen, "')'"); err != nil {
		return nil, err
	}
	cons, err := p.parseStatement()
	if err != nil {
		return nil, err
	}
	var alt Node
	if p.peek().kind == tkIdent && p.peek().text == "else" {
		p.advance()
		alt, err = p.parseStatement()
		if err != nil {
			return nil, err
		}
	}
	return &IfStmt{Test: test, Cons: cons, Alt: alt}, nil
}

func (p *parser) parseReturnStmt() (Node, error) {
	p.advance() // 'return'
	var arg Node
	// No ASI: stop only at ;, }, or EOF.
	if p.peek().kind != tkSemi && p.peek().kind != tkRBrace && p.peek().kind != tkEOF {
		e, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		arg = e
	}
	p.consumeSemi()
	return &ReturnStmt{Arg: arg}, nil
}

// parseClassDecl handles `class Name [extends Parent] { ... }`.
// Member syntax accepted: `name(params){ body }`, `static name(...){...}`,
// `constructor(...){...}`. Getters / setters / private fields / class
// fields are deliberately deferred — none of the current corpus uses
// them and the surface area is large.
func (p *parser) parseClassDecl() (Node, error) {
	p.advance() // 'class'
	if p.peek().kind != tkIdent {
		return nil, fmt.Errorf("parser: expected class name at offset %d", p.peek().pos)
	}
	name := p.advance().text
	var parent Node
	if p.peek().kind == tkIdent && p.peek().text == "extends" {
		p.advance()
		e, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		parent = e
	}
	if _, err := p.expect(tkLBrace, "'{' after class header"); err != nil {
		return nil, err
	}
	var members []ClassMember
	for p.peek().kind != tkRBrace && p.peek().kind != tkEOF {
		if p.peek().kind == tkSemi {
			p.advance()
			continue
		}
		m, err := p.parseClassMember()
		if err != nil {
			return nil, err
		}
		members = append(members, m)
	}
	if _, err := p.expect(tkRBrace, "'}' to end class"); err != nil {
		return nil, err
	}
	return &ClassDecl{Name: name, Parent: parent, Members: members}, nil
}

func (p *parser) parseClassMember() (ClassMember, error) {
	isStatic := false
	if p.peek().kind == tkIdent && p.peek().text == "static" {
		// Peek ahead: `static(...)` is a method *named* `static`.
		if p.pos+1 < len(p.toks) && p.toks[p.pos+1].kind == tkLParen {
			// fall through, treat `static` as the method name
		} else {
			p.advance()
			isStatic = true
		}
	}
	// Accessor: `get name() {}` / `set name(v) {}`. Detect by peeking
	// past the `get` / `set` ident — if the next token is ident or
	// string then it's a real accessor; otherwise the leading `get`
	// is the method's own name.
	kind := ""
	if p.peek().kind == tkIdent && (p.peek().text == "get" || p.peek().text == "set") {
		if p.pos+1 < len(p.toks) {
			nxt := p.toks[p.pos+1].kind
			if nxt == tkIdent || nxt == tkStr {
				kind = p.advance().text
			}
		}
	}
	if p.peek().kind != tkIdent && p.peek().kind != tkStr {
		return ClassMember{}, fmt.Errorf("parser: expected method name at offset %d", p.peek().pos)
	}
	name := p.advance().text
	params, body, err := p.parseFunctionTail()
	if err != nil {
		return ClassMember{}, err
	}
	return ClassMember{
		Name:          name,
		IsStatic:      isStatic,
		IsConstructor: !isStatic && name == "constructor" && kind == "",
		Kind:          kind,
		Params:        params,
		Body:          body,
	}, nil
}

func (p *parser) parseFunctionDecl() (Node, error) {
	p.advance() // 'function'
	isGenerator := false
	if p.peek().kind == tkStar {
		p.advance()
		isGenerator = true
	}
	if p.peek().kind != tkIdent {
		return nil, fmt.Errorf("parser: expected function name at offset %d", p.peek().pos)
	}
	name := p.advance().text
	params, body, err := p.parseFunctionTail()
	if err != nil {
		return nil, err
	}
	return &FunctionDecl{Name: name, Params: params, Body: body, IsGenerator: isGenerator}, nil
}

func (p *parser) parseFunctionExpr() (Node, error) {
	p.advance() // 'function'
	isGenerator := false
	if p.peek().kind == tkStar {
		p.advance()
		isGenerator = true
	}
	name := ""
	if p.peek().kind == tkIdent && p.peek().text != "" {
		name = p.advance().text
	}
	params, body, err := p.parseFunctionTail()
	if err != nil {
		return nil, err
	}
	return &FunctionExpr{Name: name, Params: params, Body: body, IsGenerator: isGenerator}, nil
}

func (p *parser) parseThrowStmt() (Node, error) {
	p.advance() // 'throw'
	e, err := p.parseExpression()
	if err != nil {
		return nil, err
	}
	p.consumeSemi()
	return &ThrowStmt{Arg: e}, nil
}

func (p *parser) parseTryStmt() (Node, error) {
	p.advance() // 'try'
	bodyNode, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	out := &TryStmt{Body: bodyNode.(*Block)}
	if p.peek().kind == tkIdent && p.peek().text == "catch" {
		p.advance() // 'catch'
		// Spec allows `catch { ... }` (no binding); accept both forms.
		if p.peek().kind == tkLParen {
			p.advance()
			if p.peek().kind != tkIdent {
				return nil, fmt.Errorf("parser: expected catch parameter at offset %d", p.peek().pos)
			}
			out.CatchParam = p.advance().text
			if _, err := p.expect(tkRParen, "')'"); err != nil {
				return nil, err
			}
		}
		catchNode, err := p.parseBlock()
		if err != nil {
			return nil, err
		}
		out.CatchBody = catchNode.(*Block)
	}
	if p.peek().kind == tkIdent && p.peek().text == "finally" {
		p.advance()
		finNode, err := p.parseBlock()
		if err != nil {
			return nil, err
		}
		out.FinallyBody = finNode.(*Block)
	}
	if out.CatchBody == nil && out.FinallyBody == nil {
		return nil, fmt.Errorf("parser: try without catch or finally at offset %d", p.peek().pos)
	}
	return out, nil
}

func (p *parser) parseDoWhileStmt() (Node, error) {
	p.advance() // 'do'
	body, err := p.parseStatement()
	if err != nil {
		return nil, err
	}
	if p.peek().kind != tkIdent || p.peek().text != "while" {
		return nil, fmt.Errorf("parser: expected 'while' after do-body at offset %d", p.peek().pos)
	}
	p.advance() // 'while'
	if _, err := p.expect(tkLParen, "'('"); err != nil {
		return nil, err
	}
	test, err := p.parseExpression()
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(tkRParen, "')'"); err != nil {
		return nil, err
	}
	p.consumeSemi()
	return &DoWhileStmt{Body: body, Test: test}, nil
}

func (p *parser) parseWhileStmt() (Node, error) {
	p.advance() // 'while'
	if _, err := p.expect(tkLParen, "'('"); err != nil {
		return nil, err
	}
	test, err := p.parseExpression()
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(tkRParen, "')'"); err != nil {
		return nil, err
	}
	body, err := p.parseStatement()
	if err != nil {
		return nil, err
	}
	return &WhileStmt{Test: test, Body: body}, nil
}

func (p *parser) parseSwitchStmt() (Node, error) {
	p.advance() // 'switch'
	if _, err := p.expect(tkLParen, "'('"); err != nil {
		return nil, err
	}
	disc, err := p.parseExpression()
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(tkRParen, "')'"); err != nil {
		return nil, err
	}
	if _, err := p.expect(tkLBrace, "'{'"); err != nil {
		return nil, err
	}
	var cases []SwitchCase
	for p.peek().kind != tkRBrace && p.peek().kind != tkEOF {
		if p.peek().kind != tkIdent {
			return nil, fmt.Errorf("parser: expected 'case' or 'default' at offset %d", p.peek().pos)
		}
		kw := p.peek().text
		switch kw {
		case "case":
			p.advance()
			test, err := p.parseExpression()
			if err != nil {
				return nil, err
			}
			if _, err := p.expect(tkColon, "':'"); err != nil {
				return nil, err
			}
			body, err := p.parseCaseBody()
			if err != nil {
				return nil, err
			}
			cases = append(cases, SwitchCase{Test: test, Body: body})
		case "default":
			p.advance()
			if _, err := p.expect(tkColon, "':'"); err != nil {
				return nil, err
			}
			body, err := p.parseCaseBody()
			if err != nil {
				return nil, err
			}
			cases = append(cases, SwitchCase{Test: nil, Body: body})
		default:
			return nil, fmt.Errorf("parser: expected 'case' or 'default' at offset %d", p.peek().pos)
		}
	}
	if _, err := p.expect(tkRBrace, "'}'"); err != nil {
		return nil, err
	}
	return &SwitchStmt{Discriminant: disc, Cases: cases}, nil
}

// parseCaseBody reads statements until the next 'case', 'default', or
// the enclosing '}'. The trailing '}' is left for the caller.
func (p *parser) parseCaseBody() ([]Node, error) {
	var body []Node
	for {
		if p.peek().kind == tkRBrace || p.peek().kind == tkEOF {
			return body, nil
		}
		if p.peek().kind == tkIdent && (p.peek().text == "case" || p.peek().text == "default") {
			return body, nil
		}
		s, err := p.parseStatement()
		if err != nil {
			return nil, err
		}
		body = append(body, s)
	}
}

// isReserved reports whether the identifier is a JS reserved word
// (something the parser routes specially elsewhere). Used to keep
// labeled-statement detection from swallowing `let:` or `if:`.
func isReserved(name string) bool {
	switch name {
	case "let", "var", "const", "for", "if", "else", "return",
		"function", "throw", "try", "catch", "finally", "while",
		"do", "switch", "case", "default", "break", "continue",
		"new", "instanceof", "in", "of", "typeof", "void", "delete",
		"true", "false", "null", "undefined", "this", "class",
		"extends", "static", "super", "async", "await", "yield",
		"import", "export":
		return true
	}
	return false
}

func (p *parser) parseFunctionTail() ([]Param, *Block, error) {
	if _, err := p.expect(tkLParen, "'(' after 'function'"); err != nil {
		return nil, nil, err
	}
	params, err := p.parseParamList()
	if err != nil {
		return nil, nil, err
	}
	if _, err := p.expect(tkRParen, "')'"); err != nil {
		return nil, nil, err
	}
	bodyNode, err := p.parseBlock()
	if err != nil {
		return nil, nil, err
	}
	return params, bodyNode.(*Block), nil
}

// parseParamList reads a comma-separated parameter list up to (but
// not including) the closing ')'. Supports simple identifiers,
// `name = default`, `...rest`, and `{a, b}` / `[a, b]` patterns.
func (p *parser) parseParamList() ([]Param, error) {
	var params []Param
	for p.peek().kind != tkRParen && p.peek().kind != tkEOF {
		if len(params) > 0 {
			if p.peek().kind != tkComma {
				return nil, fmt.Errorf("parser: expected ',' or ')' in params at offset %d", p.peek().pos)
			}
			p.advance()
		}
		// Rest param.
		if p.peek().kind == tkEllipsis {
			p.advance()
			if p.peek().kind != tkIdent {
				return nil, fmt.Errorf("parser: expected identifier after '...' at offset %d", p.peek().pos)
			}
			params = append(params, Param{Name: p.advance().text, Rest: true})
			if p.peek().kind == tkComma {
				return nil, fmt.Errorf("parser: rest param must be last at offset %d", p.peek().pos)
			}
			continue
		}
		// Destructuring pattern param.
		if p.peek().kind == tkLBrace || p.peek().kind == tkLBracket {
			pat, err := p.parsePattern()
			if err != nil {
				return nil, err
			}
			pm := Param{Pattern: pat}
			if p.peek().kind == tkAssign {
				p.advance()
				d, err := p.parseAssignment()
				if err != nil {
					return nil, err
				}
				pm.Default = d
			}
			params = append(params, pm)
			continue
		}
		if p.peek().kind != tkIdent {
			return nil, fmt.Errorf("parser: expected parameter name at offset %d", p.peek().pos)
		}
		name := p.advance().text
		pm := Param{Name: name}
		if p.peek().kind == tkAssign {
			p.advance()
			d, err := p.parseAssignment()
			if err != nil {
				return nil, err
			}
			pm.Default = d
		}
		params = append(params, pm)
	}
	return params, nil
}

func (p *parser) parseBlock() (Node, error) {
	if _, err := p.expect(tkLBrace, "'{'"); err != nil {
		return nil, err
	}
	var body []Node
	for p.peek().kind != tkRBrace && p.peek().kind != tkEOF {
		s, err := p.parseStatement()
		if err != nil {
			return nil, err
		}
		body = append(body, s)
	}
	if _, err := p.expect(tkRBrace, "'}'"); err != nil {
		return nil, err
	}
	return &Block{Body: body}, nil
}

func (p *parser) parseLetDeclNoSemi() (Node, error) {
	p.advance() // 'let' / 'var'
	var decls []Node
	for {
		// Destructuring: `let { ... } = src` or `let [ ... ] = src`.
		if p.peek().kind == tkLBrace || p.peek().kind == tkLBracket {
			pat, err := p.parsePattern()
			if err != nil {
				return nil, err
			}
			if _, err := p.expect(tkAssign, "'=' after destructuring pattern"); err != nil {
				return nil, err
			}
			init, err := p.parseAssignment()
			if err != nil {
				return nil, err
			}
			decls = append(decls, &DestructureDecl{Pattern: pat, Init: init})
			if p.peek().kind != tkComma {
				break
			}
			p.advance() // ','
			continue
		}
		if p.peek().kind != tkIdent {
			return nil, fmt.Errorf("parser: expected identifier in declaration at offset %d", p.peek().pos)
		}
		name := p.advance().text
		var init Node
		if p.peek().kind == tkAssign {
			p.advance()
			e, err := p.parseAssignment()
			if err != nil {
				return nil, err
			}
			init = e
		}
		decls = append(decls, &LetDecl{Name: name, Init: init})
		if p.peek().kind != tkComma {
			break
		}
		p.advance() // ','
	}
	if len(decls) == 1 {
		return decls[0], nil
	}
	return &MultiVarDecl{Decls: decls}, nil
}

// parsePattern is the dispatch point for the destructuring grammar.
// Caller has already seen `{` or `[` as the next token (we peek, not
// expect, so this can also be called recursively from nested forms).

func (p *parser) parseForStmt() (Node, error) {
	p.advance() // 'for'
	if _, err := p.expect(tkLParen, "'('"); err != nil {
		return nil, err
	}

	var init Node
	if p.peek().kind != tkSemi {
		if p.peek().kind == tkIdent && (p.peek().text == "let" || p.peek().text == "var") {
			n, err := p.parseLetDeclNoSemi()
			if err != nil {
				return nil, err
			}
			// for-of: `for (let x of iterable) body`. We detect it
			// after the binding because `of` isn't a keyword and
			// only matters in this position.
			if p.peek().kind == tkIdent && p.peek().text == "of" {
				ld, ok := n.(*LetDecl)
				if !ok || ld.Init != nil {
					return nil, fmt.Errorf("parser: for-of init must be a single binding without initializer at offset %d", p.peek().pos)
				}
				p.advance() // 'of'
				iterable, err := p.parseExpression()
				if err != nil {
					return nil, err
				}
				if _, err := p.expect(tkRParen, "')'"); err != nil {
					return nil, err
				}
				body, err := p.parseStatement()
				if err != nil {
					return nil, err
				}
				return &ForOfStmt{Name: ld.Name, Iterable: iterable, Body: body}, nil
			}
			// for-in: `for (let k in obj) body`. Same shape as for-of
			// but yields keys rather than values.
			if p.peek().kind == tkIdent && p.peek().text == "in" {
				ld, ok := n.(*LetDecl)
				if !ok || ld.Init != nil {
					return nil, fmt.Errorf("parser: for-in init must be a single binding without initializer at offset %d", p.peek().pos)
				}
				p.advance() // 'in'
				obj, err := p.parseExpression()
				if err != nil {
					return nil, err
				}
				if _, err := p.expect(tkRParen, "')'"); err != nil {
					return nil, err
				}
				body, err := p.parseStatement()
				if err != nil {
					return nil, err
				}
				return &ForInStmt{Name: ld.Name, Obj: obj, Body: body}, nil
			}
			init = n
		} else {
			e, err := p.parseExpression()
			if err != nil {
				return nil, err
			}
			init = &ExprStmt{X: e}
		}
	}
	if _, err := p.expect(tkSemi, "';' after for-init"); err != nil {
		return nil, err
	}

	var test Node
	if p.peek().kind != tkSemi {
		e, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		test = e
	}
	if _, err := p.expect(tkSemi, "';' after for-test"); err != nil {
		return nil, err
	}

	var update Node
	if p.peek().kind != tkRParen {
		e, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		update = e
	}
	if _, err := p.expect(tkRParen, "')'"); err != nil {
		return nil, err
	}

	body, err := p.parseStatement()
	if err != nil {
		return nil, err
	}
	return &ForStmt{Init: init, Test: test, Update: update, Body: body}, nil
}

// Expressions. Precedence (low → high):
//
//	expression → assignment
//	assignment → logicalOr (('=' | '+=' | '-=' | '*=' | '/=') assignment)?   right-assoc
//	logicalOr  → logicalAnd ('||' logicalAnd)*
//	logicalAnd → equality   ('&&' equality)*
//	equality   → relational (('==' | '!=' | '===' | '!==') relational)*
//	relational → additive   (('<' | '<=' | '>' | '>=') additive)*
//	additive   → mult       (('+' | '-') mult)*
//	mult       → unary      (('*' | '/') unary)*
//	unary      → ('!' | '-' | '+' | '++' | '--') unary | postfix
//	postfix    → primary ('++' | '--')?
//	primary    → NUMBER | IDENT | keyword-literal | '(' expression ')'

