package evaluator

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// A collect scope whose opening steps charge fails never ran, but it was
// already registered to keep its tasks: it must still be forgotten, or a
// program that catches the failure in a loop leaves one closed scope per
// iteration in the interpreter for the rest of the run (or REPL session).
func TestCollectScopeForgottenWhenItCannotOpen(t *testing.T) {
	xs := make([]string, 300)
	for i := range xs {
		xs[i] = "0"
	}
	root := runtime.NewRoot(context.Background(), runtime.NewCapSet(), &bytes.Buffer{})
	in := New(root)
	got := in.Run(parse(t, `let r = for i in [`+strings.Join(xs, ", ")+`] {
			match scope (steps: 1) { scope (supervise: "collect") { 1 } } { ok(v) => v, err(e) => 0 }
		}; len(r)`), value.NewEnvironment())
	if got.Inspect() != "300" {
		t.Fatalf("got %s, want 300", got.Inspect())
	}
	if n := len(in.collects); n != 0 {
		t.Fatalf("%d collect scopes still kept after the run, want 0", n)
	}
}
