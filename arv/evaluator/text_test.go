package evaluator

import (
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
)

func TestSplit(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{`split("a,b,c", ",")`, `[a, b, c]`},
		{`split("a,b,", ",")`, `[a, b, ]`},
		{`split("", ",")`, `[]`},
		{`split("abc", ",")`, `[abc]`},
		{`split("a::b", "::")`, `[a, b]`},
		{`split("http://127.0.0.1:8080/t/3", "/")[2]`, `127.0.0.1:8080`},
		{`len(split("a,b,c", ","))`, `3`},
	} {
		if got := testEval(tc.in); got.Inspect() != tc.want {
			t.Errorf("%s = %s, want %s", tc.in, got.Inspect(), tc.want)
		}
	}
}

func TestLines(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		// Arv string literals have no escapes; Go puts the real
		// newlines, tabs and carriage returns into the source.
		{"lines(\"a\nb\n\")", `[a, b]`},
		{"lines(\"  a  \r\n\n \t\nb\")", `[a, b]`},
		{`lines("")`, `[]`},
		{"lines(\"\n\n\")", `[]`},
		{"len(lines(\"x\ny\nz\n\"))", `3`},
	} {
		if got := testEval(tc.in); got.Inspect() != tc.want {
			t.Errorf("%s = %s, want %s", tc.in, got.Inspect(), tc.want)
		}
	}
}

func TestSplitAndLinesErrors(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{`split("a")`, `split: want 2 arguments, got 1`},
		{`split("a", 1)`, `split: want (STRING, STRING), got (STRING, INTEGER)`},
		{`split("a", "")`, `split: separator is empty`},
		{`lines(1)`, `lines: argument must be STRING, got INTEGER`},
		{`lines()`, `lines: want 1 argument, got 0`},
	} {
		got := testEval(tc.in)
		if !isError(got) || got.Inspect() != "ERROR: "+tc.want {
			t.Errorf("%s = %s, want error %q", tc.in, got.Inspect(), tc.want)
		}
	}
}

// Each piece costs its slot and a String header (pieceSize); the bytes
// are shared with the input. split charges before it builds, so a limit
// one byte short refuses the whole call.
func TestSplitAndLinesCharge(t *testing.T) {
	for _, tc := range []struct {
		src  string
		cost int64
	}{
		{`len(split("a,b,c", ","))`, 3 * pieceSize},
		// lines charges the most lines the text can have: 3 newlines.
		{"len(lines(\"a\n\nb\n\"))", 4 * pieceSize},
	} {
		if got := runCapsIn(t, tc.cost, runtime.NewCapSet(), tc.src); isError(got) {
			t.Errorf("%s under %d bytes: %s", tc.src, tc.cost, got.Inspect())
		}
		got := runCapsIn(t, tc.cost-1, runtime.NewCapSet(), tc.src)
		if !isError(got) || got.(interface{ ErrKind() string }).ErrKind() != runtime.KindBudget {
			t.Errorf("%s under %d bytes: got %s, want a budget error", tc.src, tc.cost-1, got.Inspect())
		}
	}
}
