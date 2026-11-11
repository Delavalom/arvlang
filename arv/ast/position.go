package ast

// Pos returns where n starts in the source (line and column from 1), or
// 0, 0 for a node with no token, such as a Program. An infix, call or index
// expression starts where its leftmost operand does, so a + "s" and f(x)
// point at a and f, not at the operator or the parenthesis. Long infix
// chains are walked without recursion.
func Pos(n Node) (line, col int) {
	for {
		switch x := n.(type) {
		case *InfixExpression:
			n = x.Left
		case *CallExpression:
			n = x.Function
		case *IndexExpression:
			n = x.Left
		case *ExpressionStatement:
			return x.Token.Line, x.Token.Col
		case *LetStatement:
			return x.Token.Line, x.Token.Col
		case *ReturnStatement:
			return x.Token.Line, x.Token.Col
		case *BlockStatement:
			return x.Token.Line, x.Token.Col
		case *Identifier:
			return x.Token.Line, x.Token.Col
		case *IntegerLiteral:
			return x.Token.Line, x.Token.Col
		case *StringLiteral:
			return x.Token.Line, x.Token.Col
		case *Boolean:
			return x.Token.Line, x.Token.Col
		case *PrefixExpression:
			return x.Token.Line, x.Token.Col
		case *IfExpression:
			return x.Token.Line, x.Token.Col
		case *FunctionLiteral:
			return x.Token.Line, x.Token.Col
		case *ArrayLiteral:
			return x.Token.Line, x.Token.Col
		case *HashLiteral:
			return x.Token.Line, x.Token.Col
		case *ScopeExpression:
			return x.Token.Line, x.Token.Col
		case *SpawnExpression:
			return x.Token.Line, x.Token.Col
		case *AwaitExpression:
			return x.Token.Line, x.Token.Col
		case *ForExpression:
			return x.Token.Line, x.Token.Col
		case *MatchExpression:
			return x.Token.Line, x.Token.Col
		default: // a Program, nil, or an unknown node
			return 0, 0
		}
	}
}
