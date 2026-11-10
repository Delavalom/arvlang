package parser

import (
	"github.com/delavalom/arvlang/arv/ast"
	"github.com/delavalom/arvlang/arv/token"
)

// parseSpawnExpression parses spawn <call> [with <expression>]
// example: spawn check(url) with [net("api.github.com")]
func (p *Parser) parseSpawnExpression() ast.Expression {
	exp := &ast.SpawnExpression{Token: p.curToken}
	if !p.enter() { // spawn is a prefix operator: one nesting level
		return nil
	}
	defer p.leave()
	p.nextToken()
	call, ok := p.parseExpression(LOWEST).(*ast.CallExpression)
	if !ok {
		p.addError("spawn expects a function call, e.g. spawn f(x)")
		return nil
	}
	exp.Call = call
	if p.peekTokenIs(token.WITH) {
		p.nextToken()
		p.nextToken()
		exp.Caps = p.parseExpression(LOWEST)
	}
	return exp
}

// parseAwaitExpression parses await <expression>
// example: await t
func (p *Parser) parseAwaitExpression() ast.Expression {
	exp := &ast.AwaitExpression{Token: p.curToken}
	if !p.enter() { // await is a prefix operator: one nesting level
		return nil
	}
	defer p.leave()
	p.nextToken()
	exp.Task = p.parseExpression(PREFIX)
	return exp
}

// parseScopeExpression parses scope { <statements> }
func (p *Parser) parseScopeExpression() ast.Expression {
	exp := &ast.ScopeExpression{Token: p.curToken}
	if !p.expectPeek(token.LBRACE) {
		return nil
	}
	exp.Body = p.parseBlockStatement()
	return exp
}
