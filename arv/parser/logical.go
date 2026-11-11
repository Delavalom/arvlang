package parser

import (
	"github.com/delavalom/arvlang/arv/ast"
	"github.com/delavalom/arvlang/arv/token"
)

// parseLogicalExpression parses a && b and a || b into the if expressions
// they mean, so nothing downstream sees a new node:
//
//	a && b   is   if (a) { b } else { false }
//	a || b   is   if (a) { true } else { b }
//
// b runs only when a does not decide. As in if, only true counts as true.
// The if sits where a starts, as an infix expression does; its token is
// the operator, so it prints as (a && b).
func (p *Parser) parseLogicalExpression(left ast.Expression) ast.Expression {
	op := p.curToken
	precedence := p.curPrecedence()
	p.nextToken()
	start := p.curToken
	right := p.parseExpression(precedence)

	at := op
	if line, col := ast.Pos(left); line != 0 {
		at.Line, at.Col = line, col
	}
	constant := &ast.Boolean{Token: token.Token{Type: token.FALSE, Literal: "false", Line: op.Line, Col: op.Col}}
	yes, no := oneExpression(start, right), oneExpression(op, constant)
	if op.Type == token.OR {
		constant.Token.Type, constant.Token.Literal, constant.Value = token.TRUE, "true", true
		yes, no = no, yes
	}
	return &ast.IfExpression{Token: at, Condition: left, Consequence: yes, Alternative: no}
}

// oneExpression is the block { e }, placed at tok.
func oneExpression(tok token.Token, e ast.Expression) *ast.BlockStatement {
	return &ast.BlockStatement{
		Token:      tok,
		Statements: []ast.Statement{&ast.ExpressionStatement{Token: tok, Expression: e}},
	}
}
