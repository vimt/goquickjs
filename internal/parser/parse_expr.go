package parser

import (
	"fmt"
	"strconv"

	"github.com/vimt/goquickjs/internal/jserrors"
)

func (p *parser) parseExpression() (Node, error) {
	return p.parseAssignment()
}

func (p *parser) parseAssignment() (Node, error) {
	// Arrow function detection (before logicalOr because `=>` binds
	// looser than `=`).
	// Single-identifier arrow: `x => body`.
	if p.peek().kind == tkIdent && p.pos+1 < len(p.toks) && p.toks[p.pos+1].kind == tkArrow {
		t := p.advance()
		p.advance() // =>
		return p.parseArrowBody([]Param{{Name: t.text}})
	}
	// Parenthesised arrow: `(params) => body`. Detect by scanning
	// past the matched `)` to look for `=>` after it.
	if p.peek().kind == tkLParen && p.isArrowLookahead() {
		return p.parseParenArrowFunction()
	}
	left, err := p.parseConditional()
	if err != nil {
		return nil, err
	}
	var op string
	switch p.peek().kind {
	case tkAssign:
		op = "="
	case tkPlusAssign:
		op = "+="
	case tkMinusAssign:
		op = "-="
	case tkStarAssign:
		op = "*="
	case tkSlashAssign:
		op = "/="
	case tkPercentAssign:
		op = "%="
	case tkPowAssign:
		op = "**="
	case tkLandAssign:
		op = "&&="
	case tkLorAssign:
		op = "||="
	case tkNullishAsgn:
		op = "??="
	case tkBitAndAsgn:
		op = "&="
	case tkBitOrAsgn:
		op = "|="
	case tkBitXorAsgn:
		op = "^="
	case tkShlAssign:
		op = "<<="
	case tkShrAssign:
		op = ">>="
	case tkUShrAssign:
		op = ">>>="
	default:
		return left, nil
	}
	p.advance()
	switch left.(type) {
	case *Ident:
		// OK for any op.
	case *MemberExpr, *IndexExpr:
		// Both plain `=` and compound (`+=`, `*=`, `||=`, ...) are
		// emitted by the compiler — it dups the receiver, loads the
		// current value, applies the op, then stores back.
	case *ObjectLit, *ArrayLit:
		// Destructuring assignment: `{a, b} = obj` or `[x, y] = arr`.
		// Only plain `=` is meaningful — compound ops on a pattern
		// have no spec semantics. Convert the literal to a Pattern.
		if op != "=" {
			return nil, fmt.Errorf("parser: compound op on destructuring pattern is not valid")
		}
		pat, err := coerceExprToPattern(left)
		if err != nil {
			return nil, err
		}
		rhs, err := p.parseAssignment()
		if err != nil {
			return nil, err
		}
		return &AssignExpr{Op: op, Target: pat, Value: rhs}, nil
	default:
		return nil, fmt.Errorf("parser: invalid assignment target: %w", jserrors.ErrNotImplemented)
	}
	rhs, err := p.parseAssignment() // right-associative
	if err != nil {
		return nil, err
	}
	return &AssignExpr{Op: op, Target: left, Value: rhs}, nil
}

// parseConditional handles the ternary `a ? b : c`. cons/alt are
// AssignmentExpressions so chains like `a ? b = 1 : 0` parse the way
// users expect.
func (p *parser) parseConditional() (Node, error) {
	test, err := p.parseLogicalOr()
	if err != nil {
		return nil, err
	}
	if p.peek().kind != tkQuestion {
		return test, nil
	}
	p.advance()
	cons, err := p.parseAssignment()
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(tkColon, "':' in ternary"); err != nil {
		return nil, err
	}
	alt, err := p.parseAssignment()
	if err != nil {
		return nil, err
	}
	return &ConditionalExpr{Test: test, Cons: cons, Alt: alt}, nil
}

// parseLogicalOr accepts both `||` and `??` at the same precedence.
// JS spec actually forbids mixing them without parens; we don't
// enforce that yet.
func (p *parser) parseLogicalOr() (Node, error) {
	left, err := p.parseLogicalAnd()
	if err != nil {
		return nil, err
	}
	for {
		var op string
		switch p.peek().kind {
		case tkLor:
			op = "||"
		case tkNullish:
			op = "??"
		default:
			return left, nil
		}
		p.advance()
		right, err := p.parseLogicalAnd()
		if err != nil {
			return nil, err
		}
		left = &LogicalExpr{Op: op, L: left, R: right}
	}
}

func (p *parser) parseLogicalAnd() (Node, error) {
	left, err := p.parseBitOr()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == tkLand {
		p.advance()
		right, err := p.parseBitOr()
		if err != nil {
			return nil, err
		}
		left = &LogicalExpr{Op: "&&", L: left, R: right}
	}
	return left, nil
}

func (p *parser) parseBitOr() (Node, error) {
	left, err := p.parseBitXor()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == tkBitOr {
		p.advance()
		right, err := p.parseBitXor()
		if err != nil {
			return nil, err
		}
		left = &BinaryExpr{Op: "|", L: left, R: right}
	}
	return left, nil
}

func (p *parser) parseBitXor() (Node, error) {
	left, err := p.parseBitAnd()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == tkBitXor {
		p.advance()
		right, err := p.parseBitAnd()
		if err != nil {
			return nil, err
		}
		left = &BinaryExpr{Op: "^", L: left, R: right}
	}
	return left, nil
}

func (p *parser) parseBitAnd() (Node, error) {
	left, err := p.parseEquality()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == tkBitAnd {
		p.advance()
		right, err := p.parseEquality()
		if err != nil {
			return nil, err
		}
		left = &BinaryExpr{Op: "&", L: left, R: right}
	}
	return left, nil
}

func (p *parser) parseEquality() (Node, error) {
	left, err := p.parseRelational()
	if err != nil {
		return nil, err
	}
	for {
		var op string
		switch p.peek().kind {
		case tkEq:
			op = "=="
		case tkNeq:
			op = "!="
		case tkStrictEq:
			op = "==="
		case tkStrictNeq:
			op = "!=="
		default:
			return left, nil
		}
		p.advance()
		right, err := p.parseRelational()
		if err != nil {
			return nil, err
		}
		left = &BinaryExpr{Op: op, L: left, R: right}
	}
}

func (p *parser) parseRelational() (Node, error) {
	left, err := p.parseShift()
	if err != nil {
		return nil, err
	}
	for {
		var op string
		switch p.peek().kind {
		case tkLt:
			op = "<"
		case tkLe:
			op = "<="
		case tkGt:
			op = ">"
		case tkGe:
			op = ">="
		default:
			// `instanceof` lives at relational precedence in the JS
			// grammar; recognise it by name since we never tokenised
			// it as its own kind.
			if p.peek().kind == tkIdent && p.peek().text == "instanceof" {
				op = "instanceof"
			} else if p.peek().kind == tkIdent && p.peek().text == "in" {
				op = "in"
			} else {
				return left, nil
			}
		}
		p.advance()
		right, err := p.parseShift()
		if err != nil {
			return nil, err
		}
		left = &BinaryExpr{Op: op, L: left, R: right}
	}
}

// parseShift handles `<<`, `>>`, `>>>` — between additive and relational.
func (p *parser) parseShift() (Node, error) {
	left, err := p.parseAdditive()
	if err != nil {
		return nil, err
	}
	for {
		var op string
		switch p.peek().kind {
		case tkShl:
			op = "<<"
		case tkShr:
			op = ">>"
		case tkUShr:
			op = ">>>"
		default:
			return left, nil
		}
		p.advance()
		right, err := p.parseAdditive()
		if err != nil {
			return nil, err
		}
		left = &BinaryExpr{Op: op, L: left, R: right}
	}
}

func (p *parser) parseAdditive() (Node, error) {
	left, err := p.parseMultiplicative()
	if err != nil {
		return nil, err
	}
	for {
		k := p.peek().kind
		if k != tkPlus && k != tkMinus {
			return left, nil
		}
		op := "+"
		if k == tkMinus {
			op = "-"
		}
		p.advance()
		right, err := p.parseMultiplicative()
		if err != nil {
			return nil, err
		}
		left = &BinaryExpr{Op: op, L: left, R: right}
	}
}

func (p *parser) parseMultiplicative() (Node, error) {
	left, err := p.parseExponentiation()
	if err != nil {
		return nil, err
	}
	for {
		k := p.peek().kind
		if k != tkStar && k != tkSlash && k != tkPercent {
			return left, nil
		}
		var op string
		switch k {
		case tkStar:
			op = "*"
		case tkSlash:
			op = "/"
		case tkPercent:
			op = "%"
		}
		p.advance()
		right, err := p.parseExponentiation()
		if err != nil {
			return nil, err
		}
		left = &BinaryExpr{Op: op, L: left, R: right}
	}
}

// parseExponentiation handles `**`. It's right-associative — `a ** b ** c`
// parses as `a ** (b ** c)`.
func (p *parser) parseExponentiation() (Node, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	if p.peek().kind != tkPow {
		return left, nil
	}
	p.advance()
	right, err := p.parseExponentiation()
	if err != nil {
		return nil, err
	}
	return &BinaryExpr{Op: "**", L: left, R: right}, nil
}

func (p *parser) parseUnary() (Node, error) {
	// `new Callee(args)` — bound tighter than logical/equality so it
	// sits with the rest of the prefix forms here. The new expression
	// itself can be followed by postfix accesses (`.foo`, `[i]`,
	// `()`), so we feed it into continuePostfix.
	if p.peek().kind == tkIdent && p.peek().text == "new" {
		n, err := p.parseNewExpr()
		if err != nil {
			return nil, err
		}
		return p.continuePostfix(n)
	}
	// typeof / void / delete: keyword-style unary ops. We dispatch
	// here so `typeof undefinedVar` works (it reads through the
	// global-by-name slot, returning "undefined" without throwing).
	if p.peek().kind == tkIdent {
		switch p.peek().text {
		case "typeof":
			p.advance()
			x, err := p.parseUnary()
			if err != nil {
				return nil, err
			}
			return &UnaryExpr{Op: "typeof", X: x}, nil
		case "void":
			p.advance()
			x, err := p.parseUnary()
			if err != nil {
				return nil, err
			}
			return &UnaryExpr{Op: "void", X: x}, nil
		case "await":
			p.advance()
			x, err := p.parseUnary()
			if err != nil {
				return nil, err
			}
			return &AwaitExpr{X: x}, nil
		case "yield":
			p.advance()
			// `yield` and `yield expr`. We greedily detect when the
			// next token can start an expression; if not, the yield
			// produces undefined.
			if canStartExpr(p.peek().kind) {
				x, err := p.parseAssignment()
				if err != nil {
					return nil, err
				}
				return &YieldExpr{X: x}, nil
			}
			return &YieldExpr{}, nil
		case "delete":
			p.advance()
			x, err := p.parseUnary()
			if err != nil {
				return nil, err
			}
			return &UnaryExpr{Op: "delete", X: x}, nil
		}
	}
	switch p.peek().kind {
	case tkMinus:
		p.advance()
		x, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &UnaryExpr{Op: "-", X: x}, nil
	case tkPlus:
		p.advance()
		return p.parseUnary()
	case tkBang:
		p.advance()
		x, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &UnaryExpr{Op: "!", X: x}, nil
	case tkBitNot:
		p.advance()
		x, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &UnaryExpr{Op: "~", X: x}, nil
	case tkInc, tkDec:
		op := "++"
		if p.peek().kind == tkDec {
			op = "--"
		}
		p.advance()
		x, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		ident, ok := x.(*Ident)
		if !ok {
			return nil, fmt.Errorf("parser: prefix %s requires identifier: %w", op, jserrors.ErrNotImplemented)
		}
		return &UpdateExpr{Op: op, Target: ident, Prefix: true}, nil
	}
	return p.parsePostfix()
}

// parseNewExpr handles `new Callee(args)` and `new Callee`. The
// callee is parsed up to but not including the optional arg list so
// member access chains like `new mod.X(...)` work.
func (p *parser) parseNewExpr() (Node, error) {
	p.advance() // 'new'
	callee, err := p.parseNewCallee()
	if err != nil {
		return nil, err
	}
	var args []Node
	if p.peek().kind == tkLParen {
		p.advance()
		for p.peek().kind != tkRParen && p.peek().kind != tkEOF {
			if len(args) > 0 {
				if _, err := p.expect(tkComma, "','"); err != nil {
					return nil, err
				}
			}
			if p.peek().kind == tkEllipsis {
				p.advance()
				a, err := p.parseAssignment()
				if err != nil {
					return nil, err
				}
				args = append(args, &SpreadElement{Arg: a})
				continue
			}
			a, err := p.parseAssignment()
			if err != nil {
				return nil, err
			}
			args = append(args, a)
		}
		if _, err := p.expect(tkRParen, "')'"); err != nil {
			return nil, err
		}
	}
	return &NewExpr{Callee: callee, Args: args}, nil
}

// parseNewCallee parses an expression that can serve as `new`'s
// constructor target: a primary plus any chained member/index
// accesses, but NOT a trailing call (the args belong to `new`).
func (p *parser) parseNewCallee() (Node, error) {
	x, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}
	for {
		switch p.peek().kind {
		case tkDot:
			p.advance()
			if p.peek().kind != tkIdent {
				return nil, fmt.Errorf("parser: expected identifier after '.' at offset %d", p.peek().pos)
			}
			name := p.advance().text
			x = &MemberExpr{Obj: x, Prop: name}
		case tkLBracket:
			p.advance()
			idx, err := p.parseExpression()
			if err != nil {
				return nil, err
			}
			if _, err := p.expect(tkRBracket, "']'"); err != nil {
				return nil, err
			}
			x = &IndexExpr{Obj: x, Index: idx}
		default:
			return x, nil
		}
	}
}

func (p *parser) parsePostfix() (Node, error) {
	x, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}
	return p.continuePostfix(x)
}

// continuePostfix runs the postfix loop on a seed expression. Used by
// parseNewExpr so `new Foo().bar(x).baz` can chain member access /
// calls / indexing on top of the constructed object.
func (p *parser) continuePostfix(x Node) (Node, error) {
	for {
		switch p.peek().kind {
		case tkDot:
			p.advance()
			if p.peek().kind != tkIdent {
				return nil, fmt.Errorf("parser: expected identifier after '.' at offset %d", p.peek().pos)
			}
			name := p.advance().text
			x = &MemberExpr{Obj: x, Prop: name}
		case tkQDot:
			p.advance()
			if p.peek().kind != tkIdent {
				return nil, fmt.Errorf("parser: expected identifier after '?.' at offset %d", p.peek().pos)
			}
			name := p.advance().text
			x = &OptionalMemberExpr{Obj: x, Prop: name}
		case tkLBracket:
			p.advance()
			idx, err := p.parseExpression()
			if err != nil {
				return nil, err
			}
			if _, err := p.expect(tkRBracket, "']'"); err != nil {
				return nil, err
			}
			x = &IndexExpr{Obj: x, Index: idx}
		case tkLParen:
			// Call expression: postfix `(args)`.
			p.advance()
			var args []Node
			for p.peek().kind != tkRParen && p.peek().kind != tkEOF {
				if len(args) > 0 {
					if p.peek().kind != tkComma {
						return nil, fmt.Errorf("parser: expected ',' or ')' in call args at offset %d", p.peek().pos)
					}
					p.advance()
				}
				if p.peek().kind == tkEllipsis {
					p.advance()
					a, err := p.parseAssignment()
					if err != nil {
						return nil, err
					}
					args = append(args, &SpreadElement{Arg: a})
					continue
				}
				a, err := p.parseAssignment()
				if err != nil {
					return nil, err
				}
				args = append(args, a)
			}
			if _, err := p.expect(tkRParen, "')'"); err != nil {
				return nil, err
			}
			x = &CallExpr{Callee: x, Args: args}
		case tkInc, tkDec:
			op := "++"
			if p.peek().kind == tkDec {
				op = "--"
			}
			p.advance()
			ident, ok := x.(*Ident)
			if !ok {
				return nil, fmt.Errorf("parser: postfix %s requires identifier: %w", op, jserrors.ErrNotImplemented)
			}
			return &UpdateExpr{Op: op, Target: ident, Prefix: false}, nil
		case tkTemplate:
			// Tagged template: `tag`…${…}…``. Lower to a call where
			// the first arg is the array of literal chunks and the
			// remaining args are the interpolated expressions.
			t := p.advance()
			exprs := make([]Node, len(t.exprSrcs))
			for i, src := range t.exprSrcs {
				e, err := parseExpressionString(src)
				if err != nil {
					return nil, fmt.Errorf("parser: tagged template expr at offset %d: %w", t.pos, err)
				}
				exprs[i] = e
			}
			strArr := make([]Node, len(t.quasis))
			for i, q := range t.quasis {
				strArr[i] = &StringLit{Value: q}
			}
			args := append([]Node{&ArrayLit{Items: strArr}}, exprs...)
			x = &CallExpr{Callee: x, Args: args}
		default:
			return x, nil
		}
	}
}

func (p *parser) parsePrimary() (Node, error) {
	// Object literal in expression position. (Statement-position '{'
	// is consumed by parseStatement as a Block; we only get here in
	// expression context like `let p = {x:1}` or `({})`.)
	if p.peek().kind == tkLBrace {
		return p.parseObjectLit()
	}
	// Array literal.
	if p.peek().kind == tkLBracket {
		return p.parseArrayLit()
	}
	// Function expression in expression position.
	if p.peek().kind == tkIdent && p.peek().text == "function" {
		return p.parseFunctionExpr()
	}
	t := p.advance()
	switch t.kind {
	case tkNum:
		return &NumberLit{Value: t.num}, nil
	case tkBigInt:
		base, _ := strconv.Atoi(t.regexFlags)
		if base == 0 {
			base = 10
		}
		return &BigIntLit{Digits: t.text, Base: base}, nil
	case tkStr:
		return &StringLit{Value: t.text}, nil
	case tkRegex:
		return &RegexLit{Pattern: t.text, Flags: t.regexFlags}, nil
	case tkTemplate:
		exprs := make([]Node, len(t.exprSrcs))
		for i, src := range t.exprSrcs {
			// Each `${...}` body is itself an expression — recursively
			// parse using a fresh sub-parser bounded by the source span.
			n, err := parseExpressionString(src)
			if err != nil {
				return nil, fmt.Errorf("parser: template expr at offset %d: %w", t.pos, err)
			}
			exprs[i] = n
		}
		return &TemplateLit{Quasis: t.quasis, Exprs: exprs}, nil
	case tkLParen:
		e, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(tkRParen, "')'"); err != nil {
			return nil, err
		}
		return e, nil
	case tkIdent:
		switch t.text {
		case "true":
			return &BoolLit{Value: true}, nil
		case "false":
			return &BoolLit{Value: false}, nil
		case "null":
			return &NullLit{}, nil
		case "undefined":
			return &UndefinedLit{}, nil
		case "this":
			return &ThisExpr{}, nil
		case "let", "var", "for", "return", "if", "else", "new", "throw", "try", "catch", "finally",
			"while", "do", "switch", "case", "default", "break", "continue", "instanceof":
			return nil, fmt.Errorf("parser: keyword %q in expression position at %d", t.text, t.pos)
		}
		return &Ident{Name: t.text}, nil
	}
	return nil, fmt.Errorf("parser: unexpected token at offset %d: %w",
		t.pos, jserrors.ErrNotImplemented)
}

func (p *parser) parseObjectLit() (Node, error) {
	if _, err := p.expect(tkLBrace, "'{'"); err != nil {
		return nil, err
	}
	var props []ObjectProp
	for p.peek().kind != tkRBrace && p.peek().kind != tkEOF {
		if len(props) > 0 {
			if p.peek().kind != tkComma {
				return nil, fmt.Errorf("parser: expected ',' or '}' in object at offset %d", p.peek().pos)
			}
			p.advance()
			if p.peek().kind == tkRBrace { // trailing comma
				break
			}
		}
		if p.peek().kind == tkEllipsis {
			p.advance()
			src, err := p.parseAssignment()
			if err != nil {
				return nil, err
			}
			props = append(props, ObjectProp{Spread: true, Value: src})
			continue
		}
		// Computed key: `{[expr]: value}`.
		if p.peek().kind == tkLBracket {
			p.advance()
			kExpr, err := p.parseAssignment()
			if err != nil {
				return nil, err
			}
			if _, err := p.expect(tkRBracket, "']'"); err != nil {
				return nil, err
			}
			// Method shorthand on computed key: `{[k]() {}}`.
			if p.peek().kind == tkLParen {
				params, body, err := p.parseFunctionTail()
				if err != nil {
					return nil, err
				}
				props = append(props, ObjectProp{
					KeyExpr: kExpr, Computed: true,
					Value: &FunctionExpr{Params: params, Body: body},
				})
				continue
			}
			if _, err := p.expect(tkColon, "':' after computed property key"); err != nil {
				return nil, err
			}
			v, err := p.parseAssignment()
			if err != nil {
				return nil, err
			}
			props = append(props, ObjectProp{KeyExpr: kExpr, Computed: true, Value: v})
			continue
		}
		// Accessor: `get name() {}` / `set name(v) {}` in object literals.
		// Same disambiguation rule as for class members.
		kind := ""
		if p.peek().kind == tkIdent && (p.peek().text == "get" || p.peek().text == "set") {
			if p.pos+1 < len(p.toks) {
				nxt := p.toks[p.pos+1].kind
				if nxt == tkIdent || nxt == tkStr {
					kind = p.advance().text
				}
			}
		}
		var key string
		switch p.peek().kind {
		case tkIdent:
			key = p.advance().text
		case tkStr:
			key = p.advance().text
		case tkNum:
			t := p.advance()
			key = formatNumberKey(t.num)
		default:
			return nil, fmt.Errorf("parser: expected property key at offset %d: %w",
				p.peek().pos, jserrors.ErrNotImplemented)
		}
		if kind != "" {
			params, body, err := p.parseFunctionTail()
			if err != nil {
				return nil, err
			}
			props = append(props, ObjectProp{
				Key:   key,
				Kind:  kind,
				Value: &FunctionExpr{Name: key, Params: params, Body: body},
			})
			continue
		}
		// Method shorthand: `{name() { body }}`.
		if p.peek().kind == tkLParen {
			params, body, err := p.parseFunctionTail()
			if err != nil {
				return nil, err
			}
			props = append(props, ObjectProp{
				Key: key,
				Value: &FunctionExpr{Name: key, Params: params, Body: body},
			})
			continue
		}
		// Shorthand: `{a, b}` ↔ `{a: a, b: b}`. Detected when the
		// token after the key isn't a colon — could be `,` `}` `=`.
		if p.peek().kind != tkColon {
			props = append(props, ObjectProp{Key: key, Value: &Ident{Name: key}})
			continue
		}
		p.advance() // ':'
		v, err := p.parseAssignment()
		if err != nil {
			return nil, err
		}
		props = append(props, ObjectProp{Key: key, Value: v})
	}
	if _, err := p.expect(tkRBrace, "'}'"); err != nil {
		return nil, err
	}
	return &ObjectLit{Props: props}, nil
}

// isArrowLookahead is called with p.peek() == tkLParen. It scans
// forward to the matched ')' and reports whether the very next
// token is '=>'. Side-effect free.
func (p *parser) isArrowLookahead() bool {
	depth := 0
	for j := p.pos; j < len(p.toks); j++ {
		switch p.toks[j].kind {
		case tkLParen:
			depth++
		case tkRParen:
			depth--
			if depth == 0 {
				return j+1 < len(p.toks) && p.toks[j+1].kind == tkArrow
			}
		case tkEOF:
			return false
		}
	}
	return false
}

func (p *parser) parseParenArrowFunction() (Node, error) {
	if _, err := p.expect(tkLParen, "'('"); err != nil {
		return nil, err
	}
	params, err := p.parseParamList()
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(tkRParen, "')'"); err != nil {
		return nil, err
	}
	if _, err := p.expect(tkArrow, "'=>'"); err != nil {
		return nil, err
	}
	return p.parseArrowBody(params)
}

func (p *parser) parseArrowBody(params []Param) (Node, error) {
	if p.peek().kind == tkLBrace {
		body, err := p.parseBlock()
		if err != nil {
			return nil, err
		}
		return &ArrowFunctionExpr{Params: params, Body: body.(*Block)}, nil
	}
	e, err := p.parseAssignment()
	if err != nil {
		return nil, err
	}
	return &ArrowFunctionExpr{Params: params, ExprBody: e}, nil
}

func (p *parser) parseArrayLit() (Node, error) {
	if _, err := p.expect(tkLBracket, "'['"); err != nil {
		return nil, err
	}
	var items []Node
	for p.peek().kind != tkRBracket && p.peek().kind != tkEOF {
		if len(items) > 0 {
			if _, err := p.expect(tkComma, "','"); err != nil {
				return nil, err
			}
			if p.peek().kind == tkRBracket { // trailing comma
				break
			}
		}
		if p.peek().kind == tkEllipsis {
			p.advance()
			arg, err := p.parseAssignment()
			if err != nil {
				return nil, err
			}
			items = append(items, &SpreadElement{Arg: arg})
			continue
		}
		v, err := p.parseAssignment()
		if err != nil {
			return nil, err
		}
		items = append(items, v)
	}
	if _, err := p.expect(tkRBracket, "']'"); err != nil {
		return nil, err
	}
	return &ArrayLit{Items: items}, nil
}

// canStartExpr reports whether tok could open an expression. Used by
// the `yield` parser to tell `yield foo` from a bare `yield;` — if
// the trailing tokens can't form an expression we treat it as a
// 0-arg yield producing undefined.
func canStartExpr(k tokKind) bool {
	switch k {
	case tkSemi, tkRParen, tkRBrace, tkRBracket, tkComma, tkColon, tkEOF:
		return false
	}
	return true
}

// formatNumberKey renders a numeric property key the way JS does:
// integers without a decimal point, others via strconv. Kept local
// so parser doesn't depend on the value package.
func formatNumberKey(f float64) string {
	if f == float64(int64(f)) {
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'g', -1, 64)
}

