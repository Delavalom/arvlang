package check

import (
	"fmt"
	"reflect"

	"github.com/delavalom/arvlang/arv/ast"
)

// walk calls f on root and every node below it, parents before children,
// with an explicit stack: a 100000-term infix chain is as deep as the tree
// gets, and it must not cost a Go frame per level. f returns false to skip
// a node's children. A node of a type children does not know stops the
// walk with an error, so syntax added later can never be skipped silently.
func walk(root ast.Node, f func(ast.Node) bool) error {
	stack := []ast.Node{root}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if isNil(n) || !f(n) {
			continue
		}
		if err := children(n, func(c ast.Node) { stack = append(stack, c) }); err != nil {
			return err
		}
	}
	return nil
}

// isNil reports a missing child: a nil interface, or a nil pointer such as
// an if without else.
func isNil(n ast.Node) bool {
	if n == nil {
		return true
	}
	v := reflect.ValueOf(n)
	return v.Kind() == reflect.Pointer && v.IsNil()
}

// children calls add on each child of n that is evaluated. Names that a
// node binds (a let's name, a function's parameters, a for's variable,
// match's ok and err names) are not children: they are not references.
func children(n ast.Node, add func(ast.Node)) error {
	switch n := n.(type) {
	case *ast.Program:
		for _, s := range n.Statements {
			add(s)
		}
	case *ast.BlockStatement:
		for _, s := range n.Statements {
			add(s)
		}
	case *ast.ExpressionStatement:
		add(n.Expression)
	case *ast.LetStatement:
		add(n.Value)
	case *ast.ReturnStatement:
		add(n.ReturnValue)
	case *ast.Identifier, *ast.IntegerLiteral, *ast.StringLiteral, *ast.Boolean:
	case *ast.PrefixExpression:
		add(n.Right)
	case *ast.InfixExpression:
		add(n.Left)
		add(n.Right)
	case *ast.IfExpression:
		add(n.Condition)
		add(n.Consequence)
		add(n.Alternative)
	case *ast.FunctionLiteral:
		add(n.Body)
	case *ast.CallExpression:
		add(n.Function)
		for _, a := range n.Arguments {
			add(a)
		}
	case *ast.ArrayLiteral:
		for _, e := range n.Elements {
			add(e)
		}
	case *ast.IndexExpression:
		add(n.Left)
		add(n.Index)
	case *ast.HashLiteral:
		for k, v := range n.Pairs { // Keys holds these keys again, in source order
			add(k)
			add(v)
		}
	case *ast.ScopeExpression:
		for _, o := range n.Options {
			add(o.Value)
		}
		add(n.Body)
	case *ast.SpawnExpression:
		add(n.Call)
		add(n.Caps)
	case *ast.AwaitExpression:
		add(n.Task)
	case *ast.ForExpression:
		add(n.Iterable)
		add(n.Body)
	case *ast.MatchExpression:
		add(n.Subject)
		add(n.Ok)
		add(n.Err)
	default:
		line, col := ast.Pos(n)
		return fmt.Errorf("arv check: syntax it does not know, %T at %d:%d", n, line, col)
	}
	return nil
}
