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

Measured in p85.4 (load average 10 to 29): `make build` then `bin/arv version` printed `arv (devel)`, `commit unknown`, `go1.26.2 darwin/arm64` (a worktree, see known limits), and `bin/arv check examples/fanout.arv` printed `--allow net=api.github.com` and its `#` line. `make test` all `ok`, with `cmd/arv` 10.8 s and e2e 20.6 s under `-race`; `make e2e` 15.6 s; `make test-walk` exit 0 with nothing but `ok` lines. `TestP85VersionFromGit` 2.9 s (warm build cache). The four cross builds (`CGO_ENABLED=0 go build -trimpath`, darwin and linux, amd64 and arm64) all ok. The e2e suite on a CGO-off binary: ok, 16.1 s (17.1 s wall).

## Deviations from 15-phase8-tooling.md, 8.5

- `arv version` prints the version and the toolchain as well as the SHA, and `commit unknown` where there is none to print.
- goreleaser is configured, not run: it is not installed here and nothing may be downloaded or published. `TestP85ReleaseBuild` builds as it would for the host platform, and p85.4 cross-built the four targets with the same flags. Releases are drafts.
- Added `make install` and `BIN` for `make build`.

## Known limits

- **A git worktree is not stamped.** The go command (go1.26.2) looks for a `.git` directory; a worktree's `.git` is a file, so a build there, `make build` included, prints `arv (devel)` and `commit unknown`, even with `-buildvcs=true`. A plain clone is stamped. `go run` never stamps.
- **`go install` of a tag prints `commit unknown`:** the module version is all the go command records. `go install` of a commit prints 12 hex digits, not 40.
- **`goreleaser check` was never run**, so a key goreleaser rejects would be found only when someone runs it. The keys used are the documented v2 ones (`version: 2`, `archives.formats`, `snapshot.version_template`).
- **Release builds are CGO-off** and use Go's own DNS resolver; `make e2e` tests the cgo default on macOS. The whole e2e suite passed once on a CGO-off binary (`CGO_ENABLED=0 go test -count=1 ./e2e/`, no `-race`, since `-race` needs cgo; the harness's `go build` inherits the environment).
