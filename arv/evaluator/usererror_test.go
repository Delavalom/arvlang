package evaluator

import (
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
)

// error(msg) fails with msg, as a user error, positioned where it is called.
func TestUserError(t *testing.T) {
	got, _ := runWith(t, "let f = fn() {\n  error(\"boom\") };\nf()")
	e := wantKind(t, got, runtime.KindUser)
	if e.Message != "boom" || e.Pos != "2:3" || e.Task != "root" {
		t.Fatalf("got %+v", *e)
	}
	// It stops the block like any error: nothing after it runs.
	_, out := runWith(t, `error("stop"); puts("after")`)
	if out != "" {
		t.Fatalf("printed %q after error()", out)
	}
	// From a task, through await, it is still a user error.
	got, _ = runWith(t, `let f = fn() { error("in task") }; let t = spawn f(); await t`)
	if e := wantKind(t, got, runtime.KindUser); e.Message != "in task" || e.Task != "root/0" {
		t.Fatalf("got %+v", *e)
	}
}

func TestUserErrorBadArguments(t *testing.T) {
	for src, msg := range map[string]string{
		`error()`:         "error: want 1 argument, got 0",
		`error("a", "b")`: "error: want 1 argument, got 2",
		`error(1)`:        "error: message must be STRING, got INTEGER",
	} {
		got, _ := runWith(t, src)
		if e := wantKind(t, got, runtime.KindRuntime); e.Message != msg {
			t.Errorf("%s: got %q, want %q", src, e.Message, msg)
		}
	}
}

// Strings compare with == and !=, so a program can test an error's kind.
func TestStringEquality(t *testing.T) {
	for src, want := range map[string]string{
		`"effect" == "effect"`: "true",
		`"effect" == "user"`:   "false",
		`"effect" != "user"`:   "true",
		`"a" != "a"`:           "false",
		`"a" + "b" == "ab"`:    "true",
	} {
		if got, _ := runWith(t, src); got.Inspect() != want {
			t.Errorf("%s = %s, want %s", src, got.Inspect(), want)
		}
	}
}
