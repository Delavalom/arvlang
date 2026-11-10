package ast

import (
	"strings"

	"github.com/delavalom/arvlang/arv/token"
)

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
// until they have all finished, and one failure cancels the rest. Options
// are the (name: value, ...) list after scope, in source order.
type ScopeExpression struct {
	Token   token.Token // the 'scope' token
	Options []ScopeOption
	Body    *BlockStatement
}

// ScopeOption is one name: value pair, such as timeout_ms: 2000.
type ScopeOption struct {
	Token token.Token // the name's token
	Name  string
	Value Expression
}

func (sc *ScopeExpression) expressionNode()      {}
func (sc *ScopeExpression) TokenLiteral() string { return sc.Token.Literal }
func (sc *ScopeExpression) String() string {
	out := "scope "
	if sc.Options != nil {
		opts := make([]string, len(sc.Options))
		for i, o := range sc.Options {
			opts[i] = o.Name + ": " + o.Value.String()
		}
		out += "(" + strings.Join(opts, ", ") + ") "
	}
	return out + "{ " + sc.Body.String() + " }"
}
