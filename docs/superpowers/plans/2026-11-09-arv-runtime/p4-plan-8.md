# Phase 4, task p4.8: I8 in the runtime and environments: freeze at spawn

> **For agentic workers:** this file is one task, p4.8. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: tasks p4.1 to p4.7 are committed in your tree. Start every shell command with `cd <your worktree> &&`. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify`; keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0` (toolchain 1.26.2), so do not use `testing/synctest` or `sync.WaitGroup.Go`.
>
> Every code block below was applied, in task order, to a scratch copy of the tree and passed `go vet ./...` and `go test -race ./...` (including `e2e/`). Design: `11-phase4-supervision.md`. Deviations from it, and why: `p4-plan-1.md`.

### Task p4.8: I8 in the runtime and environments: freeze at spawn

I8 says a task sees the environments it captured as they were at `spawn`. Copying every reachable environment at each spawn would cost time and memory proportional to everything in scope. Instead:
- A process-wide clock ticks once per spawn (`Task.Child`). Each task keeps its *horizon*: the spawn times on its path from the root, one per generation.
- A binding remembers the clock time it was made at, and keeps the binding it replaced (`binding{val, at, prev}`). `Set` overwrites in place when no spawn has happened since, so a loop that rebinds without spawning grows nothing.
- `GetAs(name, task)` reads each environment as of the task's horizon: in an environment created at time `born`, it skips bindings made at or after `task.Horizon(born)`, the first spawn on the task's path after that environment was created.

That is transitive (a grandchild sees what its parent saw) and costs O(1) per spawn. `Get` (no task) reads the newest binding, as before. A fresh binding measured 102 bytes including map growth, against 104 before. The evaluator starts using `GetAs` in p4.9.

**Files:**
- Create: `arv/runtime/horizon.go`
- Modify: `arv/runtime/task.go`
- Modify: `arv/value/environment.go`
- Create (test): `arv/runtime/horizon_test.go`
- Create (test): `arv/value/environment_test.go`

- [ ] **Step 1: Write the failing tests**

Create `arv/runtime/horizon_test.go`:

```go
package runtime

import (
	"context"
	"io"
	"math"
	"testing"
)

// A task's horizon for an environment is the spawn time of the first task
// on its path from the root spawned after the environment was created.
func TestHorizon(t *testing.T) {
	root := NewRoot(context.Background(), NewCapSet(), io.Discard)
	before := Now()
	if h := root.Horizon(before); h != math.MaxUint64 || root.HorizonLen() != 0 {
		t.Fatalf("root horizon = %d (len %d), want none", h, root.HorizonLen())
	}
	child := root.Child(context.Background(), NewCapSet())
	between := Now()
	grand := child.Child(context.Background(), NewCapSet())
	after := Now()
	if child.HorizonLen() != 1 || grand.HorizonLen() != 2 {
		t.Fatalf("horizon lengths %d, %d, want 1, 2", child.HorizonLen(), grand.HorizonLen())
	}
	// The child was spawned at between: what existed before is frozen then.
	if h := child.Horizon(before); h != between {
		t.Errorf("child.Horizon(before) = %d, want %d", h, between)
	}
	// The grandchild sees the root's old environment as the child did...
	if h := grand.Horizon(before); h != between {
		t.Errorf("grand.Horizon(before) = %d, want %d", h, between)
	}
	// ...the child's own environments as they were at its spawn...
	if h := grand.Horizon(between); h != after {
		t.Errorf("grand.Horizon(between) = %d, want %d", h, after)
	}
	// ...and its own without limit.
	if h := grand.Horizon(after); h != math.MaxUint64 {
		t.Errorf("grand.Horizon(after) = %d, want none", h)
	}
	if Now() != after {
		t.Errorf("the clock moved without a spawn")
	}
}
```

Create `arv/value/environment_test.go`:

```go
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
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd <worktree> && go test -race ./arv/runtime/ ./arv/value/ -run 'TestHorizon|TestEnvironment'`

Expected: FAIL. The build fails: (the first lines; ports and times vary)

```text
arv/value/environment_test.go:13:13: e.GetAs undefined (type *Environment has no field or method GetAs)
arv/value/environment_test.go:62:28: b.prev undefined (type Object has no field or method prev)
arv/runtime/horizon_test.go:14:12: undefined: Now
arv/runtime/horizon_test.go:15:15: root.Horizon undefined (type *Task has no field or method Horizon)
```

- [ ] **Step 3: Implement**

Create `arv/runtime/horizon.go`:

```go
package runtime

import (
	"math"
	"sort"
	"sync/atomic"
)

// clock orders bindings, environments and spawns, for I8: a spawned task
// never sees a name bound after it was spawned. It moves only at a spawn
// (Task.Child), so a binding made since the last spawn may be overwritten
// in place. It is shared by every run in the process; only its order
// matters, not its values.
var clock atomic.Uint64

// Now is the clock's current time: what an environment records when it is
// created and when it binds a name.
func Now() uint64 { return clock.Load() }

// Horizon is the time that bounds what t sees of an environment created
// at born: t sees a binding made there only if it was made before the
// returned time. That is the spawn time of the first task, from the
// root down to t, spawned after the environment was created: from then
// on, the environment was frozen for that task and for everything it
// spawns, however late (I8, transitively). An environment created after
// t was spawned (by t, or by a task t awaited) has no horizon for t:
// math.MaxUint64.
func (t *Task) Horizon(born uint64) uint64 {
	i := sort.Search(len(t.horizon), func(i int) bool { return t.horizon[i] > born })
	if i == len(t.horizon) {
		return math.MaxUint64
	}
	return t.horizon[i]
}

// HorizonLen is how many spawn times t keeps: one per task from the
// root's first child down to t. A child keeps one more.
func (t *Task) HorizonLen() int { return len(t.horizon) }
```

In `arv/runtime/task.go`, replace:

```go
	steps    uint64
	children atomic.Uint64
}
```

with:

```go
	steps    uint64
	children atomic.Uint64
	horizon  []uint64 // spawn times from the root's child down to this task (see Horizon)
}
```

In `arv/runtime/task.go`, replace:

```go
// Child creates a sub-task with attenuated authority and its own context.
func (t *Task) Child(ctx context.Context, caps CapSet) *Task {
	n := t.children.Add(1) - 1
	return &Task{ID: fmt.Sprintf("%s/%d", t.ID, n), Caps: caps, Ctx: ctx, Out: t.Out, parent: t}
}
```

with:

```go
// Child creates a sub-task with attenuated authority and its own context.
// It moves the clock: from now on, what the parent binds is invisible to
// the child and to everything the child spawns (I8, see Horizon).
func (t *Task) Child(ctx context.Context, caps CapSet) *Task {
	n := t.children.Add(1) - 1
	horizon := append(t.horizon[:len(t.horizon):len(t.horizon)], clock.Add(1))
	return &Task{ID: fmt.Sprintf("%s/%d", t.ID, n), Caps: caps, Ctx: ctx, Out: t.Out, parent: t, horizon: horizon}
}
```

In `arv/value/environment.go`, replace:

```go
package value

import "sync"

func NewEnvironment() *Environment {
	return &Environment{store: make(map[string]Object)}
}

// Environment is a lexical scope. Closures passed to spawn are read by the
// child task while the parent may still bind names, so access is locked.
type Environment struct {
	mu    sync.RWMutex
	store map[string]Object
	outer *Environment
}

func (e *Environment) Get(name string) (Object, bool) {
	e.mu.RLock()
	obj, ok := e.store[name]
	e.mu.RUnlock()
	if !ok && e.outer != nil {
		return e.outer.Get(name)
	}
	return obj, ok
}

func (e *Environment) Set(name string, val Object) Object {
	e.mu.Lock()
	e.store[name] = val
	e.mu.Unlock()
	return val
```

with:

```go
package value

import (
	"math"
	"sync"

	"github.com/delavalom/arvlang/arv/runtime"
)

func NewEnvironment() *Environment {
	return &Environment{store: make(map[string]*binding), born: runtime.Now()}
}

// Environment is a lexical scope. Closures passed to spawn are read by the
// child task while the parent may still bind names, so access is locked,
// and a name keeps what it was bound to before, for tasks spawned before
// it was bound again (I8: a child never sees its parent rebind a name
// after the spawn; see GetAs).
type Environment struct {
	mu    sync.RWMutex
	store map[string]*binding
	outer *Environment
	born  uint64 // runtime.Now() when created
}

// binding is one value a name was bound to, and the one before it.
type binding struct {
	val  Object
	at   uint64 // runtime.Now() when bound
	prev *binding
}

// Get finds name as the task that owns the environment sees it: its
// latest binding.
func (e *Environment) Get(name string) (Object, bool) { return e.GetAs(name, nil) }

// GetAs finds name as task t sees it: in each environment, from e
// outwards, the latest binding made before t's horizon there
// (runtime.Task.Horizon). So a task spawned with a closure, or anything
// that holds one, keeps seeing the environments it captured as they were
// at the spawn, even through closures defined earlier or values passed to
// it, and so does everything it spawns. A nil t sees the latest bindings.
func (e *Environment) GetAs(name string, t *runtime.Task) (Object, bool) {
	for env := e; env != nil; env = env.outer {
		limit := uint64(math.MaxUint64)
		if t != nil {
			limit = t.Horizon(env.born)
		}
		env.mu.RLock()
		b := env.store[name]
		for b != nil && b.at >= limit {
			b = b.prev
		}
		env.mu.RUnlock()
		if b != nil {
			return b.val, true
		}
	}
	return nil, false
}

// Set binds name to val. If no task was spawned since name was last bound
// here, no task can see the old value, and it is replaced; otherwise it
// is kept for the tasks spawned in between.
func (e *Environment) Set(name string, val Object) Object {
	now := runtime.Now()
	e.mu.Lock()
	if b := e.store[name]; b != nil && b.at == now {
		b.val = val
	} else {
		e.store[name] = &binding{val: val, at: now, prev: b}
	}
	e.mu.Unlock()
	return val
```

- [ ] **Step 4: Run the tests, then the full checks**

Run: `cd <worktree> && go test -race ./arv/runtime/ ./arv/value/ -run 'TestHorizon|TestEnvironment'`, then `cd <worktree> && gofmt -l . && go vet ./... && go test -race ./...`

Expected: `ok` for every package, and `gofmt -l .` prints nothing.

- [ ] **Step 5: Commit**

```bash
cd <worktree> && git add arv/runtime/horizon.go arv/runtime/horizon_test.go arv/runtime/task.go arv/value/environment.go arv/value/environment_test.go && git commit -F - <<'MSG'
feat(runtime,value): versioned environments read as of a spawn horizon (I8)

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
MSG
```
