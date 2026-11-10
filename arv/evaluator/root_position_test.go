package evaluator

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/delavalom/arvlang/arv/lexer"
	"github.com/delavalom/arvlang/arv/parser"
	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// runLimited runs input as a whole program under l, as arv.Run does.
func runLimited(t *testing.T, input string, l runtime.Limits) value.Object {
	t.Helper()
	return runLimitedEnv(t, input, nil, l)
}

// runLimitedEnv is runLimited with binds as host builtins.
func runLimitedEnv(t *testing.T, input string, binds map[string]value.BuiltinFunction, l runtime.Limits) value.Object {
	t.Helper()
	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}
	env := value.NewEnvironment()
	for name, fn := range binds {
		env.Set(name, &value.Builtin{Fn: fn})
	}
	root := runtime.NewRoot(context.Background(), runtime.NewCapSet(), io.Discard)
	return New(root).RunLimited(program, env, l)
}

// The root scope has no enclosing node to stamp its errors, so an error
// the root scope raises itself (the last partial steps charge, or a
// timeout once the body has returned) is placed at the program's last
// statement, where the run ended.
func TestRootBudgetErrorsHaveAPosition(t *testing.T) {
	// 4 statements of a few nodes each: fewer than one 256-node chunk, so
	// only the charge when the root scope closes can pass the limit.
	got := runLimited(t, "let a = 1;\nlet b = 2;\nlet c = a + b;\nc", runtime.Limits{Steps: 10})
	wantErrorAt(t, got, "budget exceeded: steps (limit 10)", "4:1")

	// The body has returned 1; the root times out waiting for its task.
	got = runLimited(t, "let t = spawn sleep(5000);\n1", runtime.Limits{Timeout: 100 * time.Millisecond})
	wantErrorAt(t, got, "budget exceeded: timeout (limit 100 ms)", "2:1")
}
