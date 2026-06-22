// Package parser is the tokenizer + AST builder for the in-progress
// JS engine. The supported surface grows one feature at a time;
// anything outside it returns an error wrapping jserrors.ErrNotImplemented
// so the differential test harness skips rather than fails the entry.
//
// File layout:
//   - parser.go (this file): Parse + parser struct + lookahead helpers
//   - ast.go: AST node types and astNode marker methods
//   - lex.go: tokenize + scan helpers (string / template / regex)
//   - parse_stmt.go: statement-level parsers
//   - parse_expr.go: expression-level parsers (Pratt-style precedence chain)
//   - parse_pattern.go: destructuring pattern parsers
package parser

import (
	"fmt"
)

type parser struct {
	toks []token
	pos  int
	// noIn suppresses `in` as a relational binary operator. Set while
	// parsing a for-init expression (to support `for (x in obj)`):
	// `in` there marks the loop header, not a binop. Saved+restored
	// by callers — never read at any other site.
	noIn bool
}

// Parse turns a JS source string into a Program AST.
func Parse(src string) (*Program, error) {
	toks, err := tokenize(src)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks}
	var body []Node
	for p.peek().kind != tkEOF {
		s, err := p.parseStatement()
		if err != nil {
			return nil, err
		}
		body = append(body, s)
	}
	return &Program{Body: body}, nil
}

// parseExpressionString re-tokenises src and parses it as a single
// JS expression. Used by template-literal interpolations to lower
// each `${...}` body. Reports an error if leftover tokens remain.
func parseExpressionString(src string) (Node, error) {
	toks, err := tokenize(src)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks}
	e, err := p.parseExpression()
	if err != nil {
		return nil, err
	}
	if p.peek().kind != tkEOF {
		return nil, fmt.Errorf("parser: trailing tokens in template expr at offset %d", p.peek().pos)
	}
	return e, nil
}

func (p *parser) peek() token    { return p.toks[p.pos] }
func (p *parser) advance() token { t := p.toks[p.pos]; p.pos++; return t }

func (p *parser) expect(k tokKind, what string) (token, error) {
	t := p.advance()
	if t.kind != k {
		return t, fmt.Errorf("parser: expected %s at offset %d", what, t.pos)
	}
	return t, nil
}

func (p *parser) consumeSemi() {
	if p.peek().kind == tkSemi {
		p.advance()
	}
}
