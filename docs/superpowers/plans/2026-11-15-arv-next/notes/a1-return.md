# A1: `return` leaves the function from every value position

Roadmap item A1 ([`00-roadmap.md`](../00-roadmap.md) §1). Branch `fix/return-propagation`, from `3f49420`.

## The bug

`return` is a statement, so it reaches a value position only inside a block expression, such as `if`, `match`, `for` or `scope`. Both engines passed a return value on only where a statement list or an `if` condition ended. Everywhere else it was taken as a value. `let x = if (true) { return 1 } else { 0 }; 2` gave 2. `puts(if (true) { return 3 } else { 0 })` printed 3 and went on, so the return leaked into an effect. Other positions failed with errors such as `not a function: RETURN_VALUE` or `for: want ARRAY, got RETURN_VALUE`. The two engines agreed on these wrong answers, so the Phase 8.4 differential could not see them.

## What changed

A new helper, `abrupt` (`arv/evaluator/evaluator.go`), is true for an error or a return value. Every value position now checks `abrupt` where it checked `isError`, on both engines. The return then leaves the function from there, as an error does, and nothing to its right runs. The `if` condition fix from 16zg (`isError || isReturn`) is now `abrupt`, and `isReturn` is gone.

The positions, with the tree-walker site first and the compiled site second:

| Position | Tree-walker | Compiled |
|---|---|---|
| `let` value | `evalNode` LetStatement | `compileLet` |
| `return` value (a return of a return) | `evalNode` ReturnStatement | `compileReturn` |
| call callee | `evalNode` CallExpression | `compileCall` |
| call arguments, array elements | `evalExpressions` | `execAll` |
| hash keys and values | `evalHashLiteral` | `compileHash` |
| index left and index, `.field` | `evalNode` IndexExpression | `compileIndex` |
| prefix operand | `evalNode` PrefixExpression | `compilePrefix` |
| infix operands, chains, `&&`, `\|\|` | `evalInfixChain` | `compileInfixChain` |
| `if` condition | `evalIfExpression` | `compileIf` |
| `${...}` | a call to `$str` (`ast.StrBuiltin`), so the call-argument sites | same |
| `for` iterable | `forEach` (shared) | same |
| `match` subject | `matchArm` (shared, already passed a return on) | same |
| scope option values | `scopeOptions` (shared); its third result is now `value.Object` | same |
| `spawn` callee, arguments, `with` list | `evalSpawn` (walked by both) | same |
| `await` operand | `evalAwait` (walked by both) | same |

A `match` arm, a `scope` body and a `for` body already passed a return out as their value. The `let` or argument that held them now passes it on too.

**`spawn`.** The spawning task evaluates the callee, the arguments and the `with` list. A return in any of them leaves the spawning task's function before any task starts. No task is made, no grant is attenuated, and no effect runs (I2, I7).

**Top level.** A top-level `return` ends the program with its value, as before. `evalProgram` and `compileProgram` were not changed. So `let r = if (true) { return 7 } else { 0 }; puts(r)` at top level now ends at the `let` and prints nothing. Before, it printed 7.

`docs/language.md` has one sentence on `return`. No user-facing doc listed this bug as a limit. The roadmap and 16zg/18a mention it as history, and they are not changed.

## Tests

- `arv/evaluator/return_test.go`: 36 cases, one per position, plus the `if` condition, `&&`, `||`, nested positions, a return of a return, an inner function's own return, and two top-level cases. `TestReturnLeavesEveryValuePosition` checks the value and the output on both engines. `TestReturnCasesAgreeOnBothEngines` runs `sameOnBothEngines` (tight limits, exact steps and bytes). `TestReturnRoadmapProbes` runs the roadmap's three probes and wants `1\n3\n5\n`.
- `e2e/a1_return_test.go`: `TestA1ReturnLeavesValuePositions`, 5 programs through both binaries, covering every position and the top level.
- **Before the fix** (commit `0aec5c1`): 31 of the 33 first unit cases failed on both engines. The 2 that passed were the `match` subject and the top-level `return`. `TestReturnRoadmapProbes` failed, and all 5 e2e cases failed. The engines agreed on every wrong answer, so `TestReturnCasesAgreeOnBothEngines` passed.
- **Fuzz corpus.** The expression generator (`arv/ergo_gen_test.go`) has a new `ret` node, `(if (c) { return v } else { 0 })`. It feeds `FuzzEnginesAgree`, `TestEnginesAgreeProperty` and the desugaring property. 19 of the 200 fixed expression seeds now contain one. The statement generator (`arv/engine_stmt_test.go`) has a new expression form, `(if (c) { return v } else { block })`. `FuzzEnginesAgreeSource` seeds every return case and one nested program.

## Fuzzing (foreground, 120 s each, 8 workers, load average 47 to 207)

| Target | Seconds | Execs | Result |
|---|---|---|---|
| `FuzzEnginesAgreeSource` (`arv/evaluator`) | 120 | 909,604 | pass |
| `FuzzEnginesAgree` (`arv`) | 120 | 19,033 | pass |
| `FuzzEnginesAgreeStatements` (`arv`) | 120 | 2,358 | pass |
| `FuzzDesugarEquivalence` (`arv`) | 120 | 45,508 | pass |

## Mutation round

Each mutant was applied by hand, one at a time, and the file was restored with `git checkout` after each run. Command: `go test -count=1 -run TestReturn ./arv/evaluator/`. Unless a row says otherwise, a mutant turns one `abrupt` back into `isError`. `TestReturnLeavesEveryValuePosition` is "Leaves", and `TestReturnCasesAgreeOnBothEngines` is "Agree".

| # | Mutant | Caught by |
|---|---|---|
| M1 | tree `let` | Leaves, Agree, Probes |
| M2 | compiled `let` | Leaves, Agree, Probes |
| M3 | tree arguments (`evalExpressions`) | Leaves `call argument`, `array element` |
| M4 | compiled arguments (`execAll`) | Leaves, Agree |
| M5 | tree infix loop guard `!abrupt(left)` | Leaves `infix left` |
| M6 | compiled infix right operand | Leaves `infix right`, `comparison` |
| M7 | tree return of a return | Leaves `return of a return` |
| M8 | compiled return of a return | Leaves `return of a return` |
| M9 | scope option back to an `*value.Error` check | Leaves `scope option` |
| M10 | spawn `with` list | Leaves `spawn with list` |
| M11 | `await` operand | Leaves `await operand` |
| M12 | `for` iterable | Leaves `for iterable` |
| M13 | tree hash value | Leaves `hash value` |
| M14 | compiled hash key | Leaves `hash key` |
| M15 | `abrupt` without the `ReturnValue` case | Leaves, Probes, `TestReturnInConditionIsKept` |
| M16 | compiled prefix | Leaves `prefix minus`, `prefix bang` |
| M17 | compiled index | Leaves `index` |
| M18 | tree callee | Leaves `callee` |
| M19 | tree `if` condition | **survived** at first; caught after adding `if condition`, `&& left` and `\|\| left` (Leaves, Agree) |
| M20 | spawn arguments | Leaves `spawn argument` |
| M21 | compiled infix loop guard | Leaves `infix left` |
| M22 | tree array elements | Leaves `array element` |
| M23 | compiled `if` condition | Leaves, Agree, `TestReturnInConditionIsKept` |
| M24 | spawn callee | Leaves `spawn callee` |
| M25 | tree hash key | Leaves `hash key` |

**Totals: 25 tried, 25 caught, 1 of them after a test was added.** M19 survived because `TestReturnInConditionIsKept` runs only the build's engine, and the tree-walker is not the default build's engine. Commit `fe3b45b` added the three cases that catch it on both engines.

## Required runs

All runs are at `fe3b45b`, on darwin/arm64. Other sessions kept the load average between 130 and 420.

| Command | Result |
|---|---|
| `go vet ./...` | clean |
| `go test -race -timeout 40m ./arv/... ./cmd/...` | 10 packages pass. `arv` failed `TestResumeReplaysAFailedAttemptToItsEnd` (load 247). A rerun of `./arv/` failed `TestLogsCrossEngines` once, on its 30 ms `timed` program (load 416). A third run passed (620 s). |
| `go test -timeout 30m ./e2e/` | pass (71 s) |
| `go test -tags arv_treewalk -timeout 40m ./...` | pass (283 s) |
| `ARV_E2E_TAGS=arv_treewalk go test -timeout 30m ./e2e/` | pass (76 s) |

Neither `arv` failure uses `return`. Each times a calibrated `spin()` against a short deadline. Run alone under `-race`, `TestResumeReplaysAFailedAttemptToItsEnd` passed 3 of 3 on this branch and 3 of 3 on base `3f49420`. `TestLogsCrossEngines` passed 4 of 4 on each.

## Known limits

- `TestResumeReplaysAFailedAttemptToItsEnd` (package `arv`) failed once in a plain `go test ./arv/`, at load average about 30. It times `spin()` against a 1,500 ms scope. When run alternately with `-count=5` at load 40 to 75, the base commit `3f49420` failed and this branch passed. It is the spin-calibration flake family of A8, not this change.
- No builtin checks its arguments for a return value. None can get one now, because every value position stops a return first. A new value position must check `abrupt`.
