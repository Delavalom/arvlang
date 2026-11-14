# Phase 8.5, task p85.1: the command moves to cmd/arv

> **For agentic workers:** this file is one task, p85.1. Do it exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: your worktree `/Users/delavalom/delavalom-labs/arvlang-wt/p85` is on branch `phase/p85`, branched from `feat/phases-3-8` at d550380, with only the `p85-plan-*.md` files added. Start every shell command with `cd /Users/delavalom/delavalom-labs/arvlang-wt/p85 &&`. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green (first check `df -h /System/Volumes/Data`: below 1 GB free, stop and report "blocked: disk"; the only thing you may delete is the build cache, `go clean -cache`). End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify`, never push, never tag. Keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0`; the toolchain is 1.26.2. Create and edit files with the Write and Edit tools, not shell heredocs; a `Makefile` recipe line starts with a tab.
>
> Every code block in `p85-plan-1.md` to `p85-plan-4.md` was applied, in task order, to a scratch copy of the tree (`rsync -a --exclude .git`, then `git init` there) and passed `gofmt -l`, `go vet ./...` and `go test -race` on the packages it touches after each task, and the whole suite (`go test -race ./...`, e2e and bench included) after the last. Design: section 8.5 of `15-phase8-tooling.md`.

## Phase 8.5 overview

**Goal:** `go install github.com/delavalom/arvlang/cmd/arv@latest` gives a binary named `arv`; `arv version` prints the git SHA it was built from; goreleaser can build macOS and Linux binaries. Nothing is released, pushed or tagged.

**Guarantees (each task's tests name the ones they cover):**
- D1. The binary builds from `cmd/arv`, and only from there: the module root holds no Go package.
- D2. `make build`, `make test` and `make e2e` work; `make build` honours `BIN` so a test can build into a temp dir.
- D3. The e2e suite builds the real binary from `cmd/arv` (both engines' binaries of Phase 8.4 too), and so do the bench scripts (`bench/run.sh`, `bench/escape/probe.sh`), which `go test ./bench/...` runs.
- D4. Version information is correct: `arv version` prints the version, the commit and the Go toolchain. A release build prints what its link-time variables say; a build in a git checkout prints the commit HEAD names (and `(modified)` when the tree has changes); `go install` of a commit prints its 12-digit prefix; anything else prints `commit unknown`, never a guess.
- D5. `.goreleaser.yaml` builds `./cmd/arv` as `arv` for darwin and linux, amd64 and arm64, and its ldflags reach the variables `arv version` prints (`-X` on a missing symbol is silently ignored, so a test builds with the config's own ldflags).
- D6. `arv/runtime` is not touched; no secret moves; no run's behaviour changes.

| Task | File | What |
|---|---|---|
| p85.1 | `p85-plan-1.md` | move the 21 root `.go` files to `cmd/arv`; fix their example paths; e2e, `p84` helpers, `Makefile`, bench scripts, README |
| p85.2 | `p85-plan-2.md` | `arv version`: `cmd/arv/version.go`, unit tests, e2e tests (the suite's binary, and a fresh git repository) |
| p85.3 | `p85-plan-3.md` | `.goreleaser.yaml`, a test that builds as it does, `dist/` ignored, README |
| p85.4 | `p85-plan-4.md` | notes (`16p85-distribution.md`), index pointer, full verification, cross builds, a CGO-off e2e run |

### Decisions, and why

1. **The git worktree problem.** The go command stamps `vcs.revision` only when the directory holding `.git` is a *directory*; in a git worktree `.git` is a file, so nothing is stamped (probed in this worktree with go1.26.2, even with `-buildvcs=true`: no `vcs.*` settings, `Main.Version` is `(devel)`; the same probe in a fresh `git init` repository stamps `vcs.revision`, `vcs.modified` and a pseudo-version). Every agent works in a worktree, so the e2e test of D4 copies the build's sources into a fresh temp repository, commits them, and builds there. The suite's own binary is checked too, but accepts `commit unknown`.
2. **Version sources, in order:** the link-time variables `main.version` and `main.commit` (goreleaser), then `debug.ReadBuildInfo` (`Main.Version`, `vcs.revision`, `vcs.modified`), then the commit prefix inside a pseudo-version (what `go install pkg@<commit>` stamps). Parsing a pseudo-version by hand keeps `cmd/arv` free of `golang.org/x/mod`; the module has no dependencies.
3. **Output:** three lines, `arv VERSION`, `commit SHA[ (modified)]` or `commit unknown`, and `GOVERSION GOOS/GOARCH`. Plain lines a script can read; no flags.
4. **`(modified)` is shown only when the commit printed is the stamped one**, so a release build's commit is never marked by a checkout's state.
5. **goreleaser is not installed** on this machine and nothing may be downloaded or published, so `goreleaser check` cannot run. The config uses the documented v2 keys only, and `TestP85ReleaseBuild` builds exactly as it does (its `main`, `flags`, `ldflags`, `CGO_ENABLED=0`) for the host platform. The four cross targets are built once by hand in p85.4.
6. **Release builds are CGO-off** (`CGO_ENABLED=0`), so they use Go's own DNS resolver; the suite's binaries use the platform default. p85.4 runs the e2e suite once on a CGO-off binary and records the result.
7. **No `--version` flag and no build date.** The design asks for `arv version` and the SHA; a date is not needed to identify a build and would make two builds of one commit differ.
8. **Notes go in a new file, `16p85-distribution.md`**, with a pointer in `16o-phase-index.md`. In `16-implementation-notes.md` (18 KB) only the follow-through bullet on `cmd/arv` changes, to point there.

### Deviations from the design (15-phase8-tooling.md, 8.5)

- `arv version` prints the version and the toolchain as well as the SHA, and prints `commit unknown` where no SHA exists (a worktree build, `go install` of a tag).
- goreleaser is configured but not run (decision 5); releases are drafts (`release: draft: true`) so a future `goreleaser release` publishes nothing by itself.
- Added: `make install` and `BIN` in `make build`.

## Task p85.1: the command moves to cmd/arv

Covers D1, D2, D3.

**Files:**
- Move (`git mv`): every `.go` file in the repository root to `cmd/arv/`: `check_cli.go`, `check_cli_round1_test.go`, `check_cli_round2_test.go`, `check_cli_test.go`, `example_agent_test.go`, `example_test.go`, `main.go`, `main_agent_test.go`, `main_limits_test.go`, `main_llm_log_test.go`, `main_llm_test.go`, `main_log.go`, `main_log_round3_test.go`, `main_log_test.go`, `main_stall_test.go`, `main_test.go`, `manifest_cli.go`, `manifest_cli_test.go`, `manifest_leak_test.go`, `uptime_ergonomic_test.go`, `uptime_example_test.go` (21 files)
- Modify: `cmd/arv/example_test.go`, `cmd/arv/uptime_example_test.go`, `cmd/arv/uptime_ergonomic_test.go`, `cmd/arv/example_agent_test.go`, `e2e/harness_test.go`, `e2e/p84_helpers_test.go`, `Makefile`, `bench/run.sh`, `bench/escape/probe.sh`, `README.md`
- Create: `e2e/p85_dist_test.go`

- [ ] **Step 1: Write the failing tests**

Create `e2e/p85_dist_test.go`:

```go
package e2e

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The command is package main in cmd/arv, so go install names its binary
// arv, and the module root holds no Go package.
func TestP85CommandInCmdArv(t *testing.T) {
	out, err := exec.Command("go", "list", "-f", "{{.Name}} {{.Target}}", cmdArv).CombinedOutput()
	if err != nil {
		t.Fatalf("go list %s: %v\n%s", cmdArv, err, out)
	}
	name, target, _ := strings.Cut(strings.TrimSpace(string(out)), " ")
	if name != "main" || filepath.Base(target) != "arv" {
		t.Fatalf("go list: package %q installs as %q, want package main installed as arv", name, target)
	}
	root, err := exec.Command("go", "list", "github.com/delavalom/arvlang").CombinedOutput()
	if err == nil {
		t.Fatalf("the module root is still a package:\n%s", root)
	}
}

// make build writes the binary built from cmd/arv; BIN moves it out of the
// checkout so the test leaves the tree as it was.
func TestP85MakeBuild(t *testing.T) {
	if _, err := exec.LookPath("make"); err != nil {
		t.Skip("make is not installed")
	}
	bin := t.TempDir()
	cmd := exec.Command("make", "-s", "build", "BIN="+bin)
	cmd.Dir = ".."
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("make build: %v\n%s", err, out)
	}
	made := run(t, spec{script: "basics.arv", bin: filepath.Join(bin, "arv")})
	want := run(t, spec{script: "basics.arv"})
	if made.code != 0 || made.stdout != want.stdout || made.stderr != "" {
		t.Fatalf("the binary make built differs from the suite's\n=== make build ===\n%s\n=== suite ===\n%s", made, want)
	}
}
```

`cmdArv` does not exist yet; add it to `e2e/harness_test.go` now, just above `func TestMain`:

```go
// cmdArv is the package of the arv command, which every binary the suite
// builds comes from (Phase 8.5: go install github.com/delavalom/arvlang/cmd/arv@latest).
const cmdArv = "github.com/delavalom/arvlang/cmd/arv"
```

- [ ] **Step 2: Run them and watch them fail**

```bash
cd /Users/delavalom/delavalom-labs/arvlang-wt/p85 && go vet ./e2e/ && go test -count=1 -run TestP85 ./e2e/
```

Expected: FAIL. `TestP85CommandInCmdArv`: `no required module provides package github.com/delavalom/arvlang/cmd/arv`. `TestP85MakeBuild`: the old `make build` ignores `BIN` and writes `bin/arv` in the checkout (gitignored), so running `$BIN/arv` fails (`no such file or directory`). Delete that `bin/arv`: `rm -f bin/arv`.

- [ ] **Step 3: Move the files**

```bash
cd /Users/delavalom/delavalom-labs/arvlang-wt/p85 && mkdir -p cmd/arv && git mv *.go cmd/arv/ && ls *.go 2>/dev/null; ls cmd/arv | wc -l
```

Expected: no `.go` file left in the root; `21`.

- [ ] **Step 4: Fix the paths in the moved tests**

The tests now run in `cmd/arv`. In `cmd/arv/example_test.go`, `cmd/arv/uptime_example_test.go` and `cmd/arv/uptime_ergonomic_test.go`, every string literal that starts `"examples/` becomes `"../../examples/` (6 places: `fanout.arv` once, `uptime.arv` twice in `uptime_example_test.go`, and in `uptime_ergonomic_test.go` `uptime.arv` twice and `uptime-ergonomic.arv` twice, two of them on the `count(...)` line). Afterwards `grep -n '"examples/' cmd/arv/*.go` lists only `example_agent_test.go`.

`examples/agent.arv` reads `examples/weather.txt` relative to the working directory, as its header documents, so `TestAgentExample` runs from the repository root instead. In `cmd/arv/example_agent_test.go`, make the first line of `func TestAgentExample(t *testing.T) {` this:

```go
	t.Chdir("../..") // the example's paths are relative to the repository root
```

(`t.Chdir` exists since Go 1.24; the test does not call `t.Parallel`.)

- [ ] **Step 5: Point every build at cmd/arv**

`e2e/harness_test.go`, in `runMain`: `exec.Command("go", append(buildArgs, "github.com/delavalom/arvlang")...)` becomes `exec.Command("go", append(buildArgs, cmdArv)...)`.

`e2e/p84_helpers_test.go`: `exec.Command("go", append(args, "github.com/delavalom/arvlang")...)` becomes `exec.Command("go", append(args, cmdArv)...)`.

`bench/run.sh` (line 35) and `bench/escape/probe.sh` (line 24): `go build -o "$BIN/arv" . &&` becomes `go build -o "$BIN/arv" ./cmd/arv &&`.

`Makefile`: the first lines, up to the `build` recipe, become (keep the tabs; the rest of the file is unchanged):

```make
.PHONY: build install test test-walk e2e bench-test bench escape bench-cpu

# Where make build writes the binary (the e2e suite points it at a temp dir).
BIN ?= bin

# The command lives in cmd/arv (Phase 8.5), so go install names it arv.
build:
	go build -o $(BIN)/arv ./cmd/arv

install:
	go install ./cmd/arv
```

Check: `grep -rn 'arvlang")\.\.\.\|BIN/arv" \. \|bin/arv \.$' --include='*.go' --include='*.sh' --include=Makefile .` prints nothing (before this step it listed the `Makefile`, both bench scripts and both e2e build lines).

- [ ] **Step 6: README**

In `README.md`, insert before `## Grants`:

````markdown
## Install

```bash
go install github.com/delavalom/arvlang/cmd/arv@latest   # the arv binary, into $GOBIN or $(go env GOPATH)/bin
```

From a checkout, `make build` writes `bin/arv`. The examples below run from the repository root.

````

and in `## Development`, the line `make build   # go build -o bin/arv .` becomes these two lines:

```text
make build   # go build -o bin/arv ./cmd/arv
make install # go install ./cmd/arv, into $GOBIN or $(go env GOPATH)/bin
```

- [ ] **Step 7: Run the tests and watch them pass**

```bash
cd /Users/delavalom/delavalom-labs/arvlang-wt/p85 && gofmt -l . && go vet ./... && go test -race -count=1 ./cmd/arv/ && go test -race -count=1 -run 'TestP85|TestP84' ./e2e/
```

Expected: `ok` for both (`cmd/arv` took about 10 s in the scratch copy). Then `make build && ls -l bin/arv && rm -rf bin`: the binary is built from `cmd/arv`.

- [ ] **Step 8: The whole suite, bench included, and commit**

```bash
cd /Users/delavalom/delavalom-labs/arvlang-wt/p85 && df -h /System/Volumes/Data && go test -race ./...
```

Expected: every package `ok`, including `github.com/delavalom/arvlang/bench` and `.../bench/escape` (they run `run.sh` and `probe.sh`, which build `./cmd/arv`) and `.../cmd/arv`. There is no package `github.com/delavalom/arvlang` any more.

```bash
cd /Users/delavalom/delavalom-labs/arvlang-wt/p85 && git add -A && git status --short && git commit -m "refactor(cli): move the arv command to cmd/arv

go install github.com/delavalom/arvlang/cmd/arv@latest now gives a binary
named arv. The e2e suite, the Phase 8.4 engine binaries, the bench scripts
and make build all build ./cmd/arv; make build takes BIN, and make install
is new. The moved tests read the examples from ../../examples.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

`git status --short` must show the 21 renames (`R`), the modified files above and `A e2e/p85_dist_test.go`, nothing else.
