package repl

import (
	"bytes"
	"strings"
	"testing"
)

func session(lines ...string) string {
	var out bytes.Buffer
	Start(strings.NewReader(strings.Join(lines, "\n")+"\n"), &out)
	return out.String()
}

// The session is one root task: a handle spawned on one line can be
// awaited on a later one (only its spawner may await it).
func TestHandlesSurviveAcrossLines(t *testing.T) {
	got := session(`let f = fn() { 42 };`, `let t = spawn f();`, `await t`, `await t + 1`)
	if want := ">> >> >> 42\n>> 43\n>> "; got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

// As before Eval returned NIL for them, let lines and empty lines print
// nothing; other nil results still print "nil". puts writes to the
// session's output.
func TestReplPrintsLikeBefore(t *testing.T) {
	got := session(`let x = 1;`, ``, `if (false) { 1 }`, `puts("hi")`, `let y = 1 / 0;`, `x`)
	want := ">> >> >> nil\n>> hi\nnil\n>> ERROR: division by zero\n>> 1\n>> "
	if got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}
