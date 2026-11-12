# Phase 8.4, task p84.7: environments keep four names in place

> **For agentic workers:** this file is one task, p84.7. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: task p84.6 is committed on `phase/p84` in `/Users/delavalom/delavalom-labs/arvlang-wt/p84`. Start every shell command with `cd /Users/delavalom/delavalom-labs/arvlang-wt/p84 &&`. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green (first check `df -h /System/Volumes/Data`: below 1 GB free, stop and report "blocked: disk"; the only thing you may delete is the build cache, `go clean -cache`). End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify`, never push. Keep Go files under 15 KB and markdown under 18 KB. Create files that contain `${` with the Write or Edit tool, not with a shell heredoc.
>
> Every code block below was applied, in task order, to a scratch copy of the tree and passed `go vet ./...` and `go test -race` on the packages it touches. Overview, guarantees S1-S6, decisions and deviations: `p84-plan-1.md`.

## Task p84.7: environments keep four names in place

The profile's largest single cost: every call and every loop iteration made an `Environment` with its own Go map, and the map allocated its first group of slots on the first `Set` (`maps.growToSmall`). Most environments hold one to three names (a call's parameters and its `let`s, a loop variable, a `match` arm's name). Now an environment keeps its first four names in an array inside itself and only makes a map for a fifth. Lookups scan at most four names before the map.

Nothing else changes (S1, I8): the lock, the binding history (`binding.prev`, kept only when a task was spawned since the name was last bound), what a spawned task sees (`GetAs` with its horizon), and the in-place replacement of a value bound since the last spawn. The tests check each of these for names in the array and in the map alike, and readers in spawned tasks racing the owner's rebinding under `-race`.

`envInline` is 4. In the scratch copy, 2 was about 5% faster on the speed programs (a smaller environment), and 6 slower; 4 keeps calls with up to four parameters and `let`s off the map.

**Files:**
- Create (test): `arv/value/environment_slots_test.go`
- Modify (test): `arv/value/environment_test.go`
- Replace: `arv/value/environment.go`

- [ ] **Step 1: Write the failing test**

Create `arv/value/environment_slots_test.go`:

```go
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
```

In `arv/value/environment_test.go` (in `TestEnvironmentRebindWithoutSpawnReplaces`, which looked inside the map), replace:

```go
	if b := env.store["x"]; b.prev != nil {
```

with:

```go
	if b := env.latest("x"); b.prev != nil {
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p84 && go test ./arv/value/`
Expected: FAIL, the build fails: `undefined: envInline`, `env.latest undefined`.

- [ ] **Step 3: Keep the first names in place**

Replace the whole of `arv/value/environment.go` with:

```go
package value

import (
	"math"
	"sync"

	"github.com/delavalom/arvlang/arv/runtime"
)

func NewEnvironment() *Environment {
	return &Environment{born: runtime.Now()}
}

// Environment is a lexical scope. Closures passed to spawn are read by the
// child task while the parent may still bind names, so access is locked,
// and a name keeps what it was bound to before, for tasks spawned before
// it was bound again (I8: a child never sees its parent rebind a name
// after the spawn; see GetAs).
//
// The first envInline names live in the environment itself, so a call or
// a loop iteration that binds a few names allocates one environment and
// no map (Phase 8.4: most environments hold a call's parameters or a
// loop variable). Past envInline names, a map holds the rest.
type Environment struct {
	mu    sync.RWMutex
	slots [envInline]slot     // the first names bound here, in order
	n     int                 // slots in use
	more  map[string]*binding // names past the first envInline; nil until then
	outer *Environment
	born  uint64 // runtime.Now() when created
}

// envInline is how many names an environment holds without a map.
const envInline = 4

// slot is one name bound in an environment and its latest binding.
type slot struct {
	name string
	b    *binding
}

// binding is one value a name was bound to, and the one before it.
type binding struct {
	val  Object
	at   uint64 // runtime.Now() when bound
	prev *binding
}

// latest is name's latest binding here, or nil. The caller holds e.mu.
func (e *Environment) latest(name string) *binding {
	for i := 0; i < e.n; i++ {
		if e.slots[i].name == name {
			return e.slots[i].b
		}
	}
	return e.more[name]
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
		b := env.latest(name)
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
	defer e.mu.Unlock()
	for i := 0; i < e.n; i++ {
		if s := &e.slots[i]; s.name == name {
			if s.b.at == now {
				s.b.val = val
			} else {
				s.b = &binding{val: val, at: now, prev: s.b}
			}
			return val
		}
	}
	b := &binding{val: val, at: now}
	if e.n < envInline {
		e.slots[e.n] = slot{name: name, b: b}
		e.n++
		return val
	}
	if old := e.more[name]; old != nil {
		if old.at == now {
			old.val = val
			return val
		}
		b.prev = old
	} else if e.more == nil {
		e.more = make(map[string]*binding)
	}
	e.more[name] = b
	return val
}

func NewEnclosedEnvironment(outer *Environment) *Environment {
	env := NewEnvironment()
	env.outer = outer
	return env
}
```

- [ ] **Step 4: Run the tests and watch them pass**

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p84 && gofmt -l . && go vet ./... && go test -race -count=3 ./arv/value/`
Expected: no gofmt output, vet clean, `ok`.

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p84 && go test -race ./arv/... && go test -race -tags arv_treewalk ./arv/evaluator/ ./arv/`
Expected: `ok` for each package.

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p84 && go test -run '^$' -bench 'BenchmarkSpeed/(calls|loops|closures)' -benchmem -count 3 ./arv/`
Expected: fewer bytes and allocations per op than p84.1's baseline (in the scratch copy `calls` went from 186199 to 142488 allocs/op on top of p84.5, and from about 10.7 to 8.2 ms/op).

- [ ] **Step 5: Check and commit**

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p84 && go test -race ./...`
Expected: every package `ok`.

```bash
cd /Users/delavalom/delavalom-labs/arvlang-wt/p84 && git add arv/value && git commit -m "perf(value): environments keep their first four names in place, no map

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
