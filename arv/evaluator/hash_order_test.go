package evaluator

import (
	"bytes"
	"context"
	"testing"

	"github.com/delavalom/arvlang/arv/lexer"
	"github.com/delavalom/arvlang/arv/parser"
	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// runOut runs src in a root task with no grants and returns what it
// printed and its result.
func runOut(t *testing.T, src string) (string, value.Object) {
	t.Helper()
	p := parser.New(lexer.New(src))
	program := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parse: %v", p.Errors())
	}
	var out bytes.Buffer
	root := runtime.NewRoot(context.Background(), runtime.NewCapSet(), &out)
	res := New(root).Run(program, value.NewEnvironment())
	return out.String(), res
}

// I7: a hash literal's pairs are evaluated in source order, so the
// effects in them happen in the same order on every run, and the hash
// prints in that order.
func TestHashLiteralEvaluatesInSourceOrder(t *testing.T) {
	src := `let h = {"z": puts("1"), "y": puts("2"), "x": puts("3"), "w": puts("4"), "v": puts("5")};
puts({"zeta": 1, "alpha": 2, "mid": 3, "beta": 4});
puts(json({"b": 1, "a": 2}))`
	want := "1\n2\n3\n4\n5\n{zeta: 1, alpha: 2, mid: 3, beta: 4}\n{\"a\":2,\"b\":1}\n"
	for i := 0; i < 100; i++ {
		if got, res := runOut(t, src); got != want {
			t.Fatalf("run %d: printed %q (result %s), want %q", i, got, res.Inspect(), want)
		}
	}
}

// json names the first key in order that is not a string, not a random one.
func TestJSONBadKeyIsTheFirst(t *testing.T) {
	for i := 0; i < 50; i++ {
		_, res := runOut(t, `json({"a": 1, 2: 2, true: 3})`)
		if e, ok := res.(*value.Error); !ok || e.Message != "json: hash keys must be STRING, got INTEGER" {
			t.Fatalf("run %d: %s", i, res.Inspect())
		}
	}
}
