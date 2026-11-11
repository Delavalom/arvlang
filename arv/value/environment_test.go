package value

import (
	"context"
	"io"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
)

func get(t *testing.T, e *Environment, name string, as *runtime.Task) string {
	t.Helper()
	v, ok := e.GetAs(name, as)
	if !ok {
		return "<unbound>"
	}
	return v.Inspect()
}

// A child sees each environment it can reach as it was when it was
// spawned; its parent sees the latest bindings (I8).
func TestEnvironmentFrozenAtSpawn(t *testing.T) {
	root := runtime.NewRoot(context.Background(), runtime.NewCapSet(), io.Discard)
	env := NewEnvironment()
	env.Set("x", &Integer{Value: 1})
	child := root.Child(context.Background(), runtime.NewCapSet())
	env.Set("x", &Integer{Value: 2})
	env.Set("late", &Integer{Value: 3})

	if got := get(t, env, "x", child); got != "1" {
		t.Errorf("child sees x = %s, want 1", got)
	}
	if got := get(t, env, "late", child); got != "<unbound>" {
		t.Errorf("child sees late = %s, want it unbound", got)
	}
	if got := get(t, env, "x", root); got != "2" {
		t.Errorf("root sees x = %s, want 2", got)
	}
	if v, _ := env.Get("x"); v.Inspect() != "2" {
		t.Errorf("Get(x) = %s, want 2", v.Inspect())
	}

	// An environment the child creates (a call's) is its own: it sees its
	// own rebinding, and the frozen one through it.
	own := NewEnclosedEnvironment(env)
	own.Set("y", &Integer{Value: 4})
	own.Set("y", &Integer{Value: 5})
	if got := get(t, own, "y", child); got != "5" {
		t.Errorf("child sees its own y = %s, want 5", got)
	}
	if got := get(t, own, "x", child); got != "1" {
		t.Errorf("child sees x through its own env = %s, want 1", got)
	}
}

// With no spawn in between, a rebinding replaces the old value instead of
// keeping it.
func TestEnvironmentRebindWithoutSpawnReplaces(t *testing.T) {
	env := NewEnvironment()
	env.Set("x", &Integer{Value: 1})
	env.Set("x", &Integer{Value: 2})
	if b := env.store["x"]; b.prev != nil {
		t.Fatalf("an old binding was kept with no spawn in between")
	}
}
