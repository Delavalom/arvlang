package parser

import (
	"fmt"
	"strings"

	"github.com/delavalom/arvlang/arv/ast"
	"github.com/delavalom/arvlang/arv/lexer"
	"github.com/delavalom/arvlang/arv/token"
)

// parseStringLiteral parses a string literal. One holding ${ is
// interpolated (parseInterpolated); any other means what it always did.
func (p *Parser) parseStringLiteral() ast.Expression {
	tok := p.curToken
	if !strings.Contains(tok.Literal, "${") {
		return &ast.StringLiteral{Token: tok, Value: tok.Literal}
	}
	return p.parseInterpolated(tok)
}

// parseInterpolated parses a string literal holding ${...} into the
// concatenation it means, built from nodes that already exist:
//
//	"a ${x} b"   is   "a " + $str(x) + " b"
//
// $str is the str builtin under a name no program can bind
// (ast.StrBuiltin), so each value prints as puts prints it. Empty pieces
// are left out: "${x}" is $str(x). \${ is a literal ${, and a literal
// holding only that kind of ${ stays one string literal. The expression
// ends at the } that balances the ${, and is lexed where it sits in the
// source, so its tokens and errors carry their real positions. It cannot
// hold a string literal: the lexer ends the string at its first quote.
func (p *Parser) parseInterpolated(tok token.Token) ast.Expression {
	if !p.enter() {
		return nil
	}
	defer p.leave()
	s := &spans{src: tok.Literal, line: tok.Line, col: tok.Col + 1}
	var parts []ast.Expression
	var piece strings.Builder
	pieceAt := tok // the first piece sits at the opening quote
	interpolated := false
	flush := func() {
		if piece.Len() > 0 {
			text := piece.String()
			parts = append(parts, &ast.StringLiteral{Token: token.Token{
				Type: token.STRING, Literal: text, Line: pieceAt.Line, Col: pieceAt.Col,
			}, Value: text})
			piece.Reset()
		}
	}
	for s.i < len(s.src) {
		if piece.Len() == 0 && len(parts) > 0 {
			pieceAt = s.at()
		}
		rest := s.src[s.i:]
		switch {
		case strings.HasPrefix(rest, `\${`):
			piece.WriteString("${")
			s.skip(3)
		case strings.HasPrefix(rest, "${"):
			flush()
			at := s.at()
			e := p.parseEmbedded(s, at)
			if e == nil {
				return nil
			}
			parts = append(parts, strCall(at, e))
			interpolated = true
		default:
			piece.WriteByte(rest[0])
			s.skip(1)
		}
	}
	flush()
	if !interpolated {
		return &ast.StringLiteral{Token: tok, Value: parts[0].(*ast.StringLiteral).Value}
	}
	result := parts[0]
	for _, part := range parts[1:] {
		line, col := ast.Pos(part)
		result = &ast.InfixExpression{
			Token:    token.Token{Type: token.PLUS, Literal: "+", Line: line, Col: col},
			Operator: "+", Left: result, Right: part,
		}
	}
	return result
}

// parseEmbedded parses the expression of the ${ at s's position (at), and
// moves s past its closing }. It lexes the expression in place, with this
// parser's own state, so nesting and errors count as anywhere else. It
// returns nil after recording an error.
func (p *Parser) parseEmbedded(s *spans, at token.Token) ast.Expression {
	end := closingBrace(s.src, s.i+2)
	if end < 0 {
		p.errorAt(at, "unterminated ${ in string")
		return nil
	}
	s.skip(2)
	src := s.src[s.i:end]
	if strings.TrimSpace(src) == "" {
		p.errorAt(at, "empty ${} in string")
		return nil
	}
	l, cur, peek, errs := p.l, p.curToken, p.peekToken, len(p.errors)
	p.l = lexer.NewAt(src, s.line, s.col)
	p.nextToken()
	p.nextToken()
	e := p.parseExpression(LOWEST)
	if len(p.errors) == errs && !p.peekTokenIs(token.EOF) {
		p.errorAt(p.peekToken, fmt.Sprintf("expected } to end ${, got %s instead", p.peekToken.Type))
	}
	p.l = l
	if p.tooDeep { // the nesting limit stopped parsing: the tokens stay EOF
		return nil
	}
	p.curToken, p.peekToken = cur, peek
	s.skip(end + 1 - s.i)
	if len(p.errors) > errs {
		return nil
	}
	return e
}

// closingBrace is the index in s of the } that closes a ${ whose
// expression starts at from, or -1 if none does. It skips // comments to
// the end of their line, as the lexer does: the expression holds no
// string literal, so outside a comment every // starts one.
func closingBrace(s string, from int) int {
	depth := 1
	for j := from; j < len(s); j++ {
		switch s[j] {
		case '/':
			if j+1 < len(s) && s[j+1] == '/' {
				for j < len(s) && s[j] != '\n' && s[j] != 0 {
					j++
				}
			}
		case '{':
			depth++
		case '}':
			if depth--; depth == 0 {
				return j
			}
		}
	}
	return -1
}

// strCall is $str(e), placed at the ${ that held e.
func strCall(at token.Token, e ast.Expression) ast.Expression {
	at.Type, at.Literal = token.IDENT, ast.StrBuiltin
	fn := &ast.Identifier{Token: at, Value: ast.StrBuiltin}
	at.Type, at.Literal = token.LPAREN, "("
	return &ast.CallExpression{Token: at, Function: fn, Arguments: []ast.Expression{e}}
}

// spans walks the text of a string literal, keeping the source position
// (line and column, as the lexer counts them) of the byte at i.
type spans struct {
	src       string
	i         int
	line, col int
}

func (s *spans) at() token.Token { return token.Token{Line: s.line, Col: s.col} }

func (s *spans) skip(n int) {
	for ; n > 0; n-- {
		if s.src[s.i] == '\n' {
			s.line++
			s.col = 1
		} else {
			s.col++
		}
		s.i++
	}
}
