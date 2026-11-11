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
// count or a slot count would overflow. A restart waits at most
// runtime.MaxBackoff, and runs at most maxRetries more times.
const (
	maxTimeoutMS = math.MaxInt64 / int64(time.Millisecond)
	maxAllocMB   = math.MaxInt64 >> 20
	maxTasks     = math.MaxInt32
	maxRetries   = 1000
	maxBackoffMS = int64(runtime.MaxBackoff / time.Millisecond)
)

// integerOptions are the options whose value is a positive INTEGER, with
// the largest each accepts.
var integerOptions = map[string]int64{
	"timeout_ms": maxTimeoutMS, "tasks": maxTasks, "steps": math.MaxInt64, "alloc_mb": maxAllocMB,
	"retries": maxRetries, "backoff_ms": maxBackoffMS,
}

// restartOnly are the options that configure supervise: "restart".
var restartOnly = []string{"retries", "backoff_ms", "retry_on"}

// scopeOptions evaluates a scope's options in env, in order, and turns
// them into limits and a supervision policy. Each option may appear once;
// an unknown name is an error, so a typo never silently means "no limit".
// Budgets and retries/backoff_ms are positive INTEGERs; supervise is
// "fail_fast" (the default), "collect" or "restart"; retry_on is an ARRAY
// of the kinds a restart retries, "effect" (the default) and "user".
func (in *Interpreter) scopeOptions(opts []ast.ScopeOption, env *value.Environment) (runtime.Limits, runtime.Policy, *value.Error) {
	var l runtime.Limits
	var p runtime.Policy
	restart := runtime.RestartPolicy()
	seen := map[string]ast.ScopeOption{}
	for _, o := range opts {
		max, isInt := integerOptions[o.Name]
		if !isInt && o.Name != "supervise" && o.Name != "retry_on" {
			return l, p, optionError(o, "unknown scope option %q", o.Name)
		}
		if _, dup := seen[o.Name]; dup {
			return l, p, optionError(o, "duplicate scope option %q", o.Name)
		}
		seen[o.Name] = o
		v := in.Eval(o.Value, env)
		if e, ok := v.(*value.Error); ok {
			return l, p, e
		}
		var errv *value.Error
		switch o.Name {
		case "supervise":
			p.Strategy, errv = strategy(o, v)
		case "retry_on":
			restart.RetryOn, errv = retryOn(o, v)
		default:
			var n int64
			if n, errv = positive(o, v, max); errv != nil {
				break
			}
			switch o.Name {
			case "timeout_ms":
				l.Timeout = time.Duration(n) * time.Millisecond
			case "tasks":
				l.Tasks = int(n)
			case "steps":
				l.Steps = n
			case "alloc_mb":
				l.AllocBytes = n << 20
			case "retries":
				restart.Retries = int(n)
			case "backoff_ms":
				restart.Backoff = time.Duration(n) * time.Millisecond
			}
		}
		if errv != nil {
			return l, p, errv
		}
	}
	if p.Strategy != runtime.Restart {
		for _, name := range restartOnly {
			if o, ok := seen[name]; ok {
				return l, p, optionError(o, "scope option %s needs supervise: \"restart\"", name)
			}
		}
		return l, p, nil
	}
	return l, restart, nil
}

// positive is v as a positive INTEGER no larger than max.
func positive(o ast.ScopeOption, v value.Object, max int64) (int64, *value.Error) {
	n, ok := v.(*value.Integer)
	if !ok {
		return 0, optionError(o, "scope option %s: want a positive INTEGER, got %s", o.Name, v.Type())
	}
	if n.Value <= 0 {
		return 0, optionError(o, "scope option %s: want a positive INTEGER, got %d", o.Name, n.Value)
	}
	if n.Value > max {
		return 0, optionError(o, "scope option %s: %d is too large (max %d)", o.Name, n.Value, max)
	}
	return n.Value, nil
}

var strategies = map[string]runtime.Strategy{
	"fail_fast": runtime.FailFast, "collect": runtime.Collect, "restart": runtime.Restart,
}

// strategy is v as a supervise option.
func strategy(o ast.ScopeOption, v value.Object) (runtime.Strategy, *value.Error) {
	if s, ok := v.(*value.String); ok {
		if st, ok := strategies[s.Value]; ok {
			return st, nil
		}
	}
	return 0, optionError(o, `scope option supervise: want "fail_fast", "collect" or "restart", got %s`, quoteOption(v))
}

// retryOn is v as a retry_on option: a non-empty ARRAY of the kinds a
// restart may retry. The others fail the same way every time (capability,
// budget, runtime) or must stop the task (cancelled).
func retryOn(o ast.ScopeOption, v value.Object) (map[string]bool, *value.Error) {
	arr, ok := v.(*value.Array)
	if !ok || len(arr.Elements) == 0 {
		return nil, optionError(o, `scope option retry_on: want an ARRAY of error kinds, such as ["effect", "user"], got %s`, quoteOption(v))
	}
	kinds := map[string]bool{}
	for _, el := range arr.Elements {
		s, ok := el.(*value.String)
		if !ok || (s.Value != runtime.KindEffect && s.Value != runtime.KindUser) {
			return nil, optionError(o, `scope option retry_on: %s is not a kind a restart retries (want "effect" or "user")`, quoteOption(el))
		}
		kinds[s.Value] = true
	}
	return kinds, nil
}

// quoteOption names v in an option error: a short string quoted, else its
// type, so an error never copies a long value.
func quoteOption(v value.Object) string {
	if s, ok := v.(*value.String); ok && len(s.Value) <= 32 {
		return fmt.Sprintf("%q", s.Value)
	}
	return string(v.Type())
}

// optionError is an error positioned at the option's name.
func optionError(o ast.ScopeOption, format string, a ...interface{}) *value.Error {
	e := newError(format, a...)
	e.Pos = fmt.Sprintf("%d:%d", o.Token.Line, o.Token.Col)
	return e
}
