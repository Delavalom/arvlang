package ast

import "reflect"

// Size is how many nodes root's tree has: root and every node under it
// that is evaluated, as arv/check's walk sees them (the names a node binds
// are not counted). The parser builds sugar (a field, &&, ||, an
// interpolation) as the nodes of its expansion, so a sugared form and its
// hand-written expansion have the same size. A node of a type Size does
// not know counts as one, with no children. The walk keeps its own stack:
// a 100000-term infix chain is as deep as a tree gets.
func Size(root Node) int {
	n := 0
	stack := []Node{root}
	push := func(c Node) { stack = append(stack, c) }
	for len(stack) > 0 {
		top := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if top == nil {
			continue
		}
		if v := reflect.ValueOf(top); v.Kind() == reflect.Pointer && v.IsNil() {
			continue // a missing child: an if without else
		}
		n++
		switch x := top.(type) {
		case *Program:
			for _, s := range x.Statements {
				push(s)
			}
		case *BlockStatement:
			for _, s := range x.Statements {
				push(s)
			}
		case *ExpressionStatement:
			push(x.Expression)
		case *LetStatement:
			push(x.Value)
		case *ReturnStatement:
			push(x.ReturnValue)
		case *PrefixExpression:
			push(x.Right)
		case *InfixExpression:
			push(x.Left)
			push(x.Right)
		case *IfExpression:
			push(x.Condition)
			push(x.Consequence)
			push(x.Alternative)
		case *FunctionLiteral:
			push(x.Body)
		case *CallExpression:
			push(x.Function)
			for _, a := range x.Arguments {
				push(a)
			}
		case *ArrayLiteral:
			for _, e := range x.Elements {
				push(e)
			}
		case *IndexExpression:
			push(x.Left)
			push(x.Index)
		case *HashLiteral:
			for k, v := range x.Pairs { // Keys holds these keys again
				push(k)
				push(v)
			}
		case *ScopeExpression:
			for _, o := range x.Options {
				push(o.Value)
			}
			push(x.Body)
		case *SpawnExpression:
			push(x.Call)
			push(x.Caps)
		case *AwaitExpression:
			push(x.Task)
		case *ForExpression:
			push(x.Iterable)
			push(x.Body)
		case *MatchExpression:
			push(x.Subject)
			push(x.Ok)
			push(x.Err)
		}
	}
	return n
}
