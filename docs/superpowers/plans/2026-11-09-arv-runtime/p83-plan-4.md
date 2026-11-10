# Phase 8.3, part 4 of 10: read_file and write_file open beneath their grant (task p83.4)

> **For agentic workers:** this file is self-contained. Implement task p83.4 exactly as written, test first. Background, which you don't need to read: `15-phase8-tooling.md` §8.3, `16-implementation-notes.md` (it overrides older plan text) and the invariants I1–I8 in `01-design.md`, all in `docs/superpowers/plans/2026-11-09-arv-runtime/`.

**Goal:** Close the check/use race for `read_file` and `write_file`. A directory, or the leaf, inside a grant may be swapped for a symlink to outside the grant between the capability check and the open. That swap must not redirect the open.

**Invariants touched:** I3. Guarantee: fs effects cannot escape a grant via symlink or directory swap between check and use.

**Depends on:** p83.1–p83.3 committed on `phase/p83`. Check with `git log --oneline -12`.

**Where:** worktree `/Users/delavalom/delavalom-labs/arvlang-wt/p83`, branch `phase/p83` (branched from `feat/phases-3-8`). If the worktree is missing: `git -C /Users/delavalom/delavalom-labs/arvlang worktree add /Users/delavalom/delavalom-labs/arvlang-wt/p83 phase/p83`. Don't edit the main checkout.

**Rules:**
- Start every shell command with `cd /Users/delavalom/delavalom-labs/arvlang-wt/p83 &&`.
- Keep Go files under 15 KB and markdown under 18 KB. A pre-commit hook blocks any staged file over about 20 KB. Never skip or bypass git hooks.
- Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean, and `go test -race -count=1 ./...` is green. Use `-count=1`: the e2e suite builds the binary itself, so the test cache can't see source changes.
- Check `df -h /System/Volumes/Data` before long runs. If less than 1 GB is free, stop and report `blocked` (disk). The only thing you may delete is the Go build cache (`go clean -cache`).
- Don't weaken a test to make it pass. If a test exposes a real bug outside this task, mark it `t.Skip("BUG: ...")` with a reproduction and report it.
- `arv/runtime` stays stdlib-only. Never push or touch remotes.
- The machine is heavily loaded (load average above 300), so keep timing bounds generous.

**Validated:** every block below was compiled and tested in a scratch copy, with p83.1–p83.10 applied in order, on go1.26.2 darwin/arm64: `gofmt`, `go vet` (darwin, linux, windows) and `go test -race -count=1 ./...`. If an `old` block doesn't match exactly, an earlier task was done differently. Read the surrounding code and make the equivalent change; don't guess.

---

### Task p83.4: read_file and write_file open beneath their grant

**Design.** `fsRequest` now returns `(path, grant)`: the canonical path, as before, and the scope of the most specific grant covering it (`t.Caps.Grant`, from p83.2). `readFile` and `writeFile` pass both to `openRegularIn` / `writeIn` (from p83.3). The tests use the `beforeOpen` hook to swap the tree in the race window, so they are deterministic. Evaluator tests are not parallel, so a package-level hook is safe.

**Why no e2e expectation changes**, although `16-implementation-notes.md` predicted some would: `Canonical` still resolves every symlink before the check, so the path `os.Root` opens contains no symlinks. Behaviour differs only when the tree changes between check and open. p83.10 writes this down.

**Files:**
- Modify: `arv/evaluator/effects.go` (`readFile`, `writeFile`, `fsRequest`)
- Modify: `arv/runtime/canonical.go` (doc comment only)
- Test: `arv/evaluator/swap_unix_test.go`

- [ ] **Step 1: Write the failing tests**

Create `arv/evaluator/swap_unix_test.go`:

```go
//go:build unix

package evaluator

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
)

// swapTree builds root/g/sub/f.txt ("inside"), root/g/other/f.txt ("other")
// and root/out/f.txt ("SECRET"). The grant is root/g.
func swapTree(t *testing.T) (granted, outside string) {
	t.Helper()
	root := t.TempDir()
	granted, outside = filepath.Join(root, "g"), filepath.Join(root, "out")
	for path, text := range map[string]string{
		filepath.Join(granted, "sub", "f.txt"):   "inside",
		filepath.Join(granted, "other", "f.txt"): "other",
		filepath.Join(outside, "f.txt"):          "SECRET",
	} {
		mustMkdir(t, filepath.Dir(path))
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return granted, outside
}

// swapBeforeOpen replaces dir with a symlink to target in the window
// between the capability check and the open, as a racing process could.
func swapBeforeOpen(t *testing.T, dir, target string) {
	t.Helper()
	beforeOpen = func(string) {
		if err := os.Rename(dir, dir+".orig"); err != nil {
			t.Error(err)
		}
		if err := os.Symlink(target, dir); err != nil {
			t.Error(err)
		}
	}
	t.Cleanup(func() { beforeOpen = nil })
}

// I3: a directory inside the grant swapped for a symlink to outside it
// after the check must not redirect read_file there.
func TestReadFileDirSwapCannotEscape(t *testing.T) {
	granted, outside := swapTree(t)
	swapBeforeOpen(t, filepath.Join(granted, "sub"), outside)
	got, _ := runWith(t, fmt.Sprintf(`read_file(%q)`, filepath.Join(granted, "sub", "f.txt")),
		grant(t, runtime.FSRead, granted))
	if got.Inspect() == "SECRET" {
		t.Fatal("read_file followed a swapped directory out of its grant")
	}
	wantErrorHas(t, got, "path escapes")
}

// I3: the same race must not let write_file create a file outside the grant.
func TestWriteFileDirSwapCannotEscape(t *testing.T) {
	granted, outside := swapTree(t)
	swapBeforeOpen(t, filepath.Join(granted, "sub"), outside)
	got, _ := runWith(t, fmt.Sprintf(`write_file(%q, "pwned")`, filepath.Join(granted, "sub", "new.txt")),
		grant(t, runtime.FSWrite, granted))
	if _, err := os.Lstat(filepath.Join(outside, "new.txt")); err == nil {
		t.Fatal("write_file followed a swapped directory out of its grant")
	}
	wantErrorHas(t, got, "path escapes")
}

// A swap to a relative symlink that stays inside the grant reaches a file
// the task may read anyway, so os.Root follows it. (os.Root treats every
// absolute symlink as escaping, even one that points inside the root.)
func TestReadFileDirSwapInsideGrantIsFollowed(t *testing.T) {
	granted, _ := swapTree(t)
	swapBeforeOpen(t, filepath.Join(granted, "sub"), "other")
	got, _ := runWith(t, fmt.Sprintf(`read_file(%q)`, filepath.Join(granted, "sub", "f.txt")),
		grant(t, runtime.FSRead, granted))
	if got.Inspect() != "other" {
		t.Fatalf("got %s, want the swapped-in file inside the grant", got.Inspect())
	}
}

// A leaf swapped for a symlink to outside the grant is refused too.
func TestReadFileLeafSwapCannotEscape(t *testing.T) {
	granted, outside := swapTree(t)
	leaf := filepath.Join(granted, "sub", "f.txt")
	swapBeforeOpen(t, leaf, filepath.Join(outside, "f.txt"))
	got, _ := runWith(t, fmt.Sprintf(`read_file(%q)`, leaf), grant(t, runtime.FSRead, granted))
	if got.Inspect() == "SECRET" {
		t.Fatal("read_file followed a swapped leaf out of its grant")
	}
	wantErrorHas(t, got, "path escapes")
}

// A file grant has no directory to confine; the leaf keeps O_NOFOLLOW.
func TestReadFileFileGrantLeafSwapIsRefused(t *testing.T) {
	granted, outside := swapTree(t)
	leaf := filepath.Join(granted, "sub", "f.txt")
	swapBeforeOpen(t, leaf, filepath.Join(outside, "f.txt"))
	got, _ := runWith(t, fmt.Sprintf(`read_file(%q)`, leaf), grant(t, runtime.FSRead, leaf))
	if got.Inspect() == "SECRET" {
		t.Fatal("read_file followed a swapped leaf out of its file grant")
	}
	wantErrorHas(t, got, "read_file: ")
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p83 && go test -count=1 ./arv/evaluator/`

Expected:
`TestReadFileDirSwapCannotEscape` (`read_file followed a swapped directory out of its grant`), `TestWriteFileDirSwapCannotEscape` and `TestReadFileLeafSwapCannotEscape` fail. The leaf case fails on the message: the old `O_NOFOLLOW` open refuses it, but with "too many levels of symbolic links". `TestReadFileDirSwapInsideGrantIsFollowed` and `TestReadFileFileGrantLeafSwapIsRefused` already pass.

- [ ] **Step 3: Implement**

In `arv/evaluator/effects.go`, replace:

```go
	path, errObj := fsRequest(t, "read_file", runtime.FSRead, args)
	if errObj != nil {
		return errObj
	}
	f, err := openRegular(path)
```

with:

```go
	path, grant, errObj := fsRequest(t, "read_file", runtime.FSRead, args)
	if errObj != nil {
		return errObj
	}
	f, err := openRegularIn(grant, path)
```

In `arv/evaluator/effects.go`, replace:

```go
	path, errObj := fsRequest(t, "write_file", runtime.FSWrite, args[:1])
	if errObj != nil {
		return errObj
	}
	if err := writeNoFollow(path, []byte(text.Value)); err != nil {
```

with:

```go
	path, grant, errObj := fsRequest(t, "write_file", runtime.FSWrite, args[:1])
	if errObj != nil {
		return errObj
	}
	if err := writeIn(grant, path, []byte(text.Value)); err != nil {
```

In `arv/evaluator/effects.go`, replace the whole block from the line
`// the task's grants may leak (I1). The caller must open the returned`
through the closing `}` of `func fsRequest` (the end of that function), 28 lines in all, with:

```go
// the task's grants may leak (I1). It returns the canonical path and the
// scope of the most specific grant covering it: the caller must open that
// path beneath that grant (openIn), so the check and the use agree (I3)
// even if the tree changes in between.
func fsRequest(t *runtime.Task, fn string, kind runtime.Kind, args []value.Object) (path, grant string, errObj *value.Error) {
	raw, errObj := oneString(fn, args)
	if errObj != nil {
		return "", "", errObj
	}
	if raw == "" {
		return "", "", newError("%s: empty path", fn)
	}
	abs, err := absPath(raw)
	if err != nil {
		return "", "", newError("%s: %s", fn, err)
	}
	lit, err := runtime.Literal(kind, abs)
	if err != nil {
		return "", "", newError("%s: %s", fn, err)
	}
	want, err := t.Admit(lit)
	var denied *runtime.DeniedError
	if errors.As(err, &denied) {
		return "", "", newError("%s", err)
	}
	if err != nil { // resolution failed inside a grant: the OS error
		return "", "", newError("%s: %s", fn, err)
	}
	g, ok := t.Caps.Grant(want)
	if !ok { // Admit just found a cover, and grants never change
		return "", "", newError("%s", t.Require(want))
	}
	return want.Scope, g.Scope, nil
}
```

In `arv/runtime/canonical.go`, replace:

```go
// Limit: this is check-then-use. Between Canonical and the open, another
// process that can write inside the grant could swap a directory component
// for a symlink and redirect the open. Callers open the canonical path with
// O_NOFOLLOW, which closes this for the final component only. Closing it for
// directories needs openat-style resolution (Go 1.24's os.Root).
```

with:

```go
// This is check-then-use: between Canonical and the open, another process
// that can write inside the grant could swap a directory component for a
// symlink. The fs effects therefore open the canonical path beneath the
// covering grant with os.Root (evaluator.openIn), which refuses any symlink
// that leads out of the grant, so the swap cannot redirect the open.
```

- [ ] **Step 4: Run the checks and watch them pass**

```bash
cd /Users/delavalom/delavalom-labs/arvlang-wt/p83 && gofmt -l . && go vet ./... && GOOS=windows go vet ./... && go test -race -count=1 ./...
```

Expected: `gofmt` prints nothing, both vets are clean, and every package says `ok` (`arv/token` has no test files).

- [ ] **Step 5: Commit**

```bash
cd /Users/delavalom/delavalom-labs/arvlang-wt/p83 && git add arv/evaluator/effects.go arv/evaluator/swap_unix_test.go arv/runtime/canonical.go && git commit -F - <<'EOF'
evaluator: read_file and write_file open beneath their grant

A directory inside a grant swapped for a symlink between the capability
check and the open used to redirect the open outside the grant
(O_NOFOLLOW only covered the leaf). fsRequest now also returns the
covering grant, and the effects open the canonical path beneath it
with os.Root, which refuses any symlink that escapes.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
EOF
```
