# Phase 8.1: `arv check`, e2e and hardening

Continues [`16p81c-check-round2.md`](16p81c-check-round2.md). This round added tests only. No product code changed. Measured on this shared machine at load averages between 14 and 51.

## E2E (real binary)

`e2e/p81_check_test.go` (examples acceptance, never runs, `run --manifest`, usage) is joined by:

- `e2e/p81_accept_test.go`:
  - **Every syntax form.** Scope options, `supervise` restart and collect, both `match` arms, `spawn ... with`, `for`, hash literals, a dead branch and an uncalled function are all reported. Nothing runs: the server sees no request, no file is made, and the program's `puts` never prints.
  - **Notes.** A run-time argument, a builtin used as a value and a wrong argument count each give a note. Folded constants are still needs.
  - **Misuse.** Exit 2 for usage errors. Exit 1 for a missing file or a parse error, with the same message `run` and `run --manifest` print.
  - **Round trip.** `arv check`'s printed lines, passed to `arv run`, run the program with every kind of grant (net through a spawn, fs_read, fs_write, env). Leaving any one line out is denied before the denied effect happens (I1).
  - **Secrets.** With the variable set in the environment, `arv check` prints the variable's name and never its value.
  - **Never ends.** A program that sleeps, spins forever and waits on a gate is checked at once. The gate gets no request.
- `e2e/p81_manifest_test.go`:
  - **Refused before running.** A refusal comes before the first `puts`, before any file write and before any request.
  - **Notes denied at run time.** A note is denied when the call runs, and no request leaves (I1). A capability built at run time is denied at `spawn` (I2).
  - **No header.** Without a header, `--manifest` grants nothing. A comment that says `arv:allow` after the first line of code is not part of the header.
  - **Malformed header.** A header line with two words, or with an unknown kind, is an error for both commands.
  - **Canonical paths (I3).** A path through a symlink into the granted directory is covered and runs. A path that climbs out with `..` is refused, by both commands.
  - **Dial need.** A dial to `0.0.0.0:P` also needs `127.0.0.1:P`. If the header lacks that grant, the run is refused. If it has it, the request reaches the server.

## Fuzzing (`arv/check`, `-parallel 4`)

| Target | What it asserts | Time | Execs | Found |
|---|---|---|---|---|
| `FuzzCheck` (existing) | no panic, deterministic, one line per grant, sorted needs | 125 s | 2,993,301 | nothing |
| `FuzzCheckNoEffects` (new) | `Source`, then `Manifest`, `Grants` and `Outside` (the header comparison): a counting server gets no request, the empty working directory stays empty, and no goroutine is left behind. Every miss is a site. The report's own needs, used as a header, cover every site | 126 s | 331,812 | nothing |
| `FuzzCheckSound` (new) | for any program whose needs are known (only "never allowed" notes) and confined (relative paths without `..`, the local server by IP literal, env), a run under budgets with exactly the reported grants never requests a grant outside them | 126 s | 69,354 | two oracle defects (below), then nothing |
| `FuzzSoundGen` (new) | the generator property (`TestReportedGrantsSufficeAndAreNeeded`: exact set, sufficient, each grant needed), on fuzzed seeds and depths up to 6 | 125 s | 138,958 | nothing |

`FuzzCheckSound` grows coverage slowly, since most mutations break the program or leave the confined set. `TestAssertSoundRunsGeneratedPrograms` shows that its filter is not vacuous: the generator's programs pass it and run, and programs with a note or an absolute path are skipped.

### What `FuzzCheckSound` found (both in the test oracle; kept as corpus regressions)

1. `let un = "http://0.0.0.0:P"; spawn (fn() { http_get(un) })()`. The child holds nothing (I2), so the runtime denies `net=0.0.0.0:P`, even though the root holds the reported grants. Here the program denies itself. This is not an under-report. The oracle now reads the denied request from the error message (`deniedWant`) and asks only that a reported grant covers it.
2. `scope { await spawn (fn() { read_file("0") })() }`. The denial names the path as requested (`/var/folders/...`), while the grants are canonical (`/private/var/folders/...`). The oracle now canonicalises the request with `runtime.New` before calling `Covers`. In a denial message the path is shown uncanonicalised, as it was requested. That is cosmetic and belongs to the runtime, not this phase.

## Leak checks

- `manifest_leak_test.go` runs `arv check`, then `arv run --manifest`, in process on six programs and waits for the goroutine count to settle back. The programs end in these ways: a refusal before running, a note denied while sibling tasks sleep, a spawn denied by the header while a sibling sleeps, supervised restarts, `collect` with a failing task, and a timeout with sleeping tasks.
- `assertSound` (used by `FuzzCheckSound`) checks the same after every fuzzed run. `FuzzCheckNoEffects` checks that the check itself starts no goroutine.

## Final runs

`go test -race -count=5 ./...` passed, as did `ARV_E2E_RACE=1 go test -race ./e2e/` and `go test ./e2e/`.
