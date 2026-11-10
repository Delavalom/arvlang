package parser

import (
	"github.com/delavalom/arvlang/arv/ast"
	"github.com/delavalom/arvlang/arv/token"
)

// parseForExpression parses for <identifier> in <expression> { <statements> }
// example: for url in urls { spawn check(url) }
func (p *Parser) parseForExpression() ast.Expression {
	exp := &ast.ForExpression{Token: p.curToken}
	if !p.expectPeek(token.IDENT) {
		return nil
	}
	exp.Var = &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal}
	if !p.expectPeek(token.IN) {
		return nil
	}
	if !p.enter() { // like an if condition: one nesting level
		return nil
	}
	p.nextToken()
	exp.Iterable = p.parseExpression(LOWEST)
	p.leave()
	if exp.Iterable == nil {
		return nil
	}
	if !p.expectPeek(token.LBRACE) {
		return nil
	}
	exp.Body = p.parseBlockStatement()
	return exp
}
