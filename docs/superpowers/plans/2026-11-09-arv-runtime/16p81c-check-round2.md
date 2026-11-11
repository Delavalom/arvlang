# Phase 8.1: `arv check`, review round 2

Continues [`16p81b-check-round1.md`](16p81b-check-round1.md). One Important and three Minor findings. Each fix was test-first: the regression test failed for the reason named, then passed. Measurements were taken on this shared machine at load averages between 20 and 90.

## Fixed

| Finding | Fix | Regression test |
|---|---|---|
| Important: `Outside` was O(H x S), H header lines times S distinct needs. It called `CapSet.Admit` once per site. `Admit` scans every grant and, on a denial, copies all of them into `DeniedError.Held`. 16000 `env` header lines with 16000 `net` needs outside them: `run --manifest --timeout 1s` took 7.9 s. 16000 `net` lines covering 16000 needs took 14.5 s. Both ran before `--timeout` starts | `grantIndex` (`arv/check/grantindex.go`) holds the header's grants by kind and scope. For each site it asks `Covers` (through the `covers` variable) only about grants that can cover it. For a path, that is a grant of `/`, of the path itself, or of an ancestor, looked up only at the scope lengths that some grant has. For net and env it is the scopes `coverers` lists, the same lookup `least` uses. The decision is `Admit`'s. `runtime.New` canonicalises the path (what `Admit`'s walk computes when nothing is charged), and the request is allowed if a grant covers the result. If canonicalising fails at a component that a grant covers, the OS error is returned, as `Admit` returns its `*CanonicalError`. Any other failure is a plain miss | `TestOutsideIsNotQuadratic` (at most 4 `covers` calls per site; 0 before, since `Admit` bypassed it), `TestOutsideMemoryIsLinear` (bytes allocated per site with 1000 and with 8000 grants: 17858 against 132637 before, flat now), and `TestOutsideMatchesAdmit`, which compares `Outside` on 3000 random grant and site sets with the old `Admit`-against-every-grant version. Those sets cover net (CIDR, IPv6, mapped, `*.` suffixes, ports), env, and paths through symlinks, a symlink loop, a file and `missing/..`. The test fails if the index drops ancestor lookups, the root grant, CIDR prefix lengths, the canonical-error branch or its error |
| Minor (case 2): a relative path under 4096 bytes as written but over 4096 once the working directory is prepended. The evaluator refuses it before admission. `arv check` still printed it as a need, `--allow` refused the printed grant, and a header of `fs_read=/` failed `arv check` and `run --manifest` | `analysis.resolvedLen` (`check.go`) resolves a relative path against `os.Getwd()`, as the evaluator's `absPath` does, and applies `runtime.CheckScopeLen`. A path that is too long becomes a note: `never allowed: fs_read: scope is 4097 bytes long (max 4096)` | `TestRelativePathTooLongOnceResolvedIsANote`, `TestManifestAcceptsAPathTheRuntimeNeverAdmits` |
| Minor: notes and refusals printed raw U+202E, a UTF-8-encoded C1 CSI (U+009B) and similar characters taken from the program, in the numeric-name note and in the lstat path of a "cannot resolve" refusal | `oneLine` (`check_cli.go`) now replaces with `?` every character that `unicode.IsPrint` rejects: C0 and C1 controls, bidi controls, zero-width characters, and every space except U+0020. `--allow` lines were already escaped byte by byte | `TestNotesAndRefusalsNeutraliseBidiAndC1` (a note, and a refusal through both `check` and `run --manifest`) |

Measured after the fix:

| Case | Before (round-2 finding) | After |
|---|---|---|
| `run --manifest --timeout 1s`, 16000 `env` header lines, 16000 `net` needs outside them (refusal capped at ten) | 7.9 s real, 5.2 s user | 0.34 s real, 0.06 s user |
| `run --manifest --timeout 1s`, 16000 `net` header lines covering 16000 needs (858 KB) | 14.5 s real, 5.2 s user | 0.07 s real, 0.09 s user |
| `arv check`, the same file | about 9.6 s real | 0.06 s real |
| `arv check`, 16000 `fs_read=dN` lines and 16000 dead-code `read_file("dN/f")` | not measured | 0.47 s real (0.26 s sys: one canonicalisation per path) |

## Known limits (recorded, not changed)

- **A relative path through a missing directory followed by `..` prints a grant that `--allow` refuses** (Minor, case 1). `read_file("missing/../x")` makes `arv check` print `--allow fs_read=missing/../x`. `runtime.Parse` cannot canonicalise that grant: `..` after a missing component fails, as it does in the kernel. So `arv run --allow fs_read=missing/../x` stops at startup (exit 2). The program runs with no grants if the call is dead code or caught by `match`. Without a header, `arv check` reads only the file, and it cannot tell this case apart without an lstat. The output fails closed: the run refuses to start, and it never gets more authority than it asked for. With a header, round 1 already reports the call as "cannot resolve". Reproduced on this branch: `arv check` exits 0 with that line, `arv run` prints `hi`, and `arv run --allow fs_read=missing/../x` exits 2 with `lstat .../missing: no such file or directory`.
- **The `--manifest` pre-check lstats the paths the program names, before `--timeout` starts** (Minor; replaces round 1's "a few hundredths of a second"). To compare a path need with the header, `Outside` canonicalises each distinct path once. That costs one lstat per component the walk visits: about one per 5 bytes of `d/../`, up to about 800 for a 4 KB path. The run itself never touches those paths if the calls are dead code. Measured on a 7.9 MB script with header `fs_read=/` and 2000 dead-code `read_file("d/../" x 780 + "fN")` calls: `arv run --timeout 1s` takes 0.02 to 0.31 s, while `arv run --manifest --timeout 1s` and `arv check` take 2.1 to 2.4 s (0.55 s user, 1.6 s sys). The cost is linear in the file's size plus the folding budget. A path built by `+` costs folded bytes, and folding stops at 1 MiB in all, so a 150 KB script that adds 3000 suffixes to one 3.9 KB `let` gets 268 needs and 2732 notes, and takes 0.45 s. Under the bounded-overhead rule this is a known limit, not a fix. A deadline shared with `--timeout` would remove it.

## Review round 3

- **The "too long once resolved" note depends on the directory `arv check` runs in** (Minor). `check.Program` reads `os.Getwd()`, so its report depends on process state. From a directory whose path is about 100 bytes long, a 4000-byte relative path resolves to 4101 bytes and becomes a note, with no `--allow` line. `arv run --allow fs_read=<path>` from `/tmp` resolves it to about 4005 bytes and admits it. This fails closed: without the printed grant the runtime denies the call, and the call is still listed. Fixed the wording, not the behaviour: the note now reads `never allowed from this directory: fs_read: scope is 4101 bytes long once resolved (max 4096)`. A path too long as written (absolute, or over 4096 bytes before resolving) keeps `never allowed:`, since no directory makes it fit. `Program`'s doc comment records the cwd dependence. `TestRelativePathTooLongOnceResolvedIsANote` checks both wordings. `TestManifestAcceptsAPathTheRuntimeNeverAdmits` checks the CLI output. Both failed on the old wording first. Known limit: a library caller cannot pass a directory other than the process's own. Report soundness holds relative to that directory.

## Mutations

| Mutation | Caught by |
|---|---|
| `grantIndex.allows`: skip ancestor lookups | `TestOutsideMatchesAdmit` |
| `grantIndex.allows`: skip the `/` grant | `TestOutsideMatchesAdmit` |
| `newGrantIndex`: do not record CIDR prefix lengths | `TestOutsideMatchesAdmit` |
| `grantIndex.admit`: never treat a canonical error as inside a grant | `TestOutsideMatchesAdmit` |
| `grantIndex.admit`: drop the canonical error's OS error | `TestOutsideMatchesAdmit` |
| `Outside` back to `CapSet.Admit` against every grant | `TestOutsideIsNotQuadratic`, `TestOutsideMemoryIsLinear` |
