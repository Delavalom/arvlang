# Phase 4, task p4.4: Every effect decides its error kind

> **For agentic workers:** this file is one task, p4.4. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: tasks p4.1 to p4.3 are committed in your tree. Start every shell command with `cd <your worktree> &&`. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify`; keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0` (toolchain 1.26.2), so do not use `testing/synctest` or `sync.WaitGroup.Go`.
>
> Every code block below was applied, in task order, to a scratch copy of the tree and passed `go vet ./...` and `go test -race ./...` (including `e2e/`). Design: `11-phase4-supervision.md`. Deviations from it, and why: `p4-plan-1.md`.

### Task p4.4: Every effect decides its error kind

Each effect decides the kind where it makes the error, as the design asks:
- A missing grant is `capability`.
- A failure in the world (connection refused, file not found, a directory where a file was expected, a missing parent directory) is `effect`.
- A spent budget is `budget`, and an effect cut short because its task was cancelled is `cancelled`, not `effect`: the world did not fail.
- A bad argument stays `runtime`.

`TestEffectErrorKinds` is the table-driven test over every effect that the design's "phase done" line asks for.

**Files:**
- Modify: `arv/evaluator/builtins.go`
- Modify: `arv/evaluator/effects.go`
- Modify: `arv/evaluator/httpget.go`
- Create (test): `arv/evaluator/errkind_effects_test.go`

- [ ] **Step 1: Write the failing tests**

Create `arv/evaluator/errkind_effects_test.go`:

```go
package evaluator

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// refusedURL is an http URL on 127.0.0.1 where nothing listens.
func refusedURL(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	return "http://" + addr + "/"
}

// Every effect decides its error's kind where the error is made: a
// missing grant is capability, a failure in the world is effect, a spent
// budget is budget, and a bad argument is runtime.
func TestEffectErrorKinds(t *testing.T) {
	big := strings.Repeat("x", 2<<20)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, big)
	}))
	defer srv.Close()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "big.txt"), []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}
	lo := grant(t, runtime.Net, "127.0.0.1")
	rd := grant(t, runtime.FSRead, dir)
	wr := grant(t, runtime.FSWrite, dir)
	q := func(s string) string { return fmt.Sprintf("%q", s) }

	for _, tt := range []struct {
		name, src, kind string
		caps            []runtime.Capability
	}{
		{"http_get without a grant", `http_get(` + q(srv.URL) + `)`, runtime.KindCapability, nil},
		{"http_get refused", `http_get(` + q(refusedURL(t)) + `)`, runtime.KindEffect, []runtime.Capability{lo}},
		{"http_get bad url", `http_get("ftp://x")`, runtime.KindRuntime, nil},
		{"http_get over alloc_mb", `scope (alloc_mb: 1) { http_get(` + q(srv.URL) + `) }`, runtime.KindBudget, []runtime.Capability{lo}},
		{"read_file without a grant", `read_file(` + q(filepath.Join(dir, "big.txt")) + `)`, runtime.KindCapability, nil},
		{"read_file missing", `read_file(` + q(filepath.Join(dir, "missing.txt")) + `)`, runtime.KindEffect, []runtime.Capability{rd}},
		{"read_file of a directory", `read_file(` + q(dir) + `)`, runtime.KindEffect, []runtime.Capability{rd}},
		{"read_file over alloc_mb", `scope (alloc_mb: 1) { read_file(` + q(filepath.Join(dir, "big.txt")) + `) }`, runtime.KindBudget, []runtime.Capability{rd}},
		{"read_file bad argument", `read_file(1)`, runtime.KindRuntime, nil},
		{"write_file without a grant", `write_file(` + q(filepath.Join(dir, "w.txt")) + `, "y")`, runtime.KindCapability, nil},
		{"write_file into a missing directory", `write_file(` + q(filepath.Join(dir, "no", "w.txt")) + `, "y")`, runtime.KindEffect, []runtime.Capability{wr}},
		{"write_file bad text", `write_file(` + q(filepath.Join(dir, "w.txt")) + `, 1)`, runtime.KindRuntime, []runtime.Capability{wr}},
		{"getenv without a grant", `getenv("ARV_KIND_X")`, runtime.KindCapability, nil},
		{"getenv bad argument", `getenv(1)`, runtime.KindRuntime, nil},
		{"sleep bad argument", `sleep(-1)`, runtime.KindRuntime, nil},
		{"net bad scope", `net("")`, runtime.KindRuntime, nil},
		{"fs_read bad scope", `fs_read("")`, runtime.KindRuntime, nil},
		{"puts over alloc_mb", `let d = fn(a, n) { if (n == 0) { a } else { d([a, a], n - 1) } };
			scope (alloc_mb: 1) { puts(d([1], 20)) }`, runtime.KindBudget, nil},
		{"push over alloc_mb", `let d = fn(a, n) { if (n == 0) { a } else { d(push(a, n), n - 1) } };
			scope (alloc_mb: 1) { d([], 2000) }`, runtime.KindBudget, nil},
		{"len bad argument", `len(1)`, runtime.KindRuntime, nil},
		{"child's denial, through await", `let f = fn() { getenv("ARV_KIND_X") }; let t = spawn f(); await t`, runtime.KindCapability, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := runWith(t, tt.src, tt.caps...)
			wantKind(t, got, tt.kind)
		})
	}
}

// An effect cut short because its task was cancelled is of kind
// cancelled, not effect: the world did not fail.
func TestCancelledEffectKinds(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	caps := runtime.NewCapSet(grant(t, runtime.Net, "127.0.0.1"))
	task := runtime.NewRoot(ctx, caps, io.Discard)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	wantKind(t, sleep(task, &value.Integer{Value: 5000}), runtime.KindCancelled)
	wantKind(t, httpGet(task, func(int64) error { return nil }, &value.String{Value: srv.URL}), runtime.KindCancelled)
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd <worktree> && go test -race ./arv/evaluator/ -run 'TestEffectErrorKinds|TestCancelledEffectKinds'`

Expected: FAIL. The tests fail: (the first lines; ports and times vary)

```text
--- FAIL: TestEffectErrorKinds (0.29s)
        errkind_effects_test.go:79: error "capability denied: net(\"127.0.0.1:61396\") in task root (holds: [])" has kind "runtime", want "capability"
--- FAIL: TestCancelledEffectKinds (0.00s)
    errkind_effects_test.go:94: error "sleep cancelled: context canceled" has kind "runtime", want "cancelled"
```

- [ ] **Step 3: Implement**

In `arv/evaluator/builtins.go`, replace:

```go
import (
	"errors"
	"fmt"
```

with:

```go
import (
	"fmt"
```

In `arv/evaluator/builtins.go`, replace:

```go
			length := len(arr.Elements)
			if err := charge(arrayElemSize * int64(length+1)); err != nil {
				return newError("%s", err)
			}
			newElements := make([]value.Object, length+1)
```

with:

```go
			length := len(arr.Elements)
			if err := charge(arrayElemSize * int64(length+1)); err != nil {
				return errorOf(err, runtime.KindBudget)
			}
			newElements := make([]value.Object, length+1)
```

In `arv/evaluator/builtins.go`, replace:

```go
	check := func(n int64) error {
		if err := in.Task.Err(); err != nil {
			return fmt.Errorf("task %s cancelled: %w", in.Task.ID, err)
		}
		if e := in.charge(n); e != nil {
			return errors.New(e.Message)
		}
		return nil
```

with:

```go
	check := func(n int64) error {
		if err := in.Task.Err(); err != nil {
			return &failure{newErrorKind(runtime.KindCancelled, "task %s cancelled: %s", in.Task.ID, err)}
		}
		if e := in.charge(n); e != nil {
			return &failure{e}
		}
		return nil
```

In `arv/evaluator/builtins.go`, replace:

```go
		text, err := value.InspectChecked(arg, check)
		if err != nil {
			return newError("%s", err)
		}
		runtime.WriteLine(in.Task.Out, text)
```

with:

```go
		text, err := value.InspectChecked(arg, check)
		if err != nil {
			return errorValue(err)
		}
		runtime.WriteLine(in.Task.Out, text)
```

In `arv/evaluator/effects.go`, replace:

```go
// readError is the error for a failed read by fn: a spent budget as
// itself, like every budget error, anything else prefixed with what.
func readError(what string, err error) *value.Error {
	var exceeded *runtime.ExceededError
	if errors.As(err, &exceeded) {
		return newError("%s", err)
	}
	return newError("%s: %s", what, err)
}
```

with:

```go
// readError is the error for a failed read by fn: a spent budget as
// itself, like every budget error, anything else prefixed with what, of
// kind effect (or cancelled, if the read was).
func readError(what string, err error) *value.Error {
	var exceeded *runtime.ExceededError
	if errors.As(err, &exceeded) {
		return errorOf(err, runtime.KindBudget)
	}
	return newErrorKind(runtime.ErrorKind(err, runtime.KindEffect), "%s: %s", what, err)
}
```

In `arv/evaluator/effects.go`, replace:

```go
	f, err := openRegularIn(tg)
	if err != nil {
		return newError("read_file: %s", err)
	}
	defer f.Close()
```

with:

```go
	f, err := openRegularIn(tg)
	if err != nil {
		return newErrorKind(runtime.KindEffect, "read_file: %s", err)
	}
	defer f.Close()
```

In `arv/evaluator/effects.go`, replace:

```go
	}
	if err := writeIn(tg, text.Value); err != nil {
		return newError("write_file: %s", err)
	}
	return NIL
```

with:

```go
	}
	if err := writeIn(tg, text.Value); err != nil {
		return newErrorKind(runtime.KindEffect, "write_file: %s", err)
	}
	return NIL
```

In `arv/evaluator/effects.go`, replace:

```go
	}
	if err := t.Require(want); err != nil {
		return newError("%s", err)
	}
	return &value.String{Value: os.Getenv(name)}
```

with:

```go
	}
	if err := t.Require(want); err != nil {
		return errorOf(err, runtime.KindCapability)
	}
	return &value.String{Value: os.Getenv(name)}
```

In `arv/evaluator/effects.go`, replace:

```go
	want, err := t.Admit(lit, ch)
	if spent != nil {
		return fsTarget{}, newError("%s", spent)
	}
	var denied *runtime.DeniedError
	if errors.As(err, &denied) {
		return fsTarget{}, newError("%s", err)
	}
	if err != nil { // resolution failed inside a grant: the OS error
		return fsTarget{}, newError("%s: %s", fn, err)
	}
	g, ok := t.Caps.Grant(want)
	if !ok { // Admit just found a cover, and grants never change
		return fsTarget{}, newError("%s", t.Require(want))
	}
	outer, err := confiningRoot(t.Lineage(), g.Scope, charge)
	if err != nil { // only charge fails it: the budget's error, bare
		return fsTarget{}, newError("%s", err)
	}
	return fsTarget{outer: outer, grant: g.Scope, path: want.Scope}, nil
```

with:

```go
	want, err := t.Admit(lit, ch)
	if spent != nil {
		return fsTarget{}, errorOf(spent, runtime.KindBudget)
	}
	var denied *runtime.DeniedError
	if errors.As(err, &denied) {
		return fsTarget{}, errorOf(err, runtime.KindCapability)
	}
	if err != nil { // resolution failed inside a grant: the OS error
		return fsTarget{}, newErrorKind(runtime.KindEffect, "%s: %s", fn, err)
	}
	g, ok := t.Caps.Grant(want)
	if !ok { // Admit just found a cover, and grants never change
		return fsTarget{}, errorOf(t.Require(want), runtime.KindCapability)
	}
	outer, err := confiningRoot(t.Lineage(), g.Scope, charge)
	if err != nil { // only charge fails it: the budget's error, bare
		return fsTarget{}, errorOf(err, runtime.KindBudget)
	}
	return fsTarget{outer: outer, grant: g.Scope, path: want.Scope}, nil
```

In `arv/evaluator/effects.go`, replace:

```go
		return NIL
	case <-t.Ctx.Done():
		return newError("sleep cancelled: %s", context.Cause(t.Ctx))
	}
}
```

with:

```go
		return NIL
	case <-t.Ctx.Done():
		return newErrorKind(runtime.KindCancelled, "sleep cancelled: %s", context.Cause(t.Ctx))
	}
}
```

In `arv/evaluator/httpget.go`, replace:

```go
	}
	if err := t.Require(want); err != nil {
		return newError("%s", err)
	}
	req, err := http.NewRequestWithContext(t.Ctx, http.MethodGet, raw, nil)
```

with:

```go
	}
	if err := t.Require(want); err != nil {
		return errorOf(err, runtime.KindCapability)
	}
	req, err := http.NewRequestWithContext(t.Ctx, http.MethodGet, raw, nil)
```

In `arv/evaluator/httpget.go`, replace:

```go
		var denied *runtime.DeniedError
		if errors.As(err, &denied) {
			return newError("%s", denied)
		}
		var uerr *url.Error
		if errors.As(err, &uerr) { // it quotes a URL, maybe a redirect's: clip it
			return newError("http_get: %s %s: %s", uerr.Op, quoteClipped(uerr.URL), clipped(uerr.Err.Error()))
		}
		return newError("http_get: %s", clipped(err.Error()))
	}
	defer resp.Body.Close()
	body, err := readCapped(resp.Body, resp.ContentLength, charge)
	if errors.Is(err, errTooBig) {
		return newError("http_get: %s", err)
	}
	if err != nil {
```

with:

```go
		var denied *runtime.DeniedError
		if errors.As(err, &denied) {
			return errorOf(denied, runtime.KindCapability)
		}
		// The world failed (refused, reset, too many redirects), unless
		// the task was cancelled first.
		kind := runtime.KindEffect
		if t.Err() != nil {
			kind = runtime.KindCancelled
		}
		var uerr *url.Error
		if errors.As(err, &uerr) { // it quotes a URL, maybe a redirect's: clip it
			return newErrorKind(kind, "http_get: %s %s: %s", uerr.Op, quoteClipped(uerr.URL), clipped(uerr.Err.Error()))
		}
		return newErrorKind(kind, "http_get: %s", clipped(err.Error()))
	}
	defer resp.Body.Close()
	body, err := readCapped(resp.Body, resp.ContentLength, charge)
	if errors.Is(err, errTooBig) {
		return newErrorKind(runtime.KindEffect, "http_get: %s", err)
	}
	if err != nil {
```

In `arv/evaluator/httpget.go`, replace:

```go
	}
	if err := charge(2 * hashPairSize); err != nil {
		return newError("%s", err)
	}
	return value.NewHash(map[string]value.Object{
```

with:

```go
	}
	if err := charge(2 * hashPairSize); err != nil {
		return errorOf(err, runtime.KindBudget)
	}
	return value.NewHash(map[string]value.Object{
```

- [ ] **Step 4: Run the tests, then the full checks**

Run: `cd <worktree> && go test -race ./arv/evaluator/ -run 'TestEffectErrorKinds|TestCancelledEffectKinds'`, then `cd <worktree> && gofmt -l . && go vet ./... && go test -race ./...`

Expected: `ok` for every package, and `gofmt -l .` prints nothing.

- [ ] **Step 5: Commit**

```bash
cd <worktree> && git add arv/evaluator/builtins.go arv/evaluator/effects.go arv/evaluator/errkind_effects_test.go arv/evaluator/httpget.go && git commit -F - <<'MSG'
feat(evaluator): every effect sets its error kind

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
MSG
```
