package parser

import (
	"fmt"

	"github.com/delavalom/arvlang/arv/ast"
	"github.com/delavalom/arvlang/arv/lexer"
	"github.com/delavalom/arvlang/arv/token"
)

// parseFieldExpression parses r.name, which means r["name"]: it builds
// the IndexExpression r["name"] would, with the key a string literal
// placed at the name, so nothing downstream sees a new node. Any
// identifier or keyword names a field (r.in, r.match).
func (p *Parser) parseFieldExpression(left ast.Expression) ast.Expression {
	dot := p.curToken
	name := p.peekToken
	if lexer.LookupIdent(name.Literal) != name.Type {
		p.errorAt(name, fmt.Sprintf("expected a field name after ., got %s instead", name.Type))
		return nil
	}
	p.nextToken()
	key := &ast.StringLiteral{
		Token: token.Token{Type: token.STRING, Literal: name.Literal, Line: name.Line, Col: name.Col},
		Value: name.Literal,
	}
	return &ast.IndexExpression{Token: dot, Left: left, Index: key}
}
