# Phase 1a: Capabilities and tasks (`arv/runtime`)

**Goal:** A dependency-free `runtime` package that can say *what a task holds* and *whether a request is covered*, and a `Task` type that later carries cancellation and budgets.

**Invariants touched:** I1, I2 and I3 (see `01-design.md`).

**Rule for this package:** standard library only. It must never import `arv/value` or `arv/evaluator`.

---

### Task 1.1: `Capability`, `CapSet`, `DeniedError`, `Parse`, `Canonical`

**Files:**
- Create: `arv/runtime/capability.go`
- Test: `arv/runtime/capability_test.go`

Coverage rules, all of which the tests pin down:
- `net`: an exact host; `*.example.com` covers subdomains but **not** `example.com` itself; `*` covers every host. Hosts are lowercased.
- `fs_read` / `fs_write`: must be absolute. Both grant and request are made canonical (cleaned, symlinks resolved in the longest existing prefix). A grant covers itself and anything under `grant + "/"`, so `/data` doesn't cover `/database`. Read and write are separate kinds.
- `env`: an exact variable name, or `*`.

- [ ] **Step 1: Write the failing tests**

```go
package runtime

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func mustCap(t *testing.T, k Kind, scope string) Capability {
	t.Helper()
	c, err := New(k, scope)
	if err != nil {
		t.Fatalf("New(%s, %q): %v", k, scope, err)
	}
	return c
}

func TestCovers(t *testing.T) {
	tests := []struct {
		held, want Capability
		ok         bool
	}{
		{mustCap(t, Net, "api.github.com"), mustCap(t, Net, "api.github.com"), true},
		{mustCap(t, Net, "api.github.com"), mustCap(t, Net, "evil.com"), false},
		{mustCap(t, Net, "*.github.com"), mustCap(t, Net, "api.github.com"), true},
		{mustCap(t, Net, "*.github.com"), mustCap(t, Net, "github.com"), false},
		{mustCap(t, Net, "*"), mustCap(t, Net, "anything.io"), true},
		{mustCap(t, FSRead, "/data"), mustCap(t, FSRead, "/data/a.txt"), true},
		{mustCap(t, FSRead, "/data"), mustCap(t, FSRead, "/database/a.txt"), false},
		{mustCap(t, FSRead, "/data"), mustCap(t, FSRead, "/data/../etc/passwd"), false},
		{mustCap(t, FSRead, "/data"), mustCap(t, FSWrite, "/data/a.txt"), false},
		{mustCap(t, Env, "HOME"), mustCap(t, Env, "HOME"), true},
		{mustCap(t, Env, "HOME"), mustCap(t, Env, "AWS_SECRET_ACCESS_KEY"), false},
	}
	for _, tt := range tests {
		if got := tt.held.Covers(tt.want); got != tt.ok {
			t.Errorf("%s covers %s = %v, want %v", tt.held, tt.want, got, tt.ok)
		}
	}
}

func TestNewRejectsBadInput(t *testing.T) {
	for _, c := range []struct {
		k     Kind
		scope string
	}{{"disk", "/"}, {Net, ""}, {FSRead, "relative/path"}} {
		if _, err := New(c.k, c.scope); err == nil {
			t.Errorf("New(%s, %q) succeeded, want error", c.k, c.scope)
		}
	}
}

func TestAttenuate(t *testing.T) {
	parent := NewCapSet(mustCap(t, Net, "*.github.com"), mustCap(t, FSRead, "/data"))

	child, err := parent.Attenuate([]Capability{mustCap(t, Net, "api.github.com")})
	if err != nil {
		t.Fatalf("attenuate to a covered cap: %v", err)
	}
	if child.Allows(mustCap(t, FSRead, "/data/x")) {
		t.Error("child kept fs_read it did not ask for")
	}

	_, err = parent.Attenuate([]Capability{mustCap(t, Net, "evil.com")})
	var denied *DeniedError
	if !errors.As(err, &denied) {
		t.Fatalf("want *DeniedError, got %v", err)
	}
	if !strings.Contains(err.Error(), `net("evil.com")`) {
		t.Errorf("error should name the request, got %q", err)
	}
}

func TestParse(t *testing.T) {
	base := t.TempDir()
	c, err := Parse("fs_read=data", base)
	if want := Canonical(filepath.Join(base, "data")); err != nil || c.Scope != want {
		t.Fatalf("Parse relative fs path = %v, %v; want scope %s", c, err, want)
	}
	if _, err := Parse("net", "/"); err == nil {
		t.Error("Parse without '=' should fail")
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go test ./arv/runtime/`
Expected: build failure, `undefined: New` (and `Kind`, `CapSet`, …).

- [ ] **Step 3: Implement**

```go
// Package runtime holds the per-task state the interpreter enforces:
// capabilities, cancellation, scopes and (later) budgets and the effect log.
// It must not import the value or evaluator packages.
package runtime

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Kind names a class of authority.
type Kind string

const (
	Net     Kind = "net"      // outbound network, scoped to a host
	FSRead  Kind = "fs_read"  // read files under a path
	FSWrite Kind = "fs_write" // write files under a path
	Env     Kind = "env"      // read one environment variable
)

var kinds = map[Kind]bool{Net: true, FSRead: true, FSWrite: true, Env: true}

// Capability is one grant, e.g. net("api.github.com") or fs_read("/data").
type Capability struct {
	Kind  Kind
	Scope string
}

func (c Capability) String() string { return fmt.Sprintf("%s(%q)", c.Kind, c.Scope) }

// New validates and normalises a capability. Filesystem scopes must be absolute.
func New(kind Kind, scope string) (Capability, error) {
	if !kinds[kind] {
		return Capability{}, fmt.Errorf("unknown capability kind %q", kind)
	}
	if scope == "" {
		return Capability{}, fmt.Errorf("%s: empty scope", kind)
	}
	if kind == FSRead || kind == FSWrite {
		if !filepath.IsAbs(scope) {
			return Capability{}, fmt.Errorf("%s: path %q must be absolute", kind, scope)
		}
		scope = Canonical(scope)
	}
	if kind == Net {
		scope = strings.ToLower(scope)
	}
	return Capability{Kind: kind, Scope: scope}, nil
}

// Covers reports whether holding c is enough to perform request r.
func (c Capability) Covers(r Capability) bool {
	if c.Kind != r.Kind {
		return false
	}
	switch c.Kind {
	case Net:
		if c.Scope == "*" || c.Scope == r.Scope {
			return true
		}
		if strings.HasPrefix(c.Scope, "*.") {
			return strings.HasSuffix(r.Scope, c.Scope[1:]) // "*.x.com" covers "a.x.com", not "x.com"
		}
		return false
	case FSRead, FSWrite:
		if c.Scope == "/" || c.Scope == r.Scope {
			return true
		}
		return strings.HasPrefix(r.Scope, c.Scope+string(filepath.Separator))
	case Env:
		return c.Scope == "*" || c.Scope == r.Scope
	}
	return false
}

// CapSet is an immutable set of capabilities held by one task.
type CapSet struct{ caps []Capability }

func NewCapSet(caps ...Capability) CapSet {
	return CapSet{caps: append([]Capability(nil), caps...)}
}

// List returns a copy of the grants.
func (s CapSet) List() []Capability { return append([]Capability(nil), s.caps...) }

// Allows reports whether some grant in s covers r.
func (s CapSet) Allows(r Capability) bool {
	for _, c := range s.caps {
		if c.Covers(r) {
			return true
		}
	}
	return false
}

// Attenuate returns a set holding exactly reqs, or an error naming the first
// request s does not cover. A child can never hold more than its parent.
func (s CapSet) Attenuate(reqs []Capability) (CapSet, error) {
	for _, r := range reqs {
		if !s.Allows(r) {
			return CapSet{}, &DeniedError{Want: r, Held: s.List()}
		}
	}
	return NewCapSet(reqs...), nil
}

// DeniedError is returned when a task asks for authority it does not hold.
type DeniedError struct {
	Task string
	Want Capability
	Held []Capability
}

func (e *DeniedError) Error() string {
	held := make([]string, len(e.Held))
	for i, c := range e.Held {
		held[i] = c.String()
	}
	where := ""
	if e.Task != "" {
		where = " in task " + e.Task
	}
	return fmt.Sprintf("capability denied: %s%s (holds: [%s])", e.Want, where, strings.Join(held, ", "))
}

// Parse reads the CLI form "kind=scope", e.g. "net=api.github.com".
// Relative fs paths are resolved against base.
func Parse(s, base string) (Capability, error) {
	kind, scope, ok := strings.Cut(s, "=")
	if !ok {
		return Capability{}, fmt.Errorf("capability %q: want kind=scope", s)
	}
	k := Kind(kind)
	if (k == FSRead || k == FSWrite) && !filepath.IsAbs(scope) {
		scope = filepath.Join(base, scope)
	}
	return New(k, scope)
}

// Canonical cleans an absolute path and resolves symlinks in the longest
// prefix that exists, so a grant and a request for the same file compare
// equal (on macOS /var is a symlink to /private/var) and a symlink cannot
// smuggle a request outside its grant.
func Canonical(path string) string {
	path = filepath.Clean(path)
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	dir, base := filepath.Split(path)
	if dir == "" || dir == path || base == "" {
		return path
	}
	return filepath.Join(Canonical(dir), base)
}
```

Why `Canonical` resolves symlinks: on macOS, `t.TempDir()` lives under `/var/…`, which is a symlink to `/private/var/…`. Without resolving both sides, a correct grant wouldn't match. Resolving the request also stops a symlink inside a granted directory from pointing outside it (Task 1.5 tests that).

- [ ] **Step 4: Run the tests**

Run: `go test ./arv/runtime/ -v`
Expected: `TestCovers`, `TestNewRejectsBadInput`, `TestAttenuate`, `TestParse` all PASS.

- [ ] **Step 5: Commit**

```bash
git add arv/runtime
git commit -m "feat(runtime): capabilities with coverage, attenuation and canonical paths"
```

---

### Task 1.2: `Task`

A `Task` is what the interpreter consults on every node and every effect. `Tick` is the single hook that later phases extend: Phase 3 adds budgets and Phase 6 adds effect sequencing. Writes through `Task.Out` are serialised, so tasks can share one output.

**Files:**
- Create: `arv/runtime/task.go`
- Test: `arv/runtime/task_test.go`

- [ ] **Step 1: Write the failing tests**

```go
package runtime

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

func TestChildIDsAndAuthority(t *testing.T) {
	net, _ := New(Net, "api.github.com")
	root := NewRoot(context.Background(), NewCapSet(net), &bytes.Buffer{})

	a := root.Child(root.Ctx, NewCapSet())
	b := root.Child(root.Ctx, NewCapSet(net))
	if a.ID != "root/0" || b.ID != "root/1" {
		t.Fatalf("ids = %s, %s", a.ID, b.ID)
	}
	if err := a.Require(net); err == nil {
		t.Error("child with empty caps passed Require")
	}
	if err := b.Require(net); err != nil {
		t.Errorf("child holding net failed Require: %v", err)
	}
}

func TestTickSeesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	task := NewRoot(ctx, NewCapSet(), &bytes.Buffer{})
	stop := errors.New("stop")
	cancel(stop)

	var err error
	for i := 0; i < checkEvery && err == nil; i++ {
		err = task.Tick()
	}
	if !errors.Is(err, stop) {
		t.Fatalf("Tick after cancel = %v, want %v", err, stop)
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go test ./arv/runtime/ -run 'TestChildIDs|TestTick'`
Expected: build failure, `undefined: NewRoot`.

- [ ] **Step 3: Implement**

```go
package runtime

import (
	"context"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
)

// MaxDepth bounds Arv call depth so runaway recursion becomes an Arv error
// instead of a fatal Go stack overflow.
const MaxDepth = 10_000

// checkEvery is how many interpreter steps pass between context checks.
const checkEvery = 256

// Task is one unit of Arv execution. Each task runs on its own goroutine with
// its own interpreter; tasks share no mutable Arv state.
type Task struct {
	ID   string          // "root", "root/0", "root/0/2", ...
	Caps CapSet          // authority this task holds
	Ctx  context.Context // cancelled when the task's scope is cancelled
	Out  io.Writer       // where puts writes; safe for concurrent use

	steps    uint64
	children atomic.Uint64
}

// NewRoot creates the top-level task for a program run.
func NewRoot(ctx context.Context, caps CapSet, out io.Writer) *Task {
	return &Task{ID: "root", Caps: caps, Ctx: ctx, Out: &syncWriter{w: out}}
}

// Child creates a sub-task with attenuated authority and its own context.
func (t *Task) Child(ctx context.Context, caps CapSet) *Task {
	n := t.children.Add(1) - 1
	return &Task{ID: fmt.Sprintf("%s/%d", t.ID, n), Caps: caps, Ctx: ctx, Out: t.Out}
}

// Tick is called once per evaluated node. It returns the context error once
// the task has been cancelled. Budgets (Phase 3) hook in here.
func (t *Task) Tick() error {
	t.steps++
	if t.steps%checkEvery == 0 {
		return context.Cause(t.Ctx)
	}
	return nil
}

// Steps reports how many nodes this task has evaluated.
func (t *Task) Steps() uint64 { return t.steps }

// Require fails with *DeniedError unless the task holds a grant covering c.
func (t *Task) Require(c Capability) error {
	if t.Caps.Allows(c) {
		return nil
	}
	return &DeniedError{Task: t.ID, Want: c, Held: t.Caps.List()}
}

type syncWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (s *syncWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p)
}
```

Design notes:
- The context is checked every 256 steps, not every step. `context.Cause` takes a lock, and one interpreter step is cheap. Cancellation therefore lands within 256 nodes, which is microseconds.
- `MaxDepth = 10_000` is far below where Go's 1 GB stack limit would crash the process. Task 1.3 tests that runaway recursion returns an Arv error.
- Child IDs (`root/0`, `root/0/2`) are assigned in spawn order. A task spawns sequentially, so IDs are deterministic. Phase 6 replay depends on that.

- [ ] **Step 4: Run the tests**

Run: `go test -race ./arv/runtime/`
Expected: `ok`.

- [ ] **Step 5: Commit**

```bash
git add arv/runtime
git commit -m "feat(runtime): Task with authority, cancellation tick and child ids"
```
