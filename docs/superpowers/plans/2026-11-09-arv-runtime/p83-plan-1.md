# Phase 8.3, part 1 of 10: go.mod to Go 1.24 (task p83.1)

> **For agentic workers:** this file is self-contained. Implement task p83.1 exactly as written, test first. Background, which you don't need to read: `15-phase8-tooling.md` §8.3, `16-implementation-notes.md` (it overrides older plan text) and the invariants I1–I8 in `01-design.md`, all in `docs/superpowers/plans/2026-11-09-arv-runtime/`.

**Goal:** Raise the `go` directive to the lowest version that provides `os.Root` (Go 1.24), so later tasks can use it, and keep `go vet` clean under the new language version.

**Invariants touched:** none directly (enables I3 hardening in p83.3).

**Depends on:** nothing (first task).

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

### Task p83.1: go.mod to Go 1.24

**Why 1.24.0:** `os.Root` (and `os.OpenRoot`, `Root.OpenFile`) first shipped in Go 1.24. The installed toolchain is go1.26.2, so no download happens. Write `go 1.24.0` (the form `go mod tidy` writes), not `go 1.24`.

**Side effect found while validating:** for modules at `go 1.24` or later, `go vet`'s printf check reports calls with a non-constant format string. Exactly one call in the repo trips it (`newError("identifier not found: " + node.Value)` in `arv/evaluator/eval_functions.go`). Phase 3 runs in parallel and edits the evaluator, so keep this to the one line: the merge agent will see at most a one-line conflict there.

**Phase 8.3 at a glance** (one plan file per task, `p83-plan-N.md`, all run in order on `phase/p83`):

| Task | What |
|---|---|
| p83.1 | `go 1.24.0` (for `os.Root`) and the one vet fix it needs |
| p83.2 | `CapSet.Grant`: the most specific grant that covers a request |
| p83.3 | `openIn` and the grant-aware open helpers (`os.Root`) |
| p83.4 | `read_file` and `write_file` open beneath their grant, which closes the directory-swap race |
| p83.5 | net scopes accept `:port` |
| p83.6 | `http_get` and every redirect check host **and** port |
| p83.7 | CIDR grants, internal address classes; `*` stops covering internal IPs (I2) |
| p83.8 | dial-time policy (`AllowsDial`, `RequireDial`), I2 property test, `FuzzNetScope` |
| p83.9 | every dialled address is checked (SSRF, DNS rebinding); no pooled connections |
| p83.10 | notes (every changed expectation with its reason), README, usage |

**Deviations from `15-phase8-tooling.md` §8.3,** each explained in the task that makes it:
- The acceptance test uses `localhost` (system resolver, `/etc/hosts`) instead of a `rebind.test` DNS stub (p83.9).
- The check runs in `net.Dialer.Control` on each connected address, not in a resolving `DialContext` (p83.9).
- `*` does not cover internal IP literals or CIDR ranges (p83.7).
- CIDR grants take no port (p83.7).
- The fs change flips no e2e expectation, although `16-implementation-notes.md` predicted it would (p83.4).

**Files:**
- Modify: `go.mod`
- Modify: `arv/evaluator/eval_functions.go` (one line)

- [ ] **Step 1: Write the failing tests**

Bump the directive. This is the change under test; `go vet` is the test.

In `go.mod`, replace:

```text
go 1.22.5
```

with:

```text
go 1.24.0
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p83 && go vet ./...`

Expected:
```
arv/evaluator/eval_functions.go:184:18: non-constant format string in call to github.com/delavalom/arvlang/arv/evaluator.newError
```
(The line number may differ by a few lines.)

- [ ] **Step 3: Implement**

In `arv/evaluator/eval_functions.go`, replace:

```go
	return newError("identifier not found: " + node.Value)
```

with:

```go
	return newError("identifier not found: %s", node.Value)
```

- [ ] **Step 4: Run the checks and watch them pass**

```bash
cd /Users/delavalom/delavalom-labs/arvlang-wt/p83 && gofmt -l . && go vet ./... && GOOS=windows go vet ./... && go test -race -count=1 ./...
```

Expected: `gofmt` prints nothing, both vets are clean, and every package says `ok` (`arv/token` has no test files).

- [ ] **Step 5: Commit**

```bash
cd /Users/delavalom/delavalom-labs/arvlang-wt/p83 && git add arv/evaluator/eval_functions.go go.mod && git commit -F - <<'EOF'
build: raise go directive to 1.24.0 for os.Root

os.Root first shipped in Go 1.24. A 1.24 module makes go vet reject
non-constant format strings, so evalIdentifier now passes the name as
an argument.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
EOF
```
