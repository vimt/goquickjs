package parser

import (
	"fmt"
	"strconv"

	"github.com/vimt/goquickjs/internal/jserrors"
)

type tokKind int

const (
	tkNum tokKind = iota
	tkStr
	tkIdent
	tkBigInt

	tkPlus
	tkMinus
	tkStar
	tkSlash
	tkPercent

	tkAssign
	tkPlusAssign
	tkMinusAssign
	tkStarAssign
	tkSlashAssign
	tkPercentAssign

	tkInc
	tkDec

	tkPow         // **
	tkPowAssign   // **=
	tkLandAssign  // &&=
	tkLorAssign   // ||=
	tkNullishAsgn // ??=
	tkBitAndAsgn  // &=
	tkBitOrAsgn   // |=
	tkBitXorAsgn  // ^=
	tkShlAssign   // <<=
	tkShrAssign   // >>=
	tkUShrAssign  // >>>=

	tkBang
	tkLt
	tkLe
	tkGt
	tkGe
	tkEq
	tkNeq
	tkStrictEq
	tkStrictNeq
	tkLand
	tkLor
	tkNullish
	tkArrow
	tkQuestion
	tkQDot

	tkBitAnd
	tkBitOr
	tkBitXor
	tkBitNot
	tkShl
	tkShr
	tkUShr

	tkLParen
	tkRParen
	tkLBrace
	tkRBrace
	tkLBracket
	tkRBracket
	tkSemi
	tkComma
	tkColon
	tkDot
	tkEllipsis // ...
	tkTemplate // `text${expr}text` collapsed into one token; see quasis/exprSrcs
	tkRegex    // /pat/flags — text holds pattern, regexFlags the flag string
	tkEOF
)

type token struct {
	kind tokKind
	num  float64
	text string
	// Template literal payload: quasis are the literal string chunks
	// (always len(exprSrcs)+1 entries), exprSrcs are the unparsed
	// `${...}` interior sources to be re-parsed by parseTemplate.
	quasis     []string
	exprSrcs   []string
	regexFlags string
	pos  int
}

// regexExpected reports whether `/` at the current position should
// start a regex literal rather than a division operator. Decision is
// made by the previous token's category: postfix-position tokens
// (numbers, names-as-value, ), ]) end an expression and make `/` a
// divisor; everything else, including the start of input, opens a
// regex.
func regexExpected(toks []token) bool {
	if len(toks) == 0 {
		return true
	}
	switch toks[len(toks)-1].kind {
	case tkNum, tkStr, tkRParen, tkRBracket, tkRegex:
		return false
	case tkIdent:
		// Most idents end an expression too — but JS keywords like
		// `return`, `typeof`, `in`, `instanceof`, `new`, `throw`,
		// `delete`, `void`, `case`, etc. don't. Whitelist the
		// keywords-as-prefix; everything else acts as a value.
		switch toks[len(toks)-1].text {
		case "return", "typeof", "void", "delete", "throw", "new",
			"in", "of", "instanceof", "case", "do", "else", "yield",
			"await", "if", "while", "for", "switch":
			return true
		}
		return false
	}
	return true
}

// scanRegex reads `/pattern/flags` starting at src[start] (where
// src[start] is the opening `/`). Handles `\/` escapes and
// character-class brackets so a `/` inside `[...]` doesn't end the
// pattern. Returns (pattern, flags, indexAfterFlags).
func scanRegex(src string, start int) (string, string, int, error) {
	j := start + 1
	inClass := false
	var pat []byte
	for j < len(src) {
		c := src[j]
		if c == '\\' && j+1 < len(src) {
			pat = append(pat, c, src[j+1])
			j += 2
			continue
		}
		if c == '[' {
			inClass = true
		} else if c == ']' {
			inClass = false
		} else if c == '/' && !inClass {
			j++
			break
		} else if c == '\n' {
			return "", "", 0, fmt.Errorf("parser: unterminated regex at %d", start)
		}
		pat = append(pat, c)
		j++
	}
	flagsStart := j
	for j < len(src) {
		c := src[j]
		if !(c == 'g' || c == 'i' || c == 'm' || c == 's' || c == 'u' || c == 'y') {
			break
		}
		j++
	}
	return string(pat), src[flagsStart:j], j, nil
}

// scanTemplate parses a backtick-quoted template literal at src[start]
// (where src[start] is the opening `). It walks the body collecting
// literal chunks and bracketed `${...}` expression source slices,
// honouring the usual escapes (\\ \n \t \` \$ \r \0 etc.). The
// returned quasis slice is always len(exprSrcs)+1 entries — the
// literal pieces interleaving the expressions. The expression
// sources are handed back unparsed; the higher-level parser is
// reinvoked on each one as needed.
func scanTemplate(src string, start int) ([]string, []string, int, error) {
	var quasis []string
	var exprs []string
	var b []byte
	j := start + 1
	for j < len(src) {
		switch {
		case src[j] == '`':
			quasis = append(quasis, string(b))
			return quasis, exprs, j + 1, nil
		case src[j] == '\\' && j+1 < len(src):
			switch src[j+1] {
			case 'n':
				b = append(b, '\n')
			case 't':
				b = append(b, '\t')
			case 'r':
				b = append(b, '\r')
			case '\\':
				b = append(b, '\\')
			case '`':
				b = append(b, '`')
			case '$':
				b = append(b, '$')
			case '0':
				b = append(b, 0)
			default:
				b = append(b, src[j+1])
			}
			j += 2
		case src[j] == '$' && j+1 < len(src) && src[j+1] == '{':
			quasis = append(quasis, string(b))
			b = nil
			// Scan to matching `}`, tracking nested {} (strings and
			// template literals inside the expression would each need
			// careful skipping; minimal support for now: only nest
			// plain {} pairs).
			depth := 1
			k := j + 2
			exprStart := k
			for k < len(src) && depth > 0 {
				switch src[k] {
				case '{':
					depth++
					k++
				case '}':
					depth--
					if depth == 0 {
						exprs = append(exprs, src[exprStart:k])
						k++
						continue
					}
					k++
				case '"', '\'':
					_, end, err := scanString(src, k)
					if err != nil {
						return nil, nil, 0, err
					}
					k = end
				case '`':
					// Nested template — find its end via recursive scan.
					_, _, end, err := scanTemplate(src, k)
					if err != nil {
						return nil, nil, 0, err
					}
					k = end
				default:
					k++
				}
			}
			if depth != 0 {
				return nil, nil, 0, fmt.Errorf("parser: unterminated ${} in template at offset %d", j)
			}
			j = k
		default:
			b = append(b, src[j])
			j++
		}
	}
	return nil, nil, 0, fmt.Errorf("parser: unterminated template literal at offset %d", start)
}

// scanString reads a "..." or '...' literal starting at src[start]
// and returns the decoded body and the index of the byte after the
// closing quote.
func scanString(src string, start int) (string, int, error) {
	quote := src[start]
	var b []byte
	j := start + 1
	for j < len(src) && src[j] != quote {
		if src[j] == '\n' {
			return "", 0, fmt.Errorf("parser: unterminated string at %d", start)
		}
		if src[j] == '\\' && j+1 < len(src) {
			switch src[j+1] {
			case 'n':
				b = append(b, '\n')
			case 't':
				b = append(b, '\t')
			case 'r':
				b = append(b, '\r')
			case '\\':
				b = append(b, '\\')
			case '"':
				b = append(b, '"')
			case '\'':
				b = append(b, '\'')
			case '0':
				b = append(b, 0)
			default:
				// Unknown escape: keep the char verbatim. Tighten when
				// a corpus entry forces the issue.
				b = append(b, src[j+1])
			}
			j += 2
			continue
		}
		b = append(b, src[j])
		j++
	}
	if j >= len(src) {
		return "", 0, fmt.Errorf("parser: unterminated string at %d", start)
	}
	return string(b), j + 1, nil
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// stripUnderscores drops `_` separators from a numeric literal source
// before handing it to strconv (which doesn't recognise them).
func stripUnderscores(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == '_' {
			out := make([]byte, 0, len(s))
			out = append(out, s[:i]...)
			for j := i; j < len(s); j++ {
				if s[j] != '_' {
					out = append(out, s[j])
				}
			}
			return string(out)
		}
	}
	return s
}

func isBaseDigit(c byte, base int) bool {
	if base == 16 {
		return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
	}
	return c >= '0' && int(c-'0') < base
}
func isIdentStart(c byte) bool { return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_' || c == '$' }
func isIdentPart(c byte) bool  { return isIdentStart(c) || isDigit(c) }
func isSpace(c byte) bool      { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }

func tokenize(src string) ([]token, error) {
	var toks []token
	i := 0
	for i < len(src) {
		c := src[i]
		switch {
		case isSpace(c):
			i++
		case isDigit(c):
			// 0x / 0o / 0b prefix → integer literal in that base.
			if c == '0' && i+1 < len(src) {
				base := 0
				switch src[i+1] {
				case 'x', 'X':
					base = 16
				case 'o', 'O':
					base = 8
				case 'b', 'B':
					base = 2
				}
				if base != 0 {
					j := i + 2
					for j < len(src) && (isBaseDigit(src[j], base) || src[j] == '_') {
						j++
					}
					if j == i+2 {
						return nil, fmt.Errorf("parser: empty base-%d literal at %d", base, i)
					}
					clean := stripUnderscores(src[i+2 : j])
					if j < len(src) && src[j] == 'n' {
						toks = append(toks, token{kind: tkBigInt, text: clean, regexFlags: strconv.Itoa(base), pos: i})
						i = j + 1
						continue
					}
					n, err := strconv.ParseInt(clean, base, 64)
					if err != nil {
						return nil, fmt.Errorf("parser: bad number literal at %d: %w", i, err)
					}
					toks = append(toks, token{kind: tkNum, num: float64(n), pos: i})
					i = j
					continue
				}
			}
			// Decimal: digits (with optional `_` separators), optional
			// fractional part, optional exponent.
			j := i
			for j < len(src) && (isDigit(src[j]) || src[j] == '_') {
				j++
			}
			if j < len(src) && src[j] == '.' {
				j++
				for j < len(src) && (isDigit(src[j]) || src[j] == '_') {
					j++
				}
			}
			if j < len(src) && (src[j] == 'e' || src[j] == 'E') {
				j++
				if j < len(src) && (src[j] == '+' || src[j] == '-') {
					j++
				}
				for j < len(src) && (isDigit(src[j]) || src[j] == '_') {
					j++
				}
			}
			// BigInt literal suffix `n` (decimal only — no fraction/exp).
			if j < len(src) && src[j] == 'n' {
				clean := stripUnderscores(src[i:j])
				toks = append(toks, token{kind: tkBigInt, text: clean, regexFlags: "10", pos: i})
				i = j + 1
				continue
			}
			f, err := strconv.ParseFloat(stripUnderscores(src[i:j]), 64)
			if err != nil {
				return nil, fmt.Errorf("parser: bad number literal at %d: %w", i, err)
			}
			toks = append(toks, token{kind: tkNum, num: f, pos: i})
			i = j
		case isIdentStart(c):
			j := i
			for j < len(src) && isIdentPart(src[j]) {
				j++
			}
			toks = append(toks, token{kind: tkIdent, text: src[i:j], pos: i})
			i = j
		case c == '"' || c == '\'':
			s, end, err := scanString(src, i)
			if err != nil {
				return nil, err
			}
			toks = append(toks, token{kind: tkStr, text: s, pos: i})
			i = end
		case c == '`':
			quasis, exprs, end, err := scanTemplate(src, i)
			if err != nil {
				return nil, err
			}
			toks = append(toks, token{kind: tkTemplate, pos: i, quasis: quasis, exprSrcs: exprs})
			i = end

		case c == '+':
			switch {
			case i+1 < len(src) && src[i+1] == '+':
				toks = append(toks, token{kind: tkInc, pos: i})
				i += 2
			case i+1 < len(src) && src[i+1] == '=':
				toks = append(toks, token{kind: tkPlusAssign, pos: i})
				i += 2
			default:
				toks = append(toks, token{kind: tkPlus, pos: i})
				i++
			}
		case c == '-':
			switch {
			case i+1 < len(src) && src[i+1] == '-':
				toks = append(toks, token{kind: tkDec, pos: i})
				i += 2
			case i+1 < len(src) && src[i+1] == '=':
				toks = append(toks, token{kind: tkMinusAssign, pos: i})
				i += 2
			default:
				toks = append(toks, token{kind: tkMinus, pos: i})
				i++
			}
		case c == '*':
			switch {
			case i+2 < len(src) && src[i+1] == '*' && src[i+2] == '=':
				toks = append(toks, token{kind: tkPowAssign, pos: i})
				i += 3
			case i+1 < len(src) && src[i+1] == '*':
				toks = append(toks, token{kind: tkPow, pos: i})
				i += 2
			case i+1 < len(src) && src[i+1] == '=':
				toks = append(toks, token{kind: tkStarAssign, pos: i})
				i += 2
			default:
				toks = append(toks, token{kind: tkStar, pos: i})
				i++
			}
		case c == '%':
			if i+1 < len(src) && src[i+1] == '=' {
				toks = append(toks, token{kind: tkPercentAssign, pos: i})
				i += 2
			} else {
				toks = append(toks, token{kind: tkPercent, pos: i})
				i++
			}
		case c == '/':
			switch {
			case i+1 < len(src) && src[i+1] == '/':
				// Line comment — skip to end-of-line.
				j := i + 2
				for j < len(src) && src[j] != '\n' {
					j++
				}
				i = j
			case i+1 < len(src) && src[i+1] == '*':
				// Block comment — skip past matching `*/`.
				j := i + 2
				for j+1 < len(src) && !(src[j] == '*' && src[j+1] == '/') {
					j++
				}
				if j+1 >= len(src) {
					return nil, fmt.Errorf("parser: unterminated block comment at %d", i)
				}
				i = j + 2
			case i+1 < len(src) && src[i+1] == '=' && !regexExpected(toks):
				toks = append(toks, token{kind: tkSlashAssign, pos: i})
				i += 2
			default:
				// Regex literal vs division: peek the previous token.
				// If the previous token can end an expression (number,
				// ident-as-value, ), ]), `/` is division. Otherwise
				// (start of input, after operator/keyword/punctuator)
				// it's a regex literal opening.
				if regexExpected(toks) {
					pat, flags, end, err := scanRegex(src, i)
					if err != nil {
						return nil, err
					}
					toks = append(toks, token{kind: tkRegex, text: pat, regexFlags: flags, pos: i})
					i = end
				} else {
					toks = append(toks, token{kind: tkSlash, pos: i})
					i++
				}
			}
		case c == '(':
			toks = append(toks, token{kind: tkLParen, pos: i})
			i++
		case c == ')':
			toks = append(toks, token{kind: tkRParen, pos: i})
			i++
		case c == '{':
			toks = append(toks, token{kind: tkLBrace, pos: i})
			i++
		case c == '}':
			toks = append(toks, token{kind: tkRBrace, pos: i})
			i++
		case c == '[':
			toks = append(toks, token{kind: tkLBracket, pos: i})
			i++
		case c == ']':
			toks = append(toks, token{kind: tkRBracket, pos: i})
			i++
		case c == ';':
			toks = append(toks, token{kind: tkSemi, pos: i})
			i++
		case c == ',':
			toks = append(toks, token{kind: tkComma, pos: i})
			i++
		case c == ':':
			toks = append(toks, token{kind: tkColon, pos: i})
			i++
		case c == '.':
			if i+2 < len(src) && src[i+1] == '.' && src[i+2] == '.' {
				toks = append(toks, token{kind: tkEllipsis, pos: i})
				i += 3
			} else {
				toks = append(toks, token{kind: tkDot, pos: i})
				i++
			}

		case c == '<':
			switch {
			case i+2 < len(src) && src[i+1] == '<' && src[i+2] == '=':
				toks = append(toks, token{kind: tkShlAssign, pos: i})
				i += 3
			case i+1 < len(src) && src[i+1] == '<':
				toks = append(toks, token{kind: tkShl, pos: i})
				i += 2
			case i+1 < len(src) && src[i+1] == '=':
				toks = append(toks, token{kind: tkLe, pos: i})
				i += 2
			default:
				toks = append(toks, token{kind: tkLt, pos: i})
				i++
			}
		case c == '>':
			switch {
			case i+3 < len(src) && src[i+1] == '>' && src[i+2] == '>' && src[i+3] == '=':
				toks = append(toks, token{kind: tkUShrAssign, pos: i})
				i += 4
			case i+2 < len(src) && src[i+1] == '>' && src[i+2] == '>':
				toks = append(toks, token{kind: tkUShr, pos: i})
				i += 3
			case i+2 < len(src) && src[i+1] == '>' && src[i+2] == '=':
				toks = append(toks, token{kind: tkShrAssign, pos: i})
				i += 3
			case i+1 < len(src) && src[i+1] == '>':
				toks = append(toks, token{kind: tkShr, pos: i})
				i += 2
			case i+1 < len(src) && src[i+1] == '=':
				toks = append(toks, token{kind: tkGe, pos: i})
				i += 2
			default:
				toks = append(toks, token{kind: tkGt, pos: i})
				i++
			}
		case c == '!':
			switch {
			case i+2 < len(src) && src[i+1] == '=' && src[i+2] == '=':
				toks = append(toks, token{kind: tkStrictNeq, pos: i})
				i += 3
			case i+1 < len(src) && src[i+1] == '=':
				toks = append(toks, token{kind: tkNeq, pos: i})
				i += 2
			default:
				toks = append(toks, token{kind: tkBang, pos: i})
				i++
			}
		case c == '=':
			switch {
			case i+2 < len(src) && src[i+1] == '=' && src[i+2] == '=':
				toks = append(toks, token{kind: tkStrictEq, pos: i})
				i += 3
			case i+1 < len(src) && src[i+1] == '=':
				toks = append(toks, token{kind: tkEq, pos: i})
				i += 2
			case i+1 < len(src) && src[i+1] == '>':
				toks = append(toks, token{kind: tkArrow, pos: i})
				i += 2
			default:
				toks = append(toks, token{kind: tkAssign, pos: i})
				i++
			}
		case c == '&':
			switch {
			case i+2 < len(src) && src[i+1] == '&' && src[i+2] == '=':
				toks = append(toks, token{kind: tkLandAssign, pos: i})
				i += 3
			case i+1 < len(src) && src[i+1] == '&':
				toks = append(toks, token{kind: tkLand, pos: i})
				i += 2
			case i+1 < len(src) && src[i+1] == '=':
				toks = append(toks, token{kind: tkBitAndAsgn, pos: i})
				i += 2
			default:
				toks = append(toks, token{kind: tkBitAnd, pos: i})
				i++
			}
		case c == '|':
			switch {
			case i+2 < len(src) && src[i+1] == '|' && src[i+2] == '=':
				toks = append(toks, token{kind: tkLorAssign, pos: i})
				i += 3
			case i+1 < len(src) && src[i+1] == '|':
				toks = append(toks, token{kind: tkLor, pos: i})
				i += 2
			case i+1 < len(src) && src[i+1] == '=':
				toks = append(toks, token{kind: tkBitOrAsgn, pos: i})
				i += 2
			default:
				toks = append(toks, token{kind: tkBitOr, pos: i})
				i++
			}
		case c == '^':
			if i+1 < len(src) && src[i+1] == '=' {
				toks = append(toks, token{kind: tkBitXorAsgn, pos: i})
				i += 2
			} else {
				toks = append(toks, token{kind: tkBitXor, pos: i})
				i++
			}
		case c == '~':
			toks = append(toks, token{kind: tkBitNot, pos: i})
			i++
		case c == '?':
			switch {
			case i+2 < len(src) && src[i+1] == '?' && src[i+2] == '=':
				toks = append(toks, token{kind: tkNullishAsgn, pos: i})
				i += 3
			case i+1 < len(src) && src[i+1] == '?':
				toks = append(toks, token{kind: tkNullish, pos: i})
				i += 2
			case i+1 < len(src) && src[i+1] == '.':
				toks = append(toks, token{kind: tkQDot, pos: i})
				i += 2
			default:
				toks = append(toks, token{kind: tkQuestion, pos: i})
				i++
			}

		default:
			return nil, fmt.Errorf("parser: %q at offset %d: %w",
				string(c), i, jserrors.ErrNotImplemented)
		}
	}
	toks = append(toks, token{kind: tkEOF, pos: len(src)})
	return toks, nil
}

// --- parser ---
