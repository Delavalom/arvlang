package evaluator

import (
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// A task's error crosses into the scope that ran it with its kind, its
// position and the ID of the task it arose in.
func TestChildErrorKeepsKindPosAndTask(t *testing.T) {
	got := runLimited(t, "let f = fn() {\n  scope (timeout_ms: 10) { sleep(5000) } };\nlet t = spawn f();\nawait t", runtime.Limits{})
	e := wantKind(t, got, runtime.KindBudget)
	if e.Pos != "2:28" || e.Task != "root/0" {
		t.Fatalf("pos %q task %q, want 2:28 in root/0", e.Pos, e.Task)
	}
	// No Arv node runs in spawn f() with [...]: placed at the spawn.
	e = wantKind(t, runLimited(t, "let f = fn() { 1 };\n spawn f() with [env(\"ARV_KIND_X\")]", runtime.Limits{}), runtime.KindCapability)
	if e.Pos != "2:2" || e.Task != "root" {
		t.Fatalf("pos %q task %q, want 2:2 in root", e.Pos, e.Task)
	}
	// The root's own errors name the root.
	e = wantKind(t, runLimited(t, `1 / 0`, runtime.Limits{}), runtime.KindRuntime)
	if e.Task != "root" {
		t.Fatalf("task %q, want root", e.Task)
	}
}

// at fills only what an error lacks, and keeps its kind.
func TestAtKeepsKind(t *testing.T) {
	node := parse(t, "\n  x").Statements[0]
	e := at(&value.Error{Message: "m", Kind: runtime.KindEffect}, node, "root/3")
	if e.Kind != runtime.KindEffect || e.Pos != "2:3" || e.Task != "root/3" {
		t.Fatalf("got %+v", *e)
	}
	placed := &value.Error{Message: "m", Pos: "9:9", Task: "root/1", Kind: runtime.KindUser}
	if got := at(placed, node, "root/3"); got != placed {
		t.Fatalf("at copied an error that lacked nothing: %+v", *got)
	}
}
