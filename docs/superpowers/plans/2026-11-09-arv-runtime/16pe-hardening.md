# Phase 5E: e2e and hardening

Continues [`16pe-ergonomics.md`](16pe-ergonomics.md). The guarantees G1-G6 are those of `pE-plan-1.md`. No production code changed in this pass, and nothing the tests found needed fixing.

## E2E (the real binary)

`e2e/pE_test.go` and `e2e/pE_cost_test.go`. Every case runs `arv run` on a small program and checks stdout, stderr and the exit code, with `(at FILE:LINE:COL)` checked exactly where a position matters.

| Test | Covers |
|---|---|
| `TestPEField` | G3: value, `nil` for a missing key, chains, keyword names, mixing with calls and indexes; the error on an integer, `nil` or an array is the index error at the start of `r`; `r.5` and `1.5` are parse errors at the number |
| `TestPECompare` | `<=` / `>=` on integers, precedence against `+` and `==`, the string and mixed-type errors, `< =` stays two tokens |
| `TestPEAndOr` | the truth table, only `true` is true, `\|\|` is no default, precedence and parentheses, errors on either side at that side's line:col (G5), a lone `&` or `\|` |
| `TestPEShortCircuit` | G2 and G6: a decided left side means no request reaches the server, no denial without a net grant, no spawned task, no steps (`--steps 1000` with `f(5000)` on the right) and no bytes (`--alloc-mb 1`); a right side that runs is denied, makes its request, and spends steps and bytes |
| `TestPEInterpolationRendersAsPuts` | G4: `puts(v)`, `puts("${v}")` and `puts("<${v}>")` print the same text for integers, booleans, strings (also empty), arrays, hashes, `nil`, a function, a builtin and an awaited task's value |
| `TestPEInterpolation` | the syntax, `\${`, `$`, `{` and backslashes keep their meaning (G1), lines inside `${...}`, a `//` comment with a brace, `str` against `$str`, 2000 pieces; errors inside `${...}` at their own line:col across lines, a denied `read_file` inside `${...}` at its call (G5, G6); the parse errors (unterminated, empty, two expressions, a string inside, a bad expression) at their positions |
| `TestPEStepsMatchTheHandWrittenForm` | G4: the smallest `--steps` that runs each ergonomic form equals its hand-written form's (field chain, `&&`, `\|\|`, a short circuit over a call, an interpolation of an integer and an array) |
| `TestPEAllocMatchesTheHandWrittenForm` | G4, G6: the smallest `--alloc-mb` for interpolating 768 KiB strings and an array of them equals that of `str` and `+` (7 MiB each) |
| `TestPEInterpolationOutOfBytes` | an interpolation that runs out of `alloc_mb` stops the run at its string's opening quote; nothing after it runs |
| `TestPENesting` | more than 10000 `&&`, `\|\|`, `.a` or parentheses inside `${...}` is the nesting parse error; a 50000-piece interpolation (a `+` chain) runs |
| `TestPEUptimeErgonomic` | `examples/uptime-ergonomic.arv` and `examples/uptime.arv` with the header's grants, against the e2e server: up, 404, 500 and a refused connection (restarted, then down); the same printed line and the same `out/report.json` (latency aside), and each script's own error over 5% |

## Property and leak tests (`arv/`)

- **Desugaring equivalence** (`ergo_gen_test.go`, `ergo_prop_test.go`): a random expression over `.field`, `&&`, `||`, interpolation (text pieces with `\${`, `$a`, braces, backslashes and newlines), `if`, `!`, `-`, the binary operators, a printing `tick` and `await spawn`, is written twice: ergonomic, and as the code it means (`h["k"]`, the two `if`s, `"t" + str(e) + "u"` with empty pieces left out). Both programs must give the same value or error (message and kind), the same output, and the same exact steps and bytes (the smallest limits under which they stop with no budget error). `TestDesugaringEquivalenceProperty` runs 300 fixed seeds (about 30% of them end in a runtime error, which is compared too); `TestDesugaringEquivalenceExamples` pins the short-circuit and escape shapes; `FuzzDesugarEquivalence` is the same check on fuzzer bytes. For a program that fails, the steps measure is degenerate (the root does not charge its last partial chunk after an error), so steps are compared exactly only on runs that succeed; bytes are compared on all.
- **`<=` / `>=` against Go** (`TestCompareMatchesGo`): random integers (small and full-range) give Go's answers; on strings, booleans, arrays, hashes and functions the error is `<`'s with the operator written.
- **Leak rule** (`TestErgonomicFormsLeakNoGoroutine`): no goroutine outlives `Run` for a short-circuited spawn, a spawn on the right of `&&`, an unawaited task on the right of `||` followed by an error, a timeout while the right side awaits, awaited tasks inside `${...}`, a timeout inside `${...}`, `alloc_mb` running out inside `${...}` while a task sleeps, and steps running out on the right of `||` while a task sleeps.

## Fuzz targets

| Target | Package | Oracle |
|---|---|---|
| `FuzzLexerOperators` | `arv/lexer` | bytes mapped onto `<>=!&\|. \nab1`; every token's type, literal and line:col equal a reference maximal-munch lexer's |
| `FuzzLexerNewAt` | `arv/lexer` | `NewAt(input, line, col)` gives `New(input)`'s tokens, shifted by `line-1` lines and, on the first line, `col-1` columns |
| `FuzzLogicalPrecedence` | `arv/parser` | a random tree over `\|\| && == != < > <= >= + - * /`, `!`, `-` and primary atoms (fields, calls, indexes, interpolations) written with the fewest parentheses (plus random extra ones and line breaks) parses to exactly that tree, and every sub-expression is at its own line:col; `&&` / `\|\|` nodes carry their operator token |
| `FuzzInterpolationParse` | `arv/parser` | any text in a string literal: deterministic errors placed at or after the opening quote; without `${` the literal is unchanged (G1); otherwise the pieces tile the text exactly (text spelled with `\${` read as `${`, each `$str` call at its `${`, holding what the source up to the balancing `}` parses to alone, placed there) |
| `FuzzDesugarEquivalence` | `arv` | the desugaring property above |

Results (2 minutes each, `go test -run='^$' -fuzz='^NAME$' -fuzztime=2m`, darwin/arm64, Go 1.26.2, 8 workers; the machine's 1-minute load average rose from about 8 to 155 during the runs, 2026-11-12):

| Target | Seconds | Execs | Failures |
|---|---|---|---|
| Target | Package | Seconds | Execs | New interesting | Found |
|---|---|---|---|---|---|
| `FuzzLexerOperators` | `arv/lexer` | 120 | 27,795,052 | 0 (corpus 122) | nothing |
| `FuzzLexerNewAt` | `arv/lexer` | 120 | 16,019,443 | 69 (corpus 248) | nothing |
| `FuzzLogicalPrecedence` | `arv/parser` | 121 | 1,444,203 | 62 (corpus 183) | nothing |
| `FuzzInterpolationParse` | `arv/parser` | 121 | 1,929,491 | 157 (corpus 293) | nothing |
| `FuzzDesugarEquivalence` | `arv` | 121 | 194,965 | 388 (corpus 408) | nothing |

The orchestrator ran these in the foreground after two hardening agents had stopped before the background runs finished.

## Found while hardening

Nothing in Phase 5E's scope. Two observations for the record:
- A shared scratch directory can hold another phase's `arv` binary: an ad-hoc probe must build its own (the e2e harness always does).
- After an Arv runtime error the root scope does not charge its last partial chunk of steps, so "the smallest `--steps` that runs" is 1 for any program that fails before 256 steps. This is the base's accounting, not a Phase 5E change; it only limits what a steps comparison can see.
