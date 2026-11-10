package evaluator

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/lexer"
	"github.com/delavalom/arvlang/arv/parser"
	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// runWith evaluates input as the root task holding caps; it returns the
// result and everything puts printed.
func runWith(t *testing.T, input string, caps ...runtime.Capability) (value.Object, string) {
	t.Helper()
	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}
	var out bytes.Buffer
	root := runtime.NewRoot(context.Background(), runtime.NewCapSet(caps...), &out)
	return New(root).Run(program, value.NewEnvironment()), out.String()
}

func TestPutsWritesToTaskOutput(t *testing.T) {
	_, out := runWith(t, `puts("hi"); puts(1 + 1);`)
	if out != "hi\n2\n" {
		t.Fatalf("output = %q", out)
	}
}

func TestRunawayRecursionIsAnArvError(t *testing.T) {
	got, _ := runWith(t, `let f = fn(n) { f(n + 1) }; f(0);`)
	errObj, ok := got.(*value.Error)
	if !ok || !strings.Contains(errObj.Message, "stack overflow") {
		t.Fatalf("got %v, want stack overflow error", got)
	}
}

func TestWrongArgCountIsAnError(t *testing.T) {
	got, _ := runWith(t, `let f = fn(a, b) { a }; f(1);`)
	if _, ok := got.(*value.Error); !ok {
		t.Fatalf("got %v, want error", got)
	}
}

func TestLenOfArray(t *testing.T) {
	got, _ := runWith(t, `len([1, 2, 3])`)
	if got.Inspect() != "3" {
		t.Fatalf("got %s", got.Inspect())
	}
}
