# Phase 8.5, task p85.4: notes and the full check

> **For agentic workers:** this file is one task, p85.4. Do it exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: p85.1 to p85.3 are committed on `phase/p85` in `/Users/delavalom/delavalom-labs/arvlang-wt/p85`. Start every shell command with `cd /Users/delavalom/delavalom-labs/arvlang-wt/p85 &&`. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green (first check `df -h /System/Volumes/Data`: below 1 GB free, stop and report "blocked: disk"; the only thing you may delete is the build cache, `go clean -cache`). End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify`, never push, never tag, never run `goreleaser`. Keep markdown under 18 KB (check with `wc -c`). Create and edit files with the Write and Edit tools, not shell heredocs. The phase overview, guarantees D1-D6 and decisions are in `p85-plan-1.md`.

## Task p85.4: notes and the full check

No code changes. If a step below fails, that is a bug in p85.1 to p85.3: stop and report it with the output.

**Files:**
- Create: `docs/superpowers/plans/2026-11-09-arv-runtime/16p85-distribution.md`
- Modify: `docs/superpowers/plans/2026-11-09-arv-runtime/16o-phase-index.md`, `docs/superpowers/plans/2026-11-09-arv-runtime/16-implementation-notes.md`

- [ ] **Step 1: The make targets, from a clean tree**

```bash
cd /Users/delavalom/delavalom-labs/arvlang-wt/p85 && df -h /System/Volumes/Data && git status --short && make build && bin/arv version && bin/arv check examples/fanout.arv; rm -rf bin
```

Expected: `git status` prints nothing; `bin/arv version` prints `arv (devel)`, `commit unknown` and `go1.26.2 darwin/arm64` (a worktree is not stamped, decision 1); `arv check` prints `--allow net=api.github.com` and its `#` line.

```bash
cd /Users/delavalom/delavalom-labs/arvlang-wt/p85 && make test 2>&1 | tail -25 && make e2e 2>&1 | tail -3
```

Expected: every package `ok` (no package `github.com/delavalom/arvlang`; `cmd/arv` listed), then `ok  github.com/delavalom/arvlang/e2e`. Write down the durations of `cmd/arv` and `e2e` for the notes.

- [ ] **Step 2: The tree-walker suite**

```bash
cd /Users/delavalom/delavalom-labs/arvlang-wt/p85 && make test-walk 2>&1 | grep -v '^ok\|no test files' | tail -20; echo "exit ${PIPESTATUS[0]}"
```

Expected: `exit 0` and nothing else printed (the e2e half builds `cmd/arv` with `-tags arv_treewalk`; `TestP85VersionFromGit` builds with the same tags).

- [ ] **Step 3: Release targets and a CGO-off e2e run**

```bash
cd /Users/delavalom/delavalom-labs/arvlang-wt/p85 && for t in darwin/amd64 darwin/arm64 linux/amd64 linux/arm64; do CGO_ENABLED=0 GOOS=${t%/*} GOARCH=${t#*/} go build -trimpath -o /dev/null ./cmd/arv && echo "$t ok"; done
```

Expected: four `ok` lines.

Release binaries are CGO-off (decision 6). Run the e2e suite once on a CGO-off binary (`-race` needs cgo, so this run has none):

```bash
cd /Users/delavalom/delavalom-labs/arvlang-wt/p85 && time CGO_ENABLED=0 go test -count=1 ./e2e/ 2>&1 | tail -15
```

Expected: `ok`. Write down the result and the time. If a test fails here and passes in `make e2e`, do not fix it in this phase: record the test, its output and the reason as a known limit of release builds in the notes, and report it.

- [ ] **Step 4: The notes**

Create `docs/superpowers/plans/2026-11-09-arv-runtime/16p85-distribution.md` with the text below. Replace each `[...]` with what you measured in steps 1 to 3 (the planner's scratch numbers are given as a guide).

```markdown
# Phase 8.5: distribution

Built from `p85-plan-1.md` to `p85-plan-4.md` (design: section 8.5 of `15-phase8-tooling.md`). Nothing was released, pushed or tagged.

## What was built

- **The command is in `cmd/arv`.** The 21 `.go` files of the repository root moved there (`git mv`, so history follows them); the module root holds no Go package. `go install github.com/delavalom/arvlang/cmd/arv@latest` names the binary `arv`. The moved tests read the examples from `../../examples`, and `TestAgentExample` runs from the repository root (`t.Chdir("../..")`), where `examples/agent.arv`'s paths are relative.
- **Every build follows it:** the e2e harness and the Phase 8.4 engine binaries build `cmdArv` (`e2e/harness_test.go`), `bench/run.sh` and `bench/escape/probe.sh` build `./cmd/arv` (their tests in `go test ./bench/...` run them), and `make build` writes `$(BIN)/arv` (`BIN` defaults to `bin`). `make install` is new.
- **`arv version`** (`cmd/arv/version.go`) prints `arv VERSION`, `commit SHA` (with ` (modified)` for a changed tree) or `commit unknown`, and `GOVERSION GOOS/GOARCH`. Sources in order: the link-time variables `main.version` and `main.commit`; `debug.ReadBuildInfo` (`Main.Version`, `vcs.revision`, `vcs.modified`); the 12-digit commit in a pseudo-version (`go install pkg@<commit>`). `(modified)` is shown only when the commit printed is the stamped one.
- **`.goreleaser.yaml`** (v2): `./cmd/arv` as `arv`, darwin and linux, amd64 and arm64, `CGO_ENABLED=0`, `-trimpath`, `-s -w -X main.version=v{{ .Version }} -X main.commit={{ .FullCommit }}`, tar.gz archives with the README, checksums, draft releases. `dist/` is ignored.

## Guarantees and their tests

| Guarantee | Tests |
|---|---|
| D1 builds from `cmd/arv` only | `TestP85CommandInCmdArv` (`go list`: package main, target `arv`; the root is not a package) |
| D2 make targets | `TestP85MakeBuild` (`make build BIN=tmp`, then the binary runs `basics.arv` as the suite's does); `make test`, `make e2e`, `make test-walk` run in p85.4 |
| D3 e2e and bench build the real binary | the whole e2e suite (its `TestMain` fails if `cmdArv` does not build); `bench.TestRunScript`, `escape.TestProbeScript` |
| D4 version information | `TestVersionText` (11 cases), `TestVersionCommand`, `TestP85VersionOfSuiteBinary`, `TestP85VersionFromGit` (fresh repository: clean HEAD, then `(modified)` and `+dirty`) |
| D5 goreleaser wiring | `TestP85ReleaseBuild` (builds with the config's `main`, `flags`, `ldflags` and CGO off; prints `v9.8.7` and the given SHA) |

Mutation checks run by the plan (each caught): `(modified)` without `rev == vcsRev`; `pseudoRevision` always `""`; link-time variables ignored; `-X main.Commit`; `main: .`; `{{ .Version }}` without the `v`.

Measured: `cmd/arv` tests [about 11 s], e2e [about 24 s] with `-race` (load average [about 25]); `TestP85VersionFromGit` [about 11 s] on its first run; the four cross builds [all ok]; the e2e suite on a CGO-off binary [ok, about 12 s].

## Deviations from 15-phase8-tooling.md, 8.5

- `arv version` prints the version and the toolchain as well as the SHA, and `commit unknown` where there is none to print.
- goreleaser is configured, not run: it is not installed here and nothing may be downloaded or published. `TestP85ReleaseBuild` builds as it would for the host platform, and p85.4 cross-built the four targets with the same flags. Releases are drafts.
- Added `make install` and `BIN` for `make build`.

## Known limits

- **A git worktree is not stamped.** The go command (go1.26.2) looks for a `.git` directory; a worktree's `.git` is a file, so a build there, `make build` included, prints `arv (devel)` and `commit unknown`, even with `-buildvcs=true`. A plain clone is stamped. `go run` never stamps.
- **`go install` of a tag prints `commit unknown`:** the module version is all the go command records. `go install` of a commit prints 12 hex digits, not 40.
- **`goreleaser check` was never run**, so a key goreleaser rejects would be found only when someone runs it. The keys used are the documented v2 ones (`version: 2`, `archives.formats`, `snapshot.version_template`).
- **Release builds are CGO-off** and use Go's own DNS resolver; `make e2e` tests the cgo default on macOS. [The whole e2e suite passed once on a CGO-off binary (`CGO_ENABLED=0 go test -count=1 ./e2e/`, no `-race`).]
```

- [ ] **Step 5: Pointers**

In `16o-phase-index.md`, append at the end of the file:

```markdown

## Phase 8.5: distribution

Notes, guarantees and their tests, deviations and known limits: [`16p85-distribution.md`](16p85-distribution.md). Plans: `p85-plan-1.md` to `p85-plan-4.md`. The command is in `cmd/arv`; `arv version` prints the commit; `.goreleaser.yaml` builds macOS and Linux binaries (not run).
```

In `16-implementation-notes.md`, the bullet (under `- **Phase 8 (tooling and hardening):**`)

```markdown
  - Moving `main.go` to `cmd/arv` breaks the e2e build target, the `Makefile` and the README.
```

becomes

```markdown
  - Done in Phase 8.5 ([`16p85`](16p85-distribution.md)): `main.go` is in `cmd/arv`; the e2e build, the `Makefile` and the README follow it.
```

Change nothing else in that file. Check sizes:

```bash
cd /Users/delavalom/delavalom-labs/arvlang-wt/p85 && wc -c docs/superpowers/plans/2026-11-09-arv-runtime/16p85-distribution.md docs/superpowers/plans/2026-11-09-arv-runtime/16o-phase-index.md docs/superpowers/plans/2026-11-09-arv-runtime/16-implementation-notes.md
```

Expected: each under 18432 bytes (about 4.4 KB, 9.7 KB and 18.2 KB in the scratch copy).

- [ ] **Step 6: Check and commit**

```bash
cd /Users/delavalom/delavalom-labs/arvlang-wt/p85 && gofmt -l . && go vet ./... && git add -A && git status --short && git commit -m "docs(notes): Phase 8.5 distribution: what was built, tests, deviations, known limits

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

`git status --short` must list only the three notes files.
