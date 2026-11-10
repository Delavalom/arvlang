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
		p.errorAt(exp.Token, "spawn expects a function call, e.g. spawn f(x)")
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

// parseScopeExpression parses scope [(<name>: <expression>, ...)] { <statements> }
// example: scope (timeout_ms: 2000, tasks: 20) { spawn f() }
// A block starts with {, so the ( of an option list is unambiguous.
func (p *Parser) parseScopeExpression() ast.Expression {
	exp := &ast.ScopeExpression{Token: p.curToken}
	if p.peekTokenIs(token.LPAREN) {
		p.nextToken()
		if !p.parseScopeOptions(exp) {
			return nil
		}
	}
	if !p.expectPeek(token.LBRACE) {
		return nil
	}
	exp.Body = p.parseBlockStatement()
	return exp
}

// parseScopeOptions parses (<name>: <expression>, ...) with curToken on
// the (. An empty list, (), is allowed. It reports whether parsing went on
// to the closing ).
func (p *Parser) parseScopeOptions(exp *ast.ScopeExpression) bool {
	if !p.enter() { // like a parenthesised expression: one nesting level
		return false
	}
	defer p.leave()
	exp.Options = []ast.ScopeOption{}
	if p.peekTokenIs(token.RPAREN) {
		p.nextToken()
		return true
	}
	for {
		if !p.expectPeek(token.IDENT) {
			return false
		}
		opt := ast.ScopeOption{Token: p.curToken, Name: p.curToken.Literal}
		if !p.expectPeek(token.COLON) {
			return false
		}
		p.nextToken()
		if opt.Value = p.parseExpression(LOWEST); opt.Value == nil {
			return false
		}
		exp.Options = append(exp.Options, opt)
		if !p.peekTokenIs(token.COMMA) {
			return p.expectPeek(token.RPAREN)
		}
		p.nextToken()
	}
}
