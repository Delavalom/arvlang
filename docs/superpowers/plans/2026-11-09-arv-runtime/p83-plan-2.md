# Phase 8.3, part 2 of 10: CapSet.Grant: the grant that covers a request (task p83.2)

> **For agentic workers:** this file is self-contained. Implement task p83.2 exactly as written, test first. Background, which you don't need to read: `15-phase8-tooling.md` §8.3, `16-implementation-notes.md` (it overrides older plan text) and the invariants I1–I8 in `01-design.md`, all in `docs/superpowers/plans/2026-11-09-arv-runtime/`.

**Goal:** Let an effect find *which* grant covers an admitted request, so p83.3 can confine the open to that grant's directory.

**Invariants touched:** I3 (check at use, on canonical requests).

**Depends on:** p83.1–p83.1 committed on `phase/p83`. Check with `git log --oneline -12`.

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

### Task p83.2: CapSet.Grant: the grant that covers a request

`Grant` picks the covering grant with the longest scope (the most specific). Any covering grant would be correct, because the task holds all of them, but the smallest root is the tightest confinement. `r` must already be canonical: `Admit` returns it that way.

**Files:**
- Create: `arv/runtime/grant.go`
- Test: `arv/runtime/grant_test.go`

- [ ] **Step 1: Write the failing tests**

Create `arv/runtime/grant_test.go`:

```go
package runtime

import (
	"path/filepath"
	"testing"
)

func TestGrantPicksTheMostSpecificCover(t *testing.T) {
	dir, _ := canonTree(t)
	outer := mustCap(t, FSRead, dir)
	inner := mustCap(t, FSRead, filepath.Join(dir, "a"))
	write := mustCap(t, FSWrite, filepath.Join(dir, "a", "b"))
	s := NewCapSet(outer, inner, write)

	for _, c := range []struct {
		req  Capability
		want Capability
		ok   bool
	}{
		{mustCap(t, FSRead, filepath.Join(dir, "a", "b", "f")), inner, true},
		{mustCap(t, FSRead, filepath.Join(dir, "a")), inner, true},
		{mustCap(t, FSRead, filepath.Join(dir, "z")), outer, true},
		{mustCap(t, FSWrite, filepath.Join(dir, "a", "b", "f")), write, true},
		{mustCap(t, FSWrite, filepath.Join(dir, "z")), Capability{}, false},
		{mustCap(t, Env, "HOME"), Capability{}, false},
	} {
		got, ok := s.Grant(c.req)
		if ok != c.ok || got != c.want {
			t.Errorf("Grant(%s) = %s, %v; want %s, %v", c.req, got, ok, c.want, c.ok)
		}
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p83 && go test ./arv/runtime/`

Expected:
Build failure: `s.Grant undefined (type CapSet has no field or method Grant)`.

- [ ] **Step 3: Implement**

Create `arv/runtime/grant.go`:

```go
package runtime

// Grant returns the most specific grant in s that covers r (the covering
// grant with the longest scope), and false if none does. r must already be
// canonical (Admit returns it so). The fs effects use it to confine the
// open to that grant's directory with os.Root, so a directory swapped for a
// symlink between the check and the open cannot lead outside the grant.
func (s CapSet) Grant(r Capability) (Capability, bool) {
	var best Capability
	found := false
	for _, c := range s.caps {
		if c.Covers(r) && (!found || len(c.Scope) > len(best.Scope)) {
			best, found = c, true
		}
	}
	return best, found
}
```

- [ ] **Step 4: Run the checks and watch them pass**

```bash
cd /Users/delavalom/delavalom-labs/arvlang-wt/p83 && gofmt -l . && go vet ./... && GOOS=windows go vet ./... && go test -race -count=1 ./...
```

Expected: `gofmt` prints nothing, both vets are clean, and every package says `ok` (`arv/token` has no test files).

- [ ] **Step 5: Commit**

```bash
cd /Users/delavalom/delavalom-labs/arvlang-wt/p83 && git add arv/runtime/grant.go arv/runtime/grant_test.go && git commit -F - <<'EOF'
runtime: add CapSet.Grant, the most specific covering grant

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
EOF
```
