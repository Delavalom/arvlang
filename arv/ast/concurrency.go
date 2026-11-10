package ast

import "github.com/delavalom/arvlang/arv/token"

// SpawnExpression starts Call on a new task: spawn f(x) with [net("h")].
// Caps is nil when there is no with clause; the child then holds nothing.
type SpawnExpression struct {
	Token token.Token // the 'spawn' token
	Call  *CallExpression
	Caps  Expression
}

func (se *SpawnExpression) expressionNode()      {}
func (se *SpawnExpression) TokenLiteral() string { return se.Token.Literal }
func (se *SpawnExpression) String() string {
	out := "spawn " + se.Call.String()
	if se.Caps != nil {
		out += " with " + se.Caps.String()
	}
	return out
}

// AwaitExpression waits for a task and yields its result: await t.
type AwaitExpression struct {
	Token token.Token // the 'await' token
	Task  Expression
}

func (ae *AwaitExpression) expressionNode()      {}
func (ae *AwaitExpression) TokenLiteral() string { return ae.Token.Literal }
func (ae *AwaitExpression) String() string       { return "await " + ae.Task.String() }

// ScopeExpression owns every task spawned inside Body. It does not finish
// until they have all finished, and one failure cancels the rest.
type ScopeExpression struct {
	Token token.Token // the 'scope' token
	Body  *BlockStatement
}

func (sc *ScopeExpression) expressionNode()      {}
func (sc *ScopeExpression) TokenLiteral() string { return sc.Token.Literal }
func (sc *ScopeExpression) String() string       { return "scope { " + sc.Body.String() + " }" }
