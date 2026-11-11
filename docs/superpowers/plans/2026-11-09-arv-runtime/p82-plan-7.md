# Phase 8.2, part 7 of 8: example host, e2e, README (task p82.7)

> **For agentic workers:** this file is self-contained. Implement task p82.7 exactly as written, test first. Optional background, in `docs/superpowers/plans/2026-11-09-arv-runtime/`: `15-phase8-tooling.md` §8.2, `16-implementation-notes.md` (it overrides older plan text), the invariants I1–I8 in `01-design.md`, and the overview and deviations in `p82-plan-1.md`.

**Goal:** The acceptance test of §8.2 as a runnable program: `examples/host` adds a `k8s_apply` effect backed by a fake cluster. It is denied without the grant and allowed with it, in the root and in a child handed `k8s("staging")`. An e2e test builds and runs it as a binary. The README documents host effects.

**Invariants touched:** I1, I2 (through a real binary).

**Depends on:** p82.6 (`arv.Options.Effects`).

**Where:** worktree `/Users/delavalom/delavalom-labs/arvlang-wt/p82`, branch `phase/p82` (branched from `feat/phases-3-8`). If the worktree is missing: `git -C /Users/delavalom/delavalom-labs/arvlang worktree add /Users/delavalom/delavalom-labs/arvlang-wt/p82 phase/p82`. Don't edit the main checkout.

**Rules:**
- Start every shell command with `cd /Users/delavalom/delavalom-labs/arvlang-wt/p82 &&`.
- Keep Go files under 15 KB and markdown under 18 KB. A pre-commit hook blocks any staged file over about 20 KB. Never skip or bypass git hooks.
- Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean, and `go test -race -count=1 ./...` is green. Use `-count=1`: the e2e suite builds binaries itself, so the test cache can't see source changes.
- Check `df -h /System/Volumes/Data` before long runs. If less than 1 GB is free, stop and report `blocked` (disk). The only thing you may delete is the Go build cache (`go clean -cache`).
- Don't weaken a test to make it pass. If a test exposes a real bug outside this task, mark it `t.Skip("BUG: ...")` with a reproduction and report it.
- `arv/runtime` stays stdlib-only and never imports `value` or `evaluator`. Never push or touch remotes.
- The machine is heavily loaded (load average above 300). Order events with gates (channels), not sleeps, and keep wall-clock bounds generous.
- Phases 6, 7 and 8.1 run in parallel on other branches. Keep changes additive and local: only the hunks shown here touch existing files.
- Known flake, older than this phase: `TestNoEffectAfterSiblingSpent/steps/spawn_effect(gate())` hung for 2 min once under load while this plan was validated, at a step that touches no code it runs (its `hold` builtin likely waits on `<-outer` forever when the budget refuses `gate` first). Rerun it once. If it recurs, report it, and don't edit it.

**Validated:** every block below was compiled and tested in a scratch copy, with p82.1–p82.8 applied in order, on go1.26.2 darwin/arm64 (`go.mod` says `go 1.24.0`): `gofmt`, `go vet` (darwin and linux) and `go test -race -count=1 ./...`. If an `old` block doesn't match exactly, read the surrounding code and make the equivalent change; don't guess.

---

### Task p82.7: example host, e2e, README

**Deviation: recording and replaying the example waits for Phase 6.** §8.2's acceptance says "denied without the grant, allowed with it, recorded and replayed". The first two are tested here. The third needs Phase 6, which isn't merged; `16-phase82.md` (p82.8) leaves it to the 6x7 integration.

**Why the script is a Go constant, not an `.arv` file.** Phase 8.1 (`arv check`, in parallel) checks the `.arv` files under `examples/` against their README lines. It can't know a host kind such as `k8s`, so an `examples/host/*.arv` file could trip it.

**What the example prints.** With `-allow staging`:
- `web.yaml` (the root applies it);
- `api.yaml` (a child holding only `k8s("staging")` applies it);
- `prod: capability` (a `match` on the denied prod call);
- `cluster: staging=[web.yaml api.yaml]`, the fake cluster's state, printed by the host.

With no grant, the first call fails, nothing is printed, and the exit code is 1.

**Files:**
- Create: `examples/host/main.go`
- Create: `examples/host/main_test.go`
- Create: `e2e/p82_host_test.go`
- Modify: `README.md` (one new section)

- [ ] **Step 1: Write the failing tests**

Create `examples/host/main_test.go`:

```go
package main

import (
	"bytes"
	"strings"
	"testing"
)

// The acceptance test of 15-phase8-tooling.md §8.2 (recording and
// replaying it waits for Phase 6; see 16-phase82.md): k8s_apply is denied
// without the grant and allowed with it, in the root and in a child the
// root hands k8s("staging"); prod stays out of reach.
func TestHostExample(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run(nil, &out, &errOut); code != 1 {
		t.Fatalf("no grant: exit %d, want 1; stdout %q", code, out.String())
	}
	if want := `error: capability denied: k8s("staging") in task root (holds: [])`; !strings.Contains(errOut.String(), want) {
		t.Fatalf("no grant: stderr %q, want %q", errOut.String(), want)
	}
	if out.Len() != 0 {
		t.Fatalf("no grant: stdout %q, want nothing", out.String())
	}
	out.Reset()
	errOut.Reset()
	if code := run([]string{"staging"}, &out, &errOut); code != 0 {
		t.Fatalf("staging: exit %d; stderr %q", code, errOut.String())
	}
	want := "web.yaml\napi.yaml\nprod: capability\ncluster: staging=[web.yaml api.yaml]\n"
	if out.String() != want {
		t.Fatalf("staging: stdout %q, want %q", out.String(), want)
	}
}
```

Create `e2e/p82_host_test.go`. It builds the example with `go build`, with `-race` under `ARV_E2E_RACE=1` as the harness does for `arv`, and runs it with an empty environment:

```go
package e2e

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Phase 8.2: the example Go host (examples/host), built and run as a
// binary. Its k8s_apply effect is denied without a k8s grant, allowed in
// the granted namespace, from the root and from a child handed
// k8s("staging"), and denied in a namespace the host did not grant.
func TestP82HostExample(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "host")
	args := []string{"build", "-o", bin}
	if os.Getenv("ARV_E2E_RACE") != "" {
		args = append(args, "-race")
	}
	build := exec.Command("go", append(args, "github.com/delavalom/arvlang/examples/host")...)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building examples/host: %v\n%s", err, out)
	}
	for _, tt := range []struct {
		name   string
		args   []string
		code   int
		stdout string
		stderr string
	}{
		{"no grant", nil, 1, "", `error: capability denied: k8s("staging") in task root (holds: [])`},
		{"staging", []string{"-allow", "staging"}, 0,
			"web.yaml\napi.yaml\nprod: capability\ncluster: staging=[web.yaml api.yaml]\n", ""},
		{"staging and prod", []string{"-allow", "staging", "-allow", "prod"}, 0,
			"web.yaml\napi.yaml\nprod: applied\ncluster: prod=[web.yaml] staging=[web.yaml api.yaml]\n", ""},
		{"prod only", []string{"-allow", "prod"}, 1, "", `capability denied: k8s("staging") in task root (holds: [k8s("prod")])`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			cmd := exec.CommandContext(ctx, bin, tt.args...)
			cmd.Env = []string{} // never inherit the user's env
			cmd.Dir = t.TempDir()
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			err := cmd.Run()
			code := 0
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) && exitErr.Exited() {
				code = exitErr.ExitCode()
			} else if err != nil {
				t.Fatalf("running %s: %v", bin, err)
			}
			if code != tt.code || stdout.String() != tt.stdout || !strings.Contains(stderr.String(), tt.stderr) {
				t.Fatalf("exit %d, stdout %q, stderr %q; want exit %d, stdout %q, stderr containing %q",
					code, stdout.String(), stderr.String(), tt.code, tt.stdout, tt.stderr)
			}
		})
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p82 && go test ./examples/host/ ; go test -count=1 ./e2e/ -run P82`

Expected: `examples/host` has no non-test Go file yet, so the first fails to build (`undefined: run`, or no buildable Go files). The e2e test fails at `building examples/host`.

- [ ] **Step 3: Implement**

Create `examples/host/main.go`:

```go
// Command host is an example Go host for Arv (Phase 8.2). It adds one
// effect, k8s_apply(namespace, manifest), backed by a fake in-memory
// cluster. Every call needs a k8s(namespace) grant, which only the host
// can give (-allow), and a task can hand on to a child it spawns.
//
//	go run ./examples/host                  # denied: the run holds no k8s grant
//	go run ./examples/host -allow staging   # applies to staging; prod stays denied
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/delavalom/arvlang/arv"
	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// script deploys to staging from the root and from a child holding only
// k8s("staging"), then tries prod and reports what kind of error it got.
const script = `
let deploy = fn(ns, manifest) { k8s_apply(ns, manifest)["applied"] };
puts(deploy("staging", "web.yaml"));
let t = spawn deploy("staging", "api.yaml") with [k8s("staging")];
puts(await t);
match k8s_apply("prod", "web.yaml") {
  ok(v) => puts("prod: applied"),
  err(e) => puts("prod: " + e["kind"])
}
`

// cluster is the fake: what each namespace has had applied, in order.
type cluster struct {
	mu      sync.Mutex
	applied map[string][]string
}

// args reads k8s_apply's (namespace, manifest).
func args(a []value.Object) (ns, manifest string, err error) {
	if len(a) != 2 {
		return "", "", fmt.Errorf("want 2 arguments (namespace, manifest), got %d", len(a))
	}
	n, ok1 := a[0].(*value.String)
	m, ok2 := a[1].(*value.String)
	if !ok1 || !ok2 {
		return "", "", errors.New("namespace and manifest must be STRING")
	}
	return n.Value, m.Value, nil
}

// namespace is k8s_apply's Scope: the call needs k8s(namespace).
func namespace(a []value.Object) (string, error) {
	ns, _, err := args(a)
	return ns, err
}

// apply is k8s_apply's Fn. The runtime calls it only once the calling
// task holds k8s(namespace); a real host would talk to the API server
// here, honouring t.Ctx.
func (c *cluster) apply(t *runtime.Task, a ...value.Object) (value.Object, error) {
	ns, manifest, err := args(a)
	if err != nil {
		return nil, err
	}
	if t.Ctx.Err() != nil {
		return nil, context.Cause(t.Ctx)
	}
	c.mu.Lock()
	c.applied[ns] = append(c.applied[ns], manifest)
	c.mu.Unlock()
	return value.NewHash(map[string]value.Object{
		"namespace": &value.String{Value: ns},
		"applied":   &value.String{Value: manifest},
	}), nil
}

func (c *cluster) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var parts []string
	for _, ns := range slices.Sorted(maps.Keys(c.applied)) {
		parts = append(parts, fmt.Sprintf("%s=%v", ns, c.applied[ns]))
	}
	return strings.Join(parts, " ")
}

// run runs the script holding k8s(ns) for each namespace in allow, and
// returns the exit code.
func run(allow []string, stdout, stderr io.Writer) int {
	c := &cluster{applied: map[string][]string{}}
	var caps []runtime.Capability
	for _, ns := range allow {
		caps = append(caps, runtime.Capability{Kind: "k8s", Scope: ns})
	}
	_, err := arv.Run(script, arv.Options{
		Caps:   caps,
		Out:    stdout,
		Limits: runtime.Limits{Timeout: 30 * time.Second},
		Effects: map[string]arv.Effect{
			"k8s_apply": {Kind: "k8s", Scope: namespace, Fn: c.apply},
		},
	})
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	fmt.Fprintf(stdout, "cluster: %s\n", c)
	return 0
}

type list []string

func (l *list) String() string     { return strings.Join(*l, ",") }
func (l *list) Set(s string) error { *l = append(*l, s); return nil }

func main() {
	var allow list
	flag.Var(&allow, "allow", "grant k8s(`NAMESPACE`); repeat for more")
	flag.Parse()
	os.Exit(run(allow, os.Stdout, os.Stderr))
}
```

In `README.md`, insert this section immediately before the `## Development` heading (with a blank line after it):

````markdown
## Host effects (embedding)

A Go program embeds Arv with `arv.Run(src, arv.Options{...})` and can add its own effects. Each one names the capability kind it needs and how a call's scope is found; the runtime checks the calling task's grants before it calls the host's function, and an effect without a kind can't be registered:

```go
arv.Run(src, arv.Options{
	Caps: []runtime.Capability{{Kind: "k8s", Scope: "staging"}},
	Effects: map[string]arv.Effect{
		"k8s_apply": {Kind: "k8s", Scope: namespaceArg, Fn: applyManifest},
	},
})
```

The script calls `k8s_apply("staging", manifest)` and hands the grant to a child with `spawn f() with [k8s("staging")]`. `Fn` gets the calling task (honour `t.Ctx`), returns Arv data or an error, and its errors become Arv errors of kind `effect`. `Options.Kinds` gives a kind its own coverage rule; the default is an exact match. `examples/host` is a runnable host with a fake cluster: `go run ./examples/host -allow staging`.
````

- [ ] **Step 4: Run them and watch them pass**

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p82 && go test -race -count=1 ./examples/host/ && go test -race -count=1 ./e2e/ -run P82 -v && ARV_E2E_RACE=1 go test -race -count=1 ./e2e/ -run P82`

Expected: `ok` three times. By hand: `go run ./examples/host -allow staging` prints the four lines above.

- [ ] **Step 5: Commit**

```bash
cd /Users/delavalom/delavalom-labs/arvlang-wt/p82 && gofmt -l . && go vet ./... && go test -race -count=1 ./... \
  && git add examples/host/main.go examples/host/main_test.go e2e/p82_host_test.go README.md \
  && git commit -F - <<'EOF'
feat(examples): a Go host with a k8s_apply effect

Phase 8.2 (p82.7): examples/host adds k8s_apply on a fake cluster,
needing k8s(namespace). Denied without the grant, allowed with it, in
the root and in a child handed k8s("staging"). Unit test, e2e test of
the built binary, and a README section on host effects.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
EOF
```
