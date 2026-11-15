package value

import (
	"context"
	"fmt"
	"io"
	"sync"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
)

// An environment holds its first envInline names in place and the rest
// in a map (Phase 8.4). Every name means the same wherever it is kept:
// the latest binding for its owner, the one before the spawn for a child
// spawned in between (I8), and a rebinding with no spawn in between
// replaces the value instead of keeping the old one.
func TestEnvironmentSlotsAndMapMeanTheSame(t *testing.T) {
	root := runtime.NewRoot(context.Background(), runtime.NewCapSet(), io.Discard)
	env := NewEnvironment()
	const n = 3 * envInline
	for i := 0; i < n; i++ {
		env.Set(fmt.Sprint("v", i), &Integer{Value: int64(i)})
	}
	child := root.Child(context.Background(), runtime.NewCapSet())
	for i := 0; i < n; i++ {
		env.Set(fmt.Sprint("v", i), &Integer{Value: int64(100 + i)})
		env.Set(fmt.Sprint("v", i), &Integer{Value: int64(200 + i)}) // no spawn in between: replaces
		env.Set(fmt.Sprint("late", i), &Integer{Value: 1})
	}
	for i := 0; i < n; i++ {
		name := fmt.Sprint("v", i)
		if got := get(t, env, name, child); got != fmt.Sprint(i) {
			t.Errorf("child sees %s = %s, want %d", name, got, i)
		}
		if got := get(t, env, name, root); got != fmt.Sprint(200+i) {
			t.Errorf("root sees %s = %s, want %d", name, got, 200+i)
		}
		if got := get(t, env, fmt.Sprint("late", i), child); got != "<unbound>" {
			t.Errorf("child sees late%d = %s, want it unbound", i, got)
		}
		if b := env.latest(name); b.prev == nil || b.prev.prev != nil {
			t.Errorf("%s keeps %v, want exactly the binding from before the spawn", name, b.prev)
		}
	}
	if got := get(t, NewEnclosedEnvironment(env), "v0", nil); got != "200" {
		t.Errorf("an enclosed environment sees v0 = %s, want 200", got)
	}
}

// Tasks spawned from an environment read it while its owner rebinds the
// same names, in and past the slots, under the race detector: access
// stays locked, and each reader keeps seeing what was bound before its
// spawn.
func TestEnvironmentSlotsAreLocked(t *testing.T) {
	root := runtime.NewRoot(context.Background(), runtime.NewCapSet(), io.Discard)
	env := NewEnvironment()
	const names = 2 * envInline
	for i := 0; i < names; i++ {
		env.Set(fmt.Sprint("v", i), &Integer{Value: int64(i)})
	}
	var wg sync.WaitGroup
	for r := 0; r < 4; r++ {
		child := root.Child(context.Background(), runtime.NewCapSet())
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 2000; i++ {
				if v, ok := env.GetAs(fmt.Sprint("v", i%names), child); !ok || v.(*Integer).Value != int64(i%names) {
					t.Errorf("child read v%d = %v", i%names, v)
					return
				}
			}
		}()
	}
	for i := 0; i < 2000; i++ {
		env.Set(fmt.Sprint("v", i%names), &Integer{Value: int64(1000 + i)})
	}
	wg.Wait()
}
