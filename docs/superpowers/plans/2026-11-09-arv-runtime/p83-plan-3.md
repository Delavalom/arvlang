# Phase 8.3, part 3 of 10: open helpers that stay inside a grant (os.Root) (task p83.3)

> **For agentic workers:** this file is self-contained. Implement task p83.3 exactly as written, test first. Background, which you don't need to read: `15-phase8-tooling.md` §8.3, `16-implementation-notes.md` (it overrides older plan text) and the invariants I1–I8 in `01-design.md`, all in `docs/superpowers/plans/2026-11-09-arv-runtime/`.

**Goal:** Add `openIn(grant, path, ...)` and the grant-aware open helpers it backs. Each opens a canonical path beneath its grant with `os.Root`, so no symlink can lead the open outside the grant. p83.4 wires `read_file` and `write_file` to these helpers.

**Invariants touched:** I3. Groundwork for: fs effects cannot escape a grant via symlink or directory swap between check and use.

**Depends on:** p83.1–p83.2 committed on `phase/p83`. Check with `git log --oneline -12`.

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

### Task p83.3: open helpers that stay inside a grant (os.Root)

**Design.** A new file, `arv/evaluator/fsopen.go`, holds the shared open logic:
- `openIn(grant, path, flag, perm)`: if `path == grant` (a file grant), there is no directory inside the grant to confine. It opens directly with `openLeaf`, which on unix is the existing `openNonblock` (`O_NOFOLLOW|O_NONBLOCK|O_CLOEXEC`).
- Otherwise it calls `os.OpenRoot(grant)` and `root.OpenFile(rel, flag|rootOpenFlags, perm)`. `os.Root` follows a symlink only while it stays inside the root. One that escapes fails with "path escapes from parent". **Absolute** symlinks are always refused, even ones that point inside the root.
- Errors are rewritten to `open <path>: <err>`, so messages look as they did before.
- `beforeOpen` is a test hook (a package variable, nil in production) that runs just before the open. p83.4 uses it to swap the tree inside the race window.
- `openRegularIn` / `openWritableIn` / `writeIn` are the grant-aware forms. `openRegular(path)`, `openWritable(path, perm)` and `writeNoFollow(path, data)` keep their signatures: each treats `path` as its own grant, so existing tests that call them directly are unchanged.
- The platform files shrink to `rootOpenFlags` plus `openLeaf`. On unix, `rootOpenFlags` is `O_NONBLOCK`, so a FIFO cannot block the open. A regular file ignores the flag, so the descriptor is not switched back to blocking mode. On other platforms, `os.Root` works too, and `rootOpenFlags` is 0.

Behaviour of `read_file` and `write_file` doesn't change in this task: they still call `openRegular(path)` / `writeNoFollow(path, ...)`.

**Files:**
- Create: `arv/evaluator/fsopen.go`
- Modify (rewrite): `arv/evaluator/nofollow_unix.go`, `arv/evaluator/nofollow_other.go`
- Modify: `arv/evaluator/effects.go` (`writeNoFollow` becomes `writeIn`, plus a one-line `writeNoFollow`)
- Test: `arv/evaluator/fsopen_unix_test.go`

- [ ] **Step 1: Write the failing tests**

Create `arv/evaluator/fsopen_unix_test.go`:

```go
//go:build unix

package evaluator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// openIn confines an open to its grant: a symlink inside the grant that
// leads out of it is refused, a relative one that stays inside is
// followed, and errors name the full path, as a plain open would.
func TestOpenInStaysInsideTheGrant(t *testing.T) {
	root := t.TempDir()
	granted, outside := filepath.Join(root, "g"), filepath.Join(root, "out")
	mustMkdir(t, filepath.Join(granted, "real"))
	mustMkdir(t, outside)
	for path, text := range map[string]string{
		filepath.Join(granted, "real", "f.txt"): "inside",
		filepath.Join(outside, "f.txt"):         "SECRET",
	} {
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for link, to := range map[string]string{"esc": outside, "in": "real"} {
		if err := os.Symlink(to, filepath.Join(granted, link)); err != nil {
			t.Fatal(err)
		}
	}

	escaping := filepath.Join(granted, "esc", "f.txt")
	if f, err := openRegularIn(granted, escaping); err == nil {
		f.Close()
		t.Fatal("openRegularIn followed a symlink out of its grant")
	} else if !strings.Contains(err.Error(), "path escapes") || !strings.Contains(err.Error(), escaping) {
		t.Fatalf("err = %v, want a path-escape error naming %s", err, escaping)
	}
	if err := writeIn(granted, filepath.Join(granted, "esc", "new.txt"), []byte("x")); err == nil {
		t.Fatal("writeIn followed a symlink out of its grant")
	}
	if _, err := os.Lstat(filepath.Join(outside, "new.txt")); err == nil {
		t.Fatal("writeIn created a file outside its grant")
	}

	f, err := openRegularIn(granted, filepath.Join(granted, "in", "f.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if data, err := readCapped(f); err != nil || string(data) != "inside" {
		t.Fatalf("read = %q, %v; want the file the inside link reaches", data, err)
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p83 && go test ./arv/evaluator/`

Expected:
Build failure: `undefined: openRegularIn` and `undefined: writeIn`.

- [ ] **Step 3: Implement**

Create `arv/evaluator/nofollow_unix.go`:

```go
//go:build unix

package evaluator

import (
	"os"
	"syscall"
)

// rootOpenFlags are added to every open beneath a grant (openIn). os.Root
// adds O_NOFOLLOW and O_CLOEXEC itself; O_NONBLOCK keeps a FIFO from
// blocking the open (see openRegularIn). Regular files ignore O_NONBLOCK,
// so a descriptor that passes checkRegular reads and writes as usual.
const rootOpenFlags = syscall.O_NONBLOCK

// openLeaf opens path for a file grant (the path is the grant itself), so
// there is no directory inside the grant to confine: O_NOFOLLOW refuses a
// symlink planted at the leaf after the check, and O_NONBLOCK keeps a FIFO
// from blocking the open.
func openLeaf(path string, flag int, perm uint32) (*os.File, error) {
	return openNonblock(path, flag, perm)
}

// openNonblock opens path with mode plus O_NOFOLLOW, O_NONBLOCK and
// O_CLOEXEC, then clears O_NONBLOCK so reads and writes block as usual.
func openNonblock(path string, mode int, perm uint32) (*os.File, error) {
	var fd int
	var err error
	for {
		fd, err = syscall.Open(path, mode|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, perm)
		if err != syscall.EINTR {
			break
		}
	}
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	if err := syscall.SetNonblock(fd, false); err != nil {
		syscall.Close(fd)
		return nil, &os.PathError{Op: "fcntl", Path: path, Err: err}
	}
	return os.NewFile(uintptr(fd), path), nil
}
```

Create `arv/evaluator/nofollow_other.go`:

```go
//go:build !unix

package evaluator

import "os"

// rootOpenFlags are added to every open beneath a grant (openIn).
const rootOpenFlags = 0

// openLeaf opens path for a file grant. There is no O_NOFOLLOW here;
// Canonical's resolution is the only guard against a leaf symlink.
func openLeaf(path string, flag int, perm uint32) (*os.File, error) {
	return os.OpenFile(path, flag, os.FileMode(perm))
}
```

Create `arv/evaluator/fsopen.go`:

```go
package evaluator

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// beforeOpen, when set (only by tests), runs after the capability check and
// before the open: the window in which another process could swap a
// directory inside the grant for a symlink.
var beforeOpen func(path string)

// openIn opens path, which is canonical and covered by grant (fsRequest
// returns both), so that the open cannot leave grant even if the tree
// changes after the check. A directory grant is opened as an os.Root and
// path is opened beneath it: os.Root follows a symlink only while it stays
// inside the root and refuses one that escapes ("path escapes from
// parent"), so a directory swapped for a symlink to outside the grant is
// refused, while one swapped for a symlink to elsewhere inside it reaches a
// file the task may open anyway. When path is the grant itself (a file
// grant), there is no directory inside the grant to confine, and openLeaf
// opens it directly with O_NOFOLLOW.
//
// Errors name path, as a plain open would.
func openIn(grant, path string, flag int, perm uint32) (*os.File, error) {
	if beforeOpen != nil {
		beforeOpen(path)
	}
	if path == grant {
		return openLeaf(path, flag, perm)
	}
	rel, ok := strings.CutPrefix(path, grant)
	if grant != string(filepath.Separator) {
		rel, ok = strings.CutPrefix(rel, string(filepath.Separator))
	}
	if !ok || rel == "" {
		return nil, &fs.PathError{Op: "open", Path: path, Err: errors.New("path is not inside its grant")}
	}
	root, err := os.OpenRoot(grant)
	if err != nil {
		return nil, renamePathError(err, path)
	}
	defer root.Close()
	f, err := root.OpenFile(rel, flag|rootOpenFlags, fs.FileMode(perm))
	if err != nil {
		return nil, renamePathError(err, path)
	}
	return f, nil
}

// renamePathError makes an os.Root error read like a plain open of path.
func renamePathError(err error, path string) error {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return &fs.PathError{Op: "open", Path: path, Err: pe.Err}
	}
	return err
}

// openRegular opens path for reading only if it is a regular file, treating
// path as its own grant (see openIn).
func openRegular(path string) (*os.File, error) { return openRegularIn(path, path) }

// openRegularIn opens path (beneath grant, see openIn) for reading only if
// it is a regular file. It opens with O_NONBLOCK on unix so a FIFO with no
// writer cannot block the open (and the task, which cancellation cannot
// interrupt mid-syscall), then fstats the result and rejects anything not
// regular.
func openRegularIn(grant, path string) (*os.File, error) {
	f, err := openIn(grant, path, os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	return checkRegular(f)
}

// openWritable opens path for writing, treating path as its own grant.
func openWritable(path string, perm uint32) (*os.File, error) {
	return openWritableIn(path, path, perm)
}

// openWritableIn opens path (beneath grant, see openIn) for writing,
// creating it (mode perm) if missing, only if it is a regular file, and
// truncates it. With O_NONBLOCK, a FIFO with no reader fails at once
// (ENXIO) instead of blocking until a reader appears, deaf to cancellation;
// one with a reader opens and is refused by the fstat check. O_TRUNC would
// act on whatever was opened, so the file is truncated only once it is
// known to be regular.
//
// Known limit: a non-regular file is opened before it is refused, so a FIFO
// reader is woken with EOF and a device's open side effects still happen.
// This needs an fs_write grant that covers that file, so the task already
// holds authority over it.
func openWritableIn(grant, path string, perm uint32) (*os.File, error) {
	f, err := openIn(grant, path, os.O_WRONLY|os.O_CREATE, perm)
	if err != nil {
		return nil, err
	}
	if f, err = checkRegular(f); err != nil {
		return nil, err
	}
	if err := f.Truncate(0); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}
```

In `arv/evaluator/effects.go`, replace:

```go
// writeNoFollow is os.WriteFile(path, data, 0o644) for regular files only
// (see openWritable): it refuses a FIFO or device without blocking on it or
// truncating it, and refuses to follow a symlink at the final component.
// path is canonical (fsRequest), so it has no symlinks when checked;
// O_NOFOLLOW stops one planted at the leaf between that check and this
// open. A directory component swapped for a symlink in that window is still
// followed: see runtime.Canonical.
func writeNoFollow(path string, data []byte) error {
	f, err := openWritable(path, 0o644)
```

with:

```go
// writeNoFollow is writeIn with path as its own grant.
func writeNoFollow(path string, data []byte) error { return writeIn(path, path, data) }

// writeIn is os.WriteFile(path, data, 0o644) for regular files only (see
// openWritableIn): it refuses a FIFO or device without blocking on it or
// truncating it. path is canonical and covered by grant (fsRequest), so it
// had no symlinks when checked; openIn keeps the open inside grant even if
// a directory or the leaf is swapped for a symlink after that check.
func writeIn(grant, path string, data []byte) error {
	f, err := openWritableIn(grant, path, 0o644)
```

- [ ] **Step 4: Run the checks and watch them pass**

```bash
cd /Users/delavalom/delavalom-labs/arvlang-wt/p83 && gofmt -l . && go vet ./... && GOOS=windows go vet ./... && go test -race -count=1 ./...
```

Expected: `gofmt` prints nothing, both vets are clean, and every package says `ok` (`arv/token` has no test files).

- [ ] **Step 5: Commit**

```bash
cd /Users/delavalom/delavalom-labs/arvlang-wt/p83 && git add arv/evaluator/effects.go arv/evaluator/fsopen.go arv/evaluator/fsopen_unix_test.go arv/evaluator/nofollow_other.go arv/evaluator/nofollow_unix.go && git commit -F - <<'EOF'
evaluator: open helpers that stay inside a grant (os.Root)

openIn opens a canonical path beneath its grant as an os.Root, which
refuses any symlink that escapes it; a file grant keeps the direct
O_NOFOLLOW open. The old path-only helpers treat the path as its own
grant. read_file and write_file are wired up in the next task.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
EOF
```
