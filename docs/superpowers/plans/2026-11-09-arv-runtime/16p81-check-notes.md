# Phase 8.1: `arv check` (as built)

Built from `p81-plan-1.md` to `p81-plan-11.md`; their deviations from `15-phase8-tooling.md` §8.1 are listed in `p81-plan-1.md`. Review round 1, its known limits and the mutation table: [`16p81b-check-round1.md`](16p81b-check-round1.md). Review round 2: [`16p81c-check-round2.md`](16p81c-check-round2.md). E2E, fuzzing and leak checks: [`16p81d-check-hardening.md`](16p81d-check-hardening.md).

## What it is

- `arv/check` (new package): `Source(src)` and `Program(prog)` return a `Report`: `Needs` (the least set of grants, sorted by kind then scope), `Sites` (every call that needs a grant, in source order) and `Notes` (calls whose need is not known before running). `Manifest`, `Grants` and `Outside` read and apply a `// arv:allow kind=scope` header.
- `arv check FILE` prints one `--allow kind=scope` line per need (quoted `$'...'`, the bash/zsh/ksh form, when the scope has unusual or glob characters, so it is always one word and one line), then one `# fn at line:col: why` line per note. If the file has an `arv:allow` header, a need outside it is an error (exit 1).
- `arv run --manifest FILE` grants exactly the header's lines (resolved like `--allow`, against the working directory) and refuses to run, before anything is evaluated, if `arv check` finds a need outside them. It cannot be combined with `--allow` (exit 2).
- `evaluator.BuiltinNames()` (new, additive) lists every builtin; `arv/check` classifies each one as an effect, a constructor or pure.

## What it guarantees, and how precisely

- **It never runs the program.** It parses and walks the syntax tree. `arv check` reads nothing but the file (and the working directory's name, as the evaluator does, to resolve a relative path; round 2), except when the file has an `arv:allow` header: then it canonicalises the header's paths and the needed paths as `--allow` does (lstat and readlink), to compare them as the runtime would. It never opens a granted file, connects, or reads an environment variable.
- **Sound for what the program text requests.** Admission happens only in the effects (`http_get`, `read_file`, `write_file`, `getenv`) and at `spawn ... with`, whose capabilities only the constructors (`net`, `fs_read`, `fs_write`, `env`) create. Every occurrence of those names is classified: a direct call with a known argument is a need; any other call or use (a builtin passed as a value, an unknown argument, a wrong argument count) is a note. A builtin value can only be reached through such an occurrence, so no admission escapes the report except where a note says so. A user binding that shadows a builtin's name is treated as the builtin: that can only add needs.
- **Known arguments** are string literals, names bound exactly once in the whole program, by a `let` of a known string, and `+` of two known strings. Folding is bounded (8 KiB per string, 1 MiB in all, 100 names deep); past that the call is a note.
- **Every syntax form is searched**, including code that never runs: scope options, `supervise`, `match` arms, `for`, hash keys, function bodies. A node type or field added to `arv/ast` later fails `TestChildrenKnowsEveryNodeType` or `TestNodeFieldsAreKnown`; a builtin added later fails `TestEveryBuiltinIsClassified`; at run time an unknown node stops the check with an error and an unclassified builtin is a note.
- **Least set.** Net and env needs that another need covers are dropped, decided by `runtime.Capability.Covers`. Paths are printed as written and only exact duplicates are dropped, since symlinks decide whether one path lies under another.

## Known limits

- **Redirects.** `http_get` checks every redirect at run time; the hosts a server redirects to cannot be known before running and are not reported.
- **Names that resolve to internal addresses.** `http_get("http://localhost:8080/")` needs `net=localhost:8080` and, at dial time, an IP grant such as `net=127.0.0.1:8080`. `arv check` reports the first only, since resolving the name would be an effect. A numeric name a resolver may read as an IP address (`127.1`, `2130706433`) also gets a note. For an IP literal the dial need is reported (round 1): `0.0.0.0:P` also needs `127.0.0.1:P`, and `[::]:P` needs `[::1]:P`.
- **Dead code is reported.** A call that never runs still prints its need; a minimal set for one run would need the run.
- **Custom host effects (Phase 8.2)** are registered through `arv.Options`, which the CLI's check does not see. A Go host that adds effects should classify them itself.
- **Notes do not stop `--manifest`.** The run still holds only the header's grants, so the runtime denies anything outside them; a note only means `arv check` could not prove it in advance.

## Merge notes for the phases built in parallel (5E, 6, 7, 8.2)

- A new builtin (ergonomics helpers, Phase 7's `llm`, `tool`, `ask`, `agent`) fails `TestEveryBuiltinIsClassified` until it is added to `effects` (with its kind) or `pure` in `arv/check/builtins.go`. A new capability kind should also get a place in `rank` (`check.go`).
- A new AST node or field (if ergonomics adds one instead of desugaring) fails the two `walk_test.go` tests until `children` and the binding count in `consts.go` handle it.
- If `examples/uptime.arv` or `examples/fanout.arv` changes, update the expected output in `e2e/p81_check_test.go` (`TestP81ExamplesCheck`) from `arv check`'s actual output, after checking it by hand.
- `main.go` gains five small hunks (two in the usage text, the `check` dispatch, and `splitManifest`/`applyManifest` around `parseRun`). The functions themselves live in the new files `check_cli.go` and `manifest_cli.go`.
