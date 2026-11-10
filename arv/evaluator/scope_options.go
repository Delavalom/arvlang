package evaluator

import (
	"fmt"
	"math"
	"time"

	"github.com/delavalom/arvlang/arv/ast"
	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// Largest values the scope options accept: beyond them a Duration, a byte
// count or a slot count would overflow.
const (
	maxTimeoutMS = math.MaxInt64 / int64(time.Millisecond)
	maxAllocMB   = math.MaxInt64 >> 20
	maxTasks     = math.MaxInt32
)

// scopeLimits evaluates a scope's options in env, in order, and turns them
// into limits. Each option may appear once and must be a positive INTEGER;
// an unknown name is an error, so a typo never silently means "no limit".
func (in *Interpreter) scopeLimits(opts []ast.ScopeOption, env *value.Environment) (runtime.Limits, *value.Error) {
	var l runtime.Limits
	seen := map[string]bool{}
	for _, o := range opts {
		var max int64
		switch o.Name {
		case "timeout_ms":
			max = maxTimeoutMS
		case "tasks":
			max = maxTasks
		case "steps":
			max = math.MaxInt64
		case "alloc_mb":
			max = maxAllocMB
		default:
			return l, optionError(o, "unknown scope option %q", o.Name)
		}
		if seen[o.Name] {
			return l, optionError(o, "duplicate scope option %q", o.Name)
		}
		seen[o.Name] = true
		v := in.Eval(o.Value, env)
		if e, ok := v.(*value.Error); ok {
			return l, e
		}
		n, ok := v.(*value.Integer)
		if !ok {
			return l, optionError(o, "scope option %s: want a positive INTEGER, got %s", o.Name, v.Type())
		}
		if n.Value <= 0 {
			return l, optionError(o, "scope option %s: want a positive INTEGER, got %d", o.Name, n.Value)
		}
		if n.Value > max {
			return l, optionError(o, "scope option %s: %d is too large (max %d)", o.Name, n.Value, max)
		}
		switch o.Name {
		case "timeout_ms":
			l.Timeout = time.Duration(n.Value) * time.Millisecond
		case "tasks":
			l.Tasks = int(n.Value)
		case "steps":
			l.Steps = n.Value
		case "alloc_mb":
			l.AllocBytes = n.Value << 20
		}
	}
	return l, nil
}

// optionError is an error positioned at the option's name.
func optionError(o ast.ScopeOption, format string, a ...interface{}) *value.Error {
	return &value.Error{Message: fmt.Sprintf(format, a...), Pos: fmt.Sprintf("%d:%d", o.Token.Line, o.Token.Col)}
}
