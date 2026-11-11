package parser

import (
	"fmt"

	"github.com/delavalom/arvlang/arv/ast"
	"github.com/delavalom/arvlang/arv/token"
)

// parseMatchExpression parses
// match <expression> { ok(<ident>) => <expression>, err(<ident>) => <expression> }
// example: match await t { ok(v) => v, err(e) => e["message"] }
// The arms may come in either order, with an optional trailing comma, but
// each must be there exactly once: a match without its err arm (or its ok
// arm) is a parse error, so error handling cannot be half written. ok and
// err are names only here, not reserved words.
func (p *Parser) parseMatchExpression() ast.Expression {
	exp := &ast.MatchExpression{Token: p.curToken}
	if !p.enter() { // like an if: one nesting level
		return nil
	}
	defer p.leave()
	p.nextToken()
	if exp.Subject = p.parseExpression(LOWEST); exp.Subject == nil {
		return nil
	}
	if !p.expectPeek(token.LBRACE) {
		return nil
	}
	for !p.peekTokenIs(token.RBRACE) {
		if !p.parseMatchArm(exp) {
			return nil
		}
		if !p.peekTokenIs(token.COMMA) {
			break
		}
		p.nextToken()
	}
	if !p.expectPeek(token.RBRACE) {
		return nil
	}
	if exp.Ok == nil || exp.Err == nil {
		missing := "ok"
		if exp.Ok != nil {
			missing = "err"
		}
		p.errorAt(exp.Token, fmt.Sprintf("match needs an ok arm and an err arm; %s is missing", missing))
		return nil
	}
	return exp
}

// parseMatchArm parses one arm, ok(<ident>) => <expression> or
// err(<ident>) => <expression>, which starts at peekToken, into exp.
func (p *Parser) parseMatchArm(exp *ast.MatchExpression) bool {
	if !p.expectPeek(token.IDENT) {
		return false
	}
	arm := p.curToken
	switch {
	case arm.Literal != "ok" && arm.Literal != "err":
		p.errorAt(arm, fmt.Sprintf("match arm must be ok(name) or err(name), got %s", arm.Literal))
		return false
	case arm.Literal == "ok" && exp.Ok != nil, arm.Literal == "err" && exp.Err != nil:
		p.errorAt(arm, fmt.Sprintf("duplicate match arm %s", arm.Literal))
		return false
	}
	if !p.expectPeek(token.LPAREN) || !p.expectPeek(token.IDENT) {
		return false
	}
	name := &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal}
	if !p.expectPeek(token.RPAREN) || !p.expectPeek(token.ARROW) {
		return false
	}
	p.nextToken()
	body := p.parseExpression(LOWEST)
	if body == nil {
		return false
	}
	if arm.Literal == "ok" {
		exp.OkName, exp.Ok = name, body
	} else {
		exp.ErrName, exp.Err = name, body
	}
	return true
}
