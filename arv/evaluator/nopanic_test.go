package evaluator

import (
	"context"
	"io"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// Integer division by zero is an Arv error, at the root and in a task alike.
func TestDivisionByZeroIsAnArvError(t *testing.T) {
	for _, src := range []string{`1 / 0`, `let f = fn() { 1 / 0 }; await spawn f()`} {
		got, _ := runWith(t, src)
		e, ok := got.(*value.Error)
		if !ok || e.Message != "division by zero" {
			t.Errorf("%s = %v, want the Arv error \"division by zero\"", src, got)
		}
	}
}

// Nothing evaluates to a Go nil: an empty block, a block ending in let, an
// empty program and an if without else all give the NIL singleton, which
// every builtin can print.
func TestNoGoNilValues(t *testing.T) {
	for _, src := range []string{
		``,
		`let x = 1;`,
		`let f = fn() {}; f()`,
		`let f = fn() { let x = 1; }; f()`,
		`if (true) {}`,
		`let f = fn() {}; await spawn f()`,
		`scope {}`,
	} {
		got, _ := runWith(t, src)
		if got != NIL {
			t.Errorf("%q = %#v, want NIL", src, got)
		}
	}
	for _, src := range []string{`let f = fn() {}; puts(f())`, `let f = fn() {}; puts(await spawn f())`} {
		if got, out := runWith(t, src); got != NIL || out != "nil\n" {
			t.Errorf("%s = %v, printed %q; want NIL and \"nil\\n\"", src, got, out)
		}
	}
}

// Audit: every builtin, given NIL, wrong types or no arguments, returns a
// value (usually an error) instead of panicking or returning Go nil.
func TestBuiltinsNeverPanicOrReturnNil(t *testing.T) {
	in := New(rootTask(t))
	argSets := [][]value.Object{
		nil,
		{NIL},
		{NIL, NIL},
		{&value.Integer{Value: -1}},
		{&value.Array{}},
		{&value.String{Value: ""}, NIL},
	}
	for name, b := range builtins {
		for _, args := range argSets {
			if got := in.applyFunction(b, args); got == nil {
				t.Errorf("%s(%v) returned Go nil", name, args)
			}
		}
	}
}

func rootTask(t *testing.T) *runtime.Task {
	t.Helper()
	return runtime.NewRoot(context.Background(), runtime.NewCapSet(), io.Discard)
}
