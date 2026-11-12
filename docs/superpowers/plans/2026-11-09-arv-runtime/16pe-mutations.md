# Phase 5E: mutation check, round 1

Continues [`16pe-ergonomics.md`](16pe-ergonomics.md) and [`16pe-hardening.md`](16pe-hardening.md). Guarantees G1-G6 are those of `pE-plan-1.md`. Run on `phase/pE` at f3063a3, 2026-11-12, darwin/arm64, Go 1.26.2, load average 9 to 90.

**Method.** Each mutation broke one thing in the runtime, then the whole suite ran (`go test -count=1 -timeout 180s ./...`: unit, property, fuzz seeds and `e2e/`), then `git checkout -- <file>` and a clean `git status` before the next. No mutation was committed and no test was added. M01 to M14 ran with `-timeout 300s` and without `-count=1`; see the cache note below (only M15's first run was affected, and it was re-run).

**Result: 41 mutations, 40 caught, 1 not caught (M28).**

## Mutation table

| # | Guarantee | Mutation | Caught | Catching tests (most specific first) |
|---|---|---|---|---|
| M01 | G1 | `\${` escape ignored: `"\${x}"` interpolates | yes | `TestInterpolationEscape`, `TestPEInterpolation/the_escape`, `/backslashes_stay`, desugaring property |
| M38 | G1 | a literal with only escaped `${` keeps the source text (`\${`) as its value | yes | `TestInterpolationEscape`, `TestPEInterpolation/the_escape`, `FuzzInterpolationParse` seeds |
| M02 | G1 | a lone `\|` lexes as `\|\|` | yes | `TestErgonomicTokens`, `TestAndOrSyntaxErrors`, `TestPEAndOr/a_lone_\|`, `FuzzLexerOperators` seed |
| M07 | G1 | `lexer.pair` does not consume its second byte | yes | `TestErgonomicTokens`, `FuzzLexer`, `TestPECompare`, `TestPEAndOr` and 20 more |
| M35 | G1 | a plain one-statement `if` prints as `(c if x)` | yes | `TestParseInterpolation` (only) |
| M03 | precedence | `&&` binds at `+` precedence | yes | `TestParseAndOrPrecedence`, `TestPEAndOr/precedence`, `TestUptimeErgonomicMatchesUptime` |
| M04 | precedence | `\|\|` binds as tightly as `&&` | yes | `TestParseAndOrPrecedence`, `TestAndOr`, `TestPEAndOr/precedence`, `/parentheses` |
| M05 | `<=` | `<=` evaluates `<` | yes | `TestLessOrEqualAndGreaterOrEqual`, `TestCompareMatchesGo`, `TestPECompare/integers` |
| M06 | `>=` | `a >= b` is `a >= b - 1` | yes | the same three, `TestPEUptimeErgonomic` |
| M08 | G2 | the side `&&` / `\|\|` skips is evaluated, its value dropped | yes | `TestAndOrShortCircuit`, `TestPEShortCircuit/steps_not_spent`, `/bytes_not_spent`, `TestDesugaringEquivalenceExamples` |
| M09 | G2 | a short circuit charges 3 phantom steps | yes | `TestAndOrCostWhatIfCosts`, `TestPEStepsMatchTheHandWrittenForm/short_circuit`, desugaring examples and seeds |
| M10 | G2 | `&&` built as `\|\|` and the reverse | yes | `TestPEAndOr/truth_table`, `TestParseAndOrPrecedence`, `FuzzLogicalPrecedence` seeds |
| M11 | G3 | keywords refused as field names | yes | `TestParseFieldIsIndex`, `TestFieldAccess`, `TestPEField/keyword_names` |
| M12 | G3 | a missing field is an error, not `nil` | yes | `TestFieldAccess`, `TestFieldErrorsMatchIndexErrors`, `TestPEField/missing_key_is_nil` |
| M13 | G3 | `.field` costs one step more than `["field"]` | yes | `TestFieldCostsWhatIndexCosts`, `TestPEStepsMatchTheHandWrittenForm`, desugaring property |
| M14 | G3 | `.` binds at `*` precedence | yes | `TestParseFieldIsIndex`, desugaring property |
| M15 | G3, G6 | a field chain does not count toward `MaxNesting` | yes | `TestNestingLimit/field_chain`, `TestPENesting/field_chain` |
| M16 | G3 | the key literal placed at the dot, not the name | yes | `TestFieldNodeShape` (only; no error is ever reported at a string key, so it is not visible at run time) |
| M17 | G4 | `str` of a string goes through `Inspect` and is charged | yes | `TestStrChargesItsText`, `TestInterpolatingAStringCostsTheStrCall` |
| M18 | G4 | `str` drops its final charge (a short render is free) | yes | `TestStrChargesItsText`, `TestInterpolationCostsWhatConcatenationCosts` |
| M19 | G4 | `str` charges the chunks twice | yes | `TestStrChargesItsText` (only) |
| M20 | G4 | `nil` renders as `""` | yes | `TestStrRendersAsPuts`, `TestInterpolationRendersAsPuts`, `TestPEInterpolationRendersAsPuts/nil` |
| M21 | G4 | empty text pieces kept (extra `+ ""`) | yes | `TestParseInterpolation`, `TestInterpolationCostsWhatConcatenationCosts`, `TestPEStepsMatch...`, `TestPEAllocMatches...` |
| M22 | G4 | the concatenation built right-associated | yes | `TestParseInterpolation`, `TestInterpolationCostsWhatConcatenationCosts`, `TestPENesting/a_long_interpolation`, desugaring property |
| M36 | G4 | the right side's block gets an extra statement (+2 steps) | yes | `TestAndOrCostWhatIfCosts`, `TestPEStepsMatchTheHandWrittenForm/&&`, `/\|\|` |
| M37 | G4 | the constant branch gets an extra statement | yes | `TestAndOrCostWhatIfCosts`, `TestPEStepsMatchTheHandWrittenForm/short_circuit` |
| M23 | G5 | `$str(e)` placed at the opening quote, not its `${` | yes | `TestInterpolationPositions`, `TestInterpolationErrorPositions`, `FuzzInterpolationParse` seeds |
| M41 | G5 | the `${...}` sub-lexer starts one column right | yes | `TestInterpolationPositions`, `TestInterpolationSyntaxErrors`, `TestPEInterpolation` |
| M24 | G5 | `lexer.NewAt` columns off by one (every lexer) | yes | 64 top-level tests, `TestInterpolationPositions` and `TestPEInterpolation` among them |
| M25 | G5 | the column after a newline inside a literal off by one | yes | `TestInterpolationPositions`, `TestInterpolationErrorPositions`, `TestPEInterpolation/parse_error_inside` |
| M26 | G5 | the `&&` / `\|\|` node placed at the operator, not where its left side starts | yes | `TestAndOrNodeShape`, `FuzzLogicalPrecedence` seeds |
| M27 | G5 | the first text piece placed one column after the quote | yes | `TestInterpolationConcatErrorAtLeftmostPiece`, `TestPEInterpolationOutOfBytes` |
| M28 | G5 | the right side's block and statement placed at the operator, not where the right side starts | **no** | none (see below) |
| M29 | G6, I6 | `str` ignores a failed chunk charge | yes | `TestStrStopsAtTheLimitAndWhenCancelled` (only) |
| M30 | G6, I6 | `$str` bound to a `str` that charges nothing | yes | `TestStrHasAnUnbindableName`, `TestInterpolationCostsWhatConcatenationCosts`, `TestPEAllocMatchesTheHandWrittenForm/an_array`, desugaring property |
| M31 | G6, I5 | `str` ignores cancellation | yes, by a kill | `TestStrStopsAtTheLimitAndWhenCancelled`: renders `doubling(40)` (about 1 TiB) with nothing to stop it; the package was killed at the 180 s timeout |
| M32 | G6 | an interpolated literal does not count toward `MaxNesting` | yes | `TestNestingLimit/interpolation` (only) |
| M33 | G6 | `&&` / `\|\|` chains do not count toward `MaxNesting` | yes | `TestNestingLimit/and_chain`, `/or_chain`, `TestPENesting/&&_chain`, `/\|\|_chain` |
| M34 | G6, I1 | `Task.Require` always allows | yes | `TestAndOrRightSideIsCapabilityChecked`, 31 base tests (the e2e Phase 5E cases pass: the net denial still happens at dial) |
| M39 | G6, I1 | fs admission ignores the grants | yes | `TestPEInterpolation/denial_inside`, 30 base tests |
| M40 | G6, I1 | any grant covers any request | yes | 57 base tests; no Phase 5E test (they run with no grants, where nothing covers) |

Invariants: this phase touches I1 (M34, M39, M40), I5 (M31), I6 (M09, M13, M17-M19, M29, M30, M36, M37) and, through the leak test, I4 (M08, M10). It adds no code that spawns, attenuates, canonicalises or makes an effect, and `str` is pure, so I2, I3, I7 and I8 are not touched.

## Not caught: M28 (G5)

`parseLogicalExpression` places the right side's block and its statement at `start`, the right side's first token. Placing them at the operator passes every test. It is visible: a steps or cancellation error that lands on the tick of that block or statement is reported at their position. Repro (`arv run --steps 200 p.arv`):

```
1;1;1;1;1;1;1;
let f = fn() { true &&
   g() };
let g = fn() { 7 };
let h = fn(n) { if (n == 0) { 0 } else { f(); h(n - 1) } };
h(100)
```

Unmutated: `budget exceeded: steps (limit 200) (at p.arv:3:4)`; mutated: `(at p.arv:2:21)`. With 0 to 140 leading `1;`, the two binaries differ at 7, 20, 34, 47, 61, ... A test for the round-2 fixer: the block and statement positions in `TestAndOrNodeShape` (both sides, `&&` and `||`), or this repro.

**Round-2 fix (tests only).** `TestAndOrNodeShape` now checks the block, statement and expression positions of both sides (right side where it starts, the constant at the operator) for `&&` and `||`; `TestAndOrStepsErrorAtTheRightSide` (`arv/evaluator/logical_pos_test.go`) runs this repro for `&&` and `||` with 0 to 60 leading `1;` and fails if any steps error is at the operator, and if none lands at 3:4. With M28 re-applied both fail (the sweep at 7, 20, 34 and 47 leading, for each operator); restored, both pass. M28 is now caught.

## Found while checking (for the fixer and the next checkers)

- **`go test ./...` can report `e2e` from a stale cache.** The harness builds `arv` with a `go build` subprocess, so the `e2e` package's cache key does not include `arv/` sources: after M14 passed, M15's run printed `ok e2e (cached)` although `TestPENesting/field_chain` fails under M15. Mutation runs, and any check of a runtime change through `e2e`, need `-count=1`.
- **`TestErgonomicFormsLeakNoGoroutine/short-circuited spawn` hangs instead of failing** when the short circuit breaks (M08, M10): its right side sleeps 600 s and the case sets no `Timeout`, so the package stopped at the 300 s test timeout. A `Limits{Timeout: ...}` on that case would make it a quick failure.
- **The cancelled half of `TestStrStopsAtTheLimitAndWhenCancelled` would build a 1 TiB string** if the check broke (M31): memory grew until the binary was killed. A value of a few hundred MiB keeps the 30 s bound meaningful without that risk on a shared machine.
- Single catchers, worth a second test: M19 and M29 (`TestStrChargesItsText`, `TestStrStopsAtTheLimitAndWhenCancelled`), M32 (`TestNestingLimit/interpolation`; the e2e `in ${...}` case nests parentheses, which still count), M35 (`TestParseInterpolation`).
