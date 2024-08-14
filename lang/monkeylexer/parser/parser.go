package parser

import (
	"fmt"
	"strconv"

	"github.com/delavalom/arvlang/lang/monkeylexer"
	"github.com/delavalom/arvlang/lang/monkeylexer/ast"
)

const (
	_ int = iota
	LOWEST
	EQUALS
	LESSGREATER // == LESSGREATER // > or <
	SUM         //+
	PRODUCT     //*
	PREFIX      //-Xor!X
	CALL        // myFunction(X)
)

type Parser struct {
	l         *monkeylexer.Lexer
	curToken  monkeylexer.Token
	peekToken monkeylexer.Token
	errors    []string

	prefixParseFns map[monkeylexer.TokenType]prefixParseFn
	infixParseFns  map[monkeylexer.TokenType]infixParseFn
}

type (
	prefixParseFn func() ast.Expression
	infixParseFn  func(ast.Expression) ast.Expression
)

func New(l *monkeylexer.Lexer) *Parser {
	p := &Parser{
		l:      l,
		errors: []string{},
	}
	p.prefixParseFns = make(map[monkeylexer.TokenType]prefixParseFn)
	p.registerPrefix(monkeylexer.IDENT, p.parseIdentifier)
	p.registerPrefix(monkeylexer.INT, p.parseIntegerLiteral)
	p.registerPrefix(monkeylexer.BANG, p.parsePrefixExpression)
	p.registerPrefix(monkeylexer.MINUS, p.parsePrefixExpression)
	p.registerInfix(monkeylexer.SLASH, p.parseInfixExpression)
	p.registerInfix(monkeylexer.ASTERISK, p.parseInfixExpression)
	p.registerInfix(monkeylexer.EQ, p.parseInfixExpression)
	p.registerInfix(monkeylexer.NOT_EQ, p.parseInfixExpression)
	p.registerInfix(monkeylexer.LT, p.parseInfixExpression)
	p.registerInfix(monkeylexer.GT, p.parseInfixExpression)

	// Read two tokens, so curToken and peekToken are both set
	p.nextToken()

	p.nextToken()
	return p
}

func (p *Parser) Errors() []string {
	return p.errors
}

func (p *Parser) parseIdentifier() ast.Expression {
	return &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal}
}

func (p *Parser) registerPrefix(tokenType monkeylexer.TokenType, fn prefixParseFn) {
	p.prefixParseFns[tokenType] = fn
}
func (p *Parser) registerInfix(tokenType monkeylexer.TokenType, fn infixParseFn) {
	p.infixParseFns[tokenType] = fn
}

func (p *Parser) peekError(t monkeylexer.TokenType) {
	msg := fmt.Sprintf("expected next token to be %s, got %s instead",
		t, p.peekToken.Type)
	p.errors = append(p.errors, msg)
}

func (p *Parser) nextToken() {
	p.curToken = p.peekToken
	p.peekToken = p.l.NextToken()
}

func (p *Parser) parseStatement() ast.Statement {
	switch p.curToken.Type {
	case monkeylexer.LET:
		return p.parseLetStatement()
	case monkeylexer.RETURN:
		return p.parseReturnStatement()
	default:
		return p.parseExpressionStatement()
	}
}

func (p *Parser) ParseProgram() *ast.Program {
	program := &ast.Program{}
	program.Statements = []ast.Statement{}
	for p.curToken.Type != monkeylexer.EOF {
		stmt := p.parseStatement()
		if stmt != nil {
			program.Statements = append(program.Statements, stmt)
		}
		p.nextToken()
	}
	return program
}

func (p *Parser) noPrefixParseFnError(t monkeylexer.TokenType) {
	msg := fmt.Sprintf("no prefix parse function for %s found", t)
	p.errors = append(p.errors, msg)
}

func (p *Parser) parseExpression(precedence int) ast.Expression {
	prefix := p.prefixParseFns[p.curToken.Type]
	if prefix == nil {
		p.noPrefixParseFnError(p.curToken.Type)
		return nil
	}
	leftExp := prefix()
	for !p.peekTokenIs(monkeylexer.SEMICOLON) && precedence < p.peekPrecedence() {
		infix := p.infixParseFns[p.peekToken.Type]
		if infix == nil {
			return leftExp
		}
		p.nextToken()
		leftExp = infix(leftExp)
	}
	return leftExp
}

func (p *Parser) parseExpressionStatement() *ast.ExpressionStatement {
	stmt := &ast.ExpressionStatement{Token: p.curToken}
	stmt.Expression = p.parseExpression(LOWEST)
	if p.peekTokenIs(monkeylexer.SEMICOLON) {
		p.nextToken()
	}
	return stmt
}

func (p *Parser) parsePrefixExpression() ast.Expression {
	expression := &ast.PrefixExpression{
		Token:    p.curToken,
		Operator: p.curToken.Literal,
	}
	p.nextToken()
	expression.Right = p.parseExpression(PREFIX)
	return expression
}

func (p *Parser) parseLetStatement() *ast.LetStatement {
	stmt := &ast.LetStatement{Token: p.curToken}
	if !p.expectPeek(monkeylexer.IDENT) {
		return nil
	}
	stmt.Name = &ast.Identifier{
		Token: p.curToken,
		Value: p.curToken.Literal,
	}
	if !p.expectPeek(monkeylexer.ASSIGN) {
		return nil
	}
	// TODO: We're skipping the expressions until we // encounter a semicolon
	for !p.curTokenIs(monkeylexer.SEMICOLON) {
		p.nextToken()
	}
	return stmt
}
func (p *Parser) curTokenIs(t monkeylexer.TokenType) bool {
	return p.curToken.Type == t
}
func (p *Parser) peekTokenIs(t monkeylexer.TokenType) bool {
	return p.peekToken.Type == t
}
func (p *Parser) expectPeek(t monkeylexer.TokenType) bool {
	if p.peekTokenIs(t) {
		p.nextToken()
		return true
	} else {
		return false
	}
}

func (p *Parser) parseReturnStatement() *ast.ReturnStatement {
	stmt := &ast.ReturnStatement{Token: p.curToken}
	p.nextToken()
	// TODO: We're skipping the expressions until we // encounter a semicolon
	for !p.curTokenIs(monkeylexer.SEMICOLON) {
		p.nextToken()
	}
	return stmt
}

func (p *Parser) parseIntegerLiteral() ast.Expression {
	lit := &ast.IntegerLiteral{Token: p.curToken}
	value, err := strconv.ParseInt(p.curToken.Literal, 0, 64)
	if err != nil {
		msg := fmt.Sprintf("could not parse %q as integer", p.curToken.Literal)
		p.errors = append(p.errors, msg)
		return nil
	}
	lit.Value = value
	return lit
}

var precedences = map[monkeylexer.TokenType]int{

	monkeylexer.EQ:       EQUALS,
	monkeylexer.NOT_EQ:   EQUALS,
	monkeylexer.LT:       LESSGREATER,
	monkeylexer.GT:       LESSGREATER,
	monkeylexer.PLUS:     SUM,
	monkeylexer.MINUS:    SUM,
	monkeylexer.SLASH:    PRODUCT,
	monkeylexer.ASTERISK: PRODUCT,
}

func (p *Parser) peekPrecedence() int {
	if p, ok := precedences[p.peekToken.Type]; ok {
		return p
	}
	return LOWEST
}
func (p *Parser) curPrecedence() int {
	if p, ok := precedences[p.curToken.Type]; ok {
		return p
	}
	return LOWEST
}

func (p *Parser) parseInfixExpression(left ast.Expression) ast.Expression {
	expression := &ast.InfixExpression{
		Token:    p.curToken,
		Operator: p.curToken.Literal,
		Left:     left,
	}
	precedence := p.curPrecedence()
	p.nextToken()
	expression.Right = p.parseExpression(precedence)
	return expression
}
