# Phase 8.1, task p81.11: README and implementation notes

> **For agentic workers:** this file is one task, p81.11. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: task p81.10 is committed in your tree. Start every shell command with `cd <your worktree> &&` and use absolute paths: the shell's working directory does not persist. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify` and never push; keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0` (toolchain 1.26.2): `t.Chdir`, `min` and `max` are available; do not use `testing/synctest` or `sync.WaitGroup.Go`. Before a long test run, check `df -h /System/Volumes/Data`; below 1 GB free, stop and report (only `go clean -cache` may be deleted). This machine is heavily loaded: keep wall-clock bounds generous.
>
> Every code block below was applied, in task order, to a scratch copy of `feat/phases-3-8` (860a158) and passed `gofmt -l .`, `go vet ./...` and `go test -race ./...` (including `e2e/`). Design: `15-phase8-tooling.md` §8.1. Deviations from it, and why: `p81-plan-1.md`. Phases 5E, 6, 7 and 8.2 are built at the same time and also touch the CLI, parser and evaluator: change only the files this task names.

### Task p81.11: README and implementation notes

Document what was built. `16p81-check-notes.md` is a new file named after the phase, not the next free letter, because Phases 5E, 6, 7 and 8.2 add notes files at the same time. The review rounds and the mutation table of this phase go into it later. It leaves `16-implementation-notes.md` alone: that file is already 18.2 KB, the other phases append to it too, and a `16*` file is a continuation of it by name.

**Files:**
- Modify: `README.md` (a new section before `## Budgets`)
- Create: `docs/superpowers/plans/2026-11-09-arv-runtime/16p81-check-notes.md`

- [ ] **Step 1: Write the documents**

In `README.md`, replace:

~~~~markdown
## Budgets
~~~~

with:

~~~~markdown
## Knowing the grants before running

`arv check FILE` prints the grants a program can need, without running it: one `--allow` line per grant, then a `#` line for each call whose argument it cannot know before running.

```text
$ bin/arv check examples/fanout.arv
--allow net=api.github.com
# http_get at 14:23: host not statically known
```

It looks at every call of `http_get`, `read_file`, `write_file`, `getenv`, `net`, `fs_read`, `fs_write` and `env`, wherever it is, even in code that never runs. An argument is known when it is a string literal, a name bound once in the whole program by `let` to a known string, or a `+` of known strings. A builtin passed around as a value is a `#` line too. Paths print as written, relative to the working directory, as `--allow` takes them. What a server redirects to, and the IP grant a name that resolves to an internal address also needs, are only known when the program runs.

A program can carry its grants in a header of comments before its first line of code:

```arv
// arv:allow net=api.github.com
// arv:allow fs_write=out
```

`arv run --manifest FILE` grants exactly those lines (it can't be combined with `--allow`) and refuses to start if `arv check` finds a need outside them. `arv check` on such a file exits 1 in the same case. A `#` line doesn't stop the run: the program holds the header's grants and nothing else, so anything outside them is still denied when it is tried.

## Budgets
~~~~

Create `docs/superpowers/plans/2026-11-09-arv-runtime/16p81-check-notes.md`:

~~~~markdown
# Phase 8.1: `arv check` (as built)

Built from `p81-plan-1.md` to `p81-plan-11.md`; their deviations from `15-phase8-tooling.md` §8.1 are listed in `p81-plan-1.md`. Review rounds and the mutation table are added below as they happen.

## What it is

- `arv/check` (new package): `Source(src)` and `Program(prog)` return a `Report`: `Needs` (the least set of grants, sorted by kind then scope), `Sites` (every call that needs a grant, in source order) and `Notes` (calls whose need is not known before running). `Manifest`, `Grants` and `Outside` read and apply a `// arv:allow kind=scope` header.
- `arv check FILE` prints one `--allow kind=scope` line per need (quoted `$'...'` for a shell when the scope has unusual characters, so it is always one line), then one `# fn at line:col: why` line per note. If the file has an `arv:allow` header, a need outside it is an error (exit 1).
- `arv run --manifest FILE` grants exactly the header's lines (resolved like `--allow`, against the working directory) and refuses to run, before anything is evaluated, if `arv check` finds a need outside them. It cannot be combined with `--allow` (exit 2).
- `evaluator.BuiltinNames()` (new, additive) lists every builtin; `arv/check` classifies each one as an effect, a constructor or pure.

## What it guarantees, and how precisely

- **It never runs the program.** It parses and walks the syntax tree. `arv check` reads nothing but the file, except when the file has an `arv:allow` header: then it canonicalises the header's paths and the needed paths as `--allow` does (lstat and readlink), to compare them as the runtime would. It never opens a granted file, connects, or reads an environment variable.
- **Sound for what the program text requests.** Admission happens only in the effects (`http_get`, `read_file`, `write_file`, `getenv`) and at `spawn ... with`, whose capabilities only the constructors (`net`, `fs_read`, `fs_write`, `env`) create. Every occurrence of those names is classified: a direct call with a known argument is a need; any other call or use (a builtin passed as a value, an unknown argument, a wrong argument count) is a note. A builtin value can only be reached through such an occurrence, so no admission escapes the report except where a note says so. A user binding that shadows a builtin's name is treated as the builtin: that can only add needs.
- **Known arguments** are string literals, names bound exactly once in the whole program, by a `let` of a known string, and `+` of two known strings. Folding is bounded (8 KiB per string, 1 MiB in all, 100 names deep); past that the call is a note.
- **Every syntax form is searched**, including code that never runs: scope options, `supervise`, `match` arms, `for`, hash keys, function bodies. A node type or field added to `arv/ast` later fails `TestChildrenKnowsEveryNodeType` or `TestNodeFieldsAreKnown`; a builtin added later fails `TestEveryBuiltinIsClassified`; at run time an unknown node stops the check with an error and an unclassified builtin is a note.
- **Least set.** Net and env needs that another need covers are dropped, decided by `runtime.Capability.Covers`. Paths are printed as written and only exact duplicates are dropped, since symlinks decide whether one path lies under another.

## Known limits

- **Redirects.** `http_get` checks every redirect at run time; the hosts a server redirects to cannot be known before running and are not reported.
- **Names that resolve to internal addresses.** `http_get("http://localhost:8080/")` needs `net=localhost:8080` and, at dial time, an IP grant such as `net=127.0.0.1:8080`. `arv check` reports the first only, since resolving the name would be an effect.
- **Dead code is reported.** A call that never runs still prints its need; a minimal set for one run would need the run.
- **Custom host effects (Phase 8.2)** are registered through `arv.Options`, which the CLI's check does not see. A Go host that adds effects should classify them itself.
- **Notes do not stop `--manifest`.** The run still holds only the header's grants, so the runtime denies anything outside them; a note only means `arv check` could not prove it in advance.

## Merge notes for the phases built in parallel (5E, 6, 7, 8.2)

- A new builtin (ergonomics helpers, Phase 7's `llm`, `tool`, `ask`, `agent`) fails `TestEveryBuiltinIsClassified` until it is added to `effects` (with its kind) or `pure` in `arv/check/builtins.go`. A new capability kind should also get a place in `rank` (`check.go`).
- A new AST node or field (if ergonomics adds one instead of desugaring) fails the two `walk_test.go` tests until `children` and the binding count in `consts.go` handle it.
- If `examples/uptime.arv` or `examples/fanout.arv` changes, update the expected output in `e2e/p81_check_test.go` (`TestP81ExamplesCheck`) from `arv check`'s actual output, after checking it by hand.
- `main.go` gains three small hunks (the `check` dispatch, `splitManifest`/`applyManifest` around `parseRun`, and two usage lines).
~~~~

- [ ] **Step 2: Check**

Run: `wc -c README.md docs/superpowers/plans/2026-11-09-arv-runtime/16*.md` (every markdown file under 18 KB), then `gofmt -l .`, `go vet ./...` and `go test -race ./...`.

- [ ] **Step 3: Commit**

```bash
git add README.md docs
git commit -m "docs: arv check and run --manifest" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
