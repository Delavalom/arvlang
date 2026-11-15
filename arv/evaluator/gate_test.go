package evaluator

import (
	"slices"
	"testing"

	"github.com/delavalom/arvlang/arv/ast"
)

// pureBuiltins are the builtins whose result depends only on their
// arguments. The capability constructors resolve a relative path against
// the working directory, which a log's header records (see Phase 6 notes).
// str and ast.StrBuiltin (what "${x}" calls) render a value as puts
// prints it.
var pureBuiltins = []string{"len", "last", "push", "error", "json", "split", "lines", "net", "fs_read", "fs_write", "env", "llm", "tool", "str", ast.StrBuiltin}

// gatedInside are the builtins that are no effect themselves but make
// effects through the gate under other names: ask and agent ask the host
// for the model (llm_model) and call it (llm), and an agent's tools are
// Arv functions whose effects are logged as any others.
var gatedInside = []string{"ask", "agent"}

// I7: every builtin is either pure or an effect that goes through the
// gate under its own name. A new builtin must be put in one class or the
// other here, so none reaches the world unlogged.
func TestEveryBuiltinIsPureOrAnEffect(t *testing.T) {
	effects := []string{"http_get", "read_file", "write_file", "getenv", "sleep", "clock_ms", "now", "random", "puts"}
	for name, b := range builtins {
		switch {
		case slices.Contains(pureBuiltins, name):
			if b.Effect != "" {
				t.Errorf("%s is listed as pure but is the effect %q", name, b.Effect)
			}
		case slices.Contains(effects, name):
			if b.Effect != name {
				t.Errorf("%s is an effect logged as %q", name, b.Effect)
			}
		case slices.Contains(gatedInside, name):
			if b.Effect != "" {
				t.Errorf("%s makes its effects inside, but is the effect %q", name, b.Effect)
			}
		default:
			t.Errorf("builtin %s is neither pure nor an effect: classify it", name)
		}
	}
	for _, name := range append(append(effects, pureBuiltins...), gatedInside...) {
		if builtins[name] == nil {
			t.Errorf("no builtin %s", name)
		}
	}
}

func TestNowAndRandom(t *testing.T) {
	for _, tt := range []struct{ src, want string }{
		{`now() > 1700000000000`, "true"},
		{`let r = random(3); if (r > -1) { r < 3 } else { false }`, "true"},
		{`random(1)`, "0"},
		{`random(0)`, "ERROR: random: want a positive INTEGER bound"},
		{`random("x")`, "ERROR: random: want a positive INTEGER bound"},
		{`now(1)`, "ERROR: now: want 0 arguments, got 1"},
	} {
		if _, got := runOut(t, tt.src); got.Inspect() != tt.want {
			t.Errorf("%s = %s, want %s", tt.src, got.Inspect(), tt.want)
		}
	}
}
