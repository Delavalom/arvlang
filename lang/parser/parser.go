package parser

import (
	"github.com/delavalom/arvlang/lang/ast"
	"github.com/delavalom/arvlang/lang/lexer"
	"github.com/delavalom/arvlang/lang/tokens"
)

type prefixFn func() ast.Node
type infixFn func(ast.Node) ast.Node
type postfixFn func(ast.Node) ast.Node

type Parser struct {
	lexer      *lexer.Lexer
	root       ast.Node
	prefixFns  map[tokens.Type]prefixFn
	infixFns   map[tokens.Type]infixFn
	postfixFns map[tokens.Type]postfixFn
	errors     []string

	// for and if conditions
	inCondition bool
	inPipeLoop  bool
	inMetaDef   bool

	// function content control
	hasReturn bool
	hasYield  bool
}

func NewParser() *Parser {
	p := &Parser{
		lexer:      nil,
		root:       nil,
		prefixFns:  map[tokens.Type]prefixFn{},
		infixFns:   map[tokens.Type]infixFn{},
		postfixFns: map[tokens.Type]postfixFn{},
		errors:     []string{},
	}

	return p
}

func Parse(input any) (ast.Node, error) {
	NewParser()
	return input.(ast.Node), nil
}
