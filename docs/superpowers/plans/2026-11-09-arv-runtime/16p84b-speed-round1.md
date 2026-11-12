# Phase 8.4: speed, review round 1

Continuation of [`16p84-speed.md`](16p84-speed.md). One Important finding and six Minor ones (two of them the same limit). No Critical ones.

| Finding | Severity | Disposition |
|---|---|---|
| No test records a log on one engine and replays or resumes it on the other (S2's claim that every logged check falls on the same step) | Important | Fixed: `TestLogsCrossEngines` (below). |
| Compiling cannot be cancelled and delays the first deadline check (two findings) | Minor | Known limit, with the reviewers' numbers, in `16p84-speed.md`. |
| Small integers are shared by the whole process, and `value.Integer` did not say it must not be changed | Minor | Documented on `value.Integer`, on `Effect.Fn` and in the README's host effects section. Not defended in code. |
| No test checks which engine is the default | Minor | Fixed: `TestDefaultEngine`. |
| The timing-tests known limit names one flaky resume test; a second fails too | Minor | Known limit corrected in `16p84-speed.md`. |
| Stale placeholder comment, and `TestNodesAreCompiled` looks only at the top node | Minor | Comment removed; `TestCompiledProgramsNeverWalk` added. |

## The Important finding: logs across engines

`arv/engine_log_test.go`, `TestLogsCrossEngines`. Logs from binaries built before Phase 8.4 are tree-walker logs, and the compiled default replays and resumes them. Three programs:

- **effects**: two spawned tasks, each with ten `getenv("HOME")` calls, and the Phase 5E forms (`&&`, `||`, `<=`, `>=`, `!=`, `.k`, `"${i}"`).
- **fail-fast**: a task loops with a `getenv` on every pass until its sibling's `error("boom")` cancels it. Its cancellation is a logged check.
- **timed**: the same loop is ended by a 30 ms timed scope, once in a spawned task and once in the root.

The loop does an effect on every pass, so a logged check that the other engine reaches at another point between `getenv` records makes the replay diverge. Without those effects, a cancelled infinite loop prints the same output wherever its check falls, and the engines could disagree without any test noticing.

Corrected in review round 2 ([`16p84c-speed-round2.md`](16p84c-speed-round2.md)): this round said that a check one step earlier or later would be caught. It would not. A pass of this loop is a `unit()`, tens of thousands of steps, before its `getenv`, so only a drift of about 1% of a pass moves the check across a record. Round 2 added a fourth program, **fail-fast dense**, whose passes are about 300 steps.

For each program and each direction (record on the tree-walker and read on compiled code, and the reverse), the test does the following:

1. It records the program, up to three times, until the recording prints what the test expects and, for the last two programs, logs a check.
2. It replays the log on the other engine.
3. It resumes, on the other engine, from three cuts of the log: after the first record, half way, and just after the first logged check. It then replays each resumed log on the recording engine.

Every run must print the same output and end with the same error. For **effects**, whose log does not depend on timing, the test also compares records. The resumed log must have the same records as the recording. A recording on each engine must also give the same records. Records are compared without `at`, `began`, `h` and the header's `started`, and sorted, because tasks append in scheduler order.

Checked against a mutant. Compiled integer literals ticked one extra step. The test failed with `fail-fast, recorded walk=true, read walk=false: replay: replay diverged at task root/0, check 350: the log has cancelled at check 326, which this run passed without seeing`. The **effects** program passed under that mutant: it logs no checks. 2.3 s, 4.6 s under `-race`.

## The Minor findings fixed

**`TestDefaultEngine`** (`arv/evaluator/engine_default_test.go`). `wantTreeWalk` is declared twice. `engine_want_test.go` declares it `false` (`!arv_treewalk`) and `engine_want_walk_test.go` declares it `true` (`arv_treewalk`). The test requires `defaultTreeWalk` and the current setting to equal it, and a run on the default engine to give a function with a compiled body exactly when the build wants compiled code. Flipping the constant in `engine_default.go` fails it in a plain build. Flipping it in `engine_treewalk.go` fails it under `-tags arv_treewalk`.

**`TestCompiledProgramsNeverWalk`** (`engine_nodes_test.go`). `Interpreter.walks` counts the nodes a task runs on the tree-walker: `enter` increments it when it has no code. The cost is one increment per node on the tree-walker only. Five programs together use every node type in `compiledNodes`, each nested inside the others. On compiled code none of them may walk a node, and on the tree-walker each must walk some, which shows the count works. Checked against a mutant: `compileIndex` evaluated its index with `in.Eval(node.Index, env)`, and the test failed with `walk=false: 4 nodes ran on the tree-walker`. `TestNodesAreCompiled` passed under it.

**Shared integers.** The doc comment on `value.Integer` now says an Integer is immutable and that `value.Int` shares -128 to 1023 across every run in the process. `Effect.Fn` (`hostbind.go`) and the README say that changing an argument, a small integer included, changes it for every run. Arv code cannot change an Integer, and nothing in `arv/` writes `.Value` after an Integer is made (checked with grep). A defence, such as copying a host effect's integer arguments, was not added: the documented contract already forbade changing arguments before this phase.
