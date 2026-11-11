# Handoff: Phases 3–8 run, paused after Wave 1

**Status (2026-11-10, 21:20):** the run was paused on purpose to save weekly subscription limits. Wave 1 is merged into `feat/phases-3-8`. Phases 4 through 8 have not started. Nothing is pushed.

## Where things stand

| Item | State |
|---|---|
| Base | PR #1 (Phases 0–2) merged into `main`. `feat/phases-3-8` was cut from `origin/main` (9a732df). |
| Phase 3 (budgets) | Merged (95b2261), then hardening merged (50a51e3). 13 plan tasks, 80 e2e cases, 3 fuzz targets (2 min each), budget property tests, a goroutine-leak check, 81 mutations, all caught after two rounds of added tests. |
| Phase 8.3 (network hardening, `os.Root`, go 1.24.0) | Merged (d0edc29). 10 plan tasks, 55 e2e cases, 4 fuzz targets (2 min each), a random I2 property test (3000 grant sets), 32 mutations, all caught after added tests or confirmed equivalent. |
| Suite at pause | `gofmt`, `go vet`, `go test -race -count=1 ./...` and `ARV_E2E_RACE=1 go test -race ./e2e/` all green. 1096 tests and subtests pass. 119 commits ahead of `origin/main`. |
| Worktrees | All removed. Branches `phase/p3` and `phase/p83` are kept. |
| Plans | `p3-plan-*.md` and `p83-plan-*.md` (expanded, compiled). Notes: `16-implementation-notes.md` plus continuation files `16b`–`16i`. |

## Open items, in order

1. **Re-review the last Phase 3 fix.** Phase 3's re-entry review used all five rounds (blocking findings per round: 4, 5, 6, 3, 1). In round 5 the spec reviewer found nothing. The adversarial reviewer found one Important issue: charging the canonicalisation walk made `alloc_mb` an oracle on the filesystem outside the grants. The fixer fixed it in 5e70f7b and the following commits, but no reviewer has re-checked it. The hardening code from rounds 7–11 (`git diff 620538b..phase/p3`) has also not had a mutation pass. Start the next session with one fresh adversarial review of the round-11 change (`arv/runtime/canonical.go` and its callers), then a mutation pass over that diff. The script's `reentry` mode does both.
2. **Decide the `alloc_mb` severity policy before Phase 4 (the kickoff asks the owner at the Phase 4 gate).** Rounds 5–11 kept finding new allocations that `alloc_mb` did not charge: reads, concatenation, `push`, capability values, error text, `spawn … with`, `for … in`, `await` arrays, Canonical, net coverage. Each was real and is now fixed. But `alloc_mb` is an accounting of Arv values, not a hard memory limit, and review will not converge if every uncharged Go-side allocation is Important. Suggested rule: an uncharged allocation is Important only if it is unbounded by the program's own charged values (it grows with input the script controls and no charge precedes it). A bounded constant factor is Minor and goes into the known limits. **This is the owner's decision.**
3. **Known bug, older than Phase 3 (also asked at the Phase 4 gate):** integer literals with a leading zero parse as octal (`puts(010)` prints 8; `scope (timeout_ms: 0100)` sets 64 ms). `arv/parser/integer_base_test.go` is skipped with `BUG:`.
4. **Phase 8.3 limits** (in `16b`): `0177.0.0.1` isn't parsed and its lookup went off-host, so it is left out of the e2e suite. `::127.0.0.1` (IPv4-compatible) is classed as public; adding `::/96` to the reserved classes is a cheap follow-up.

## Lessons from this run (apply them next time)

- **Cost.** Wave 1 alone used about 10.6M subagent tokens across 4 workflow runs, and hit the 5-hour limit once. Plan for at least that much per wave. Waves 4–5 are bigger.
- **Don't resume a workflow whose phases run in parallel.** Resuming with `resumeFromRunId` replays the cache only for the longest unchanged prefix of agent calls. When phases interleave, the order changes and live agents re-run work on a phase that already merged. Instead, start a fresh run with `args.waves` listing only unfinished phases, and use `args.reentry` for a phase that only needs review, mutation and merge.
- **Disk.** The Go build cache grew to 5.5 GB and free space fell to 1.8 GB. Run `go clean -cache` between waves.
- **Equivalent mutants.** The mutation loop now accepts survivors only from round 4 on, and only when the checker confirms each one is an equivalent mutant. One survivor first labelled equivalent was a real gap (the FIFO swap), so don't accept it in round 1.
- **Notes size.** Agents split notes into `16b`…`16i` to stay under the 18 KB rule. Consider consolidating per phase (`16-phase3.md`, `16-phase83.md`) before the final report.

## The orchestration script

`~/delavalom-labs/arvlang-wt/workflow/arv-phases.js` (38 KB, outside the repo because of the 20 KB commit hook). It defines every phase (p3, p83, p4, p5, p6, p7, p81, p82, p67, p84, p85), the per-phase pipeline (plan → TDD implement → spec and adversarial review with a fix loop → e2e, fuzz and properties → mutation loop → serialized merge), the gate stop and the final three-reviewer pass. Args:

```json
{ "waves": [["p4"], ["p5"]],
  "reentry": { "p3": { "planFiles": ["…"], "focus": "Mutate ONLY …" } },
  "final": false }
```

Remaining waves: `[["p4"], ["p5"]]`, then the gate decision, then `[["p6","p7","p81","p82"], ["p67", "p84 only if the gate says CPU matters"], ["p85"]]` with `"final": true`.

## Kickoff prompt for the next session

Paste this into a new Claude Code session opened in `~/delavalom-labs/arvlang`, after checking disk (`df -h /System/Volumes/Data`, more than 10 GB free) and raising "Dynamic workflow size" in `/config`:

> Continue the Arv runtime Phases 3–8 run. Read `docs/superpowers/plans/2026-11-09-arv-runtime/18-handoff-after-wave-1.md` first, then `17-phases-3-8-kickoff.md` (its rules and verification requirements still apply), `00-index.md`, `01-design.md` and the `16*` notes. Work on `feat/phases-3-8`.
> 1. Run one fresh adversarial review of the last Phase 3 fix (open item 1) and fix anything Critical or Important.
> 2. **Phase 4 decision gate: stop and ask me both questions below (AskUserQuestion) before any Phase 4 agent starts. Don't pick an answer for me, and don't launch the workflow until I've answered both.**
>    - **`alloc_mb` severity rule (open item 2).** Review didn't converge because every uncounted allocation was rated Important, and `alloc_mb` counts Arv values rather than enforcing a hard memory cap. Suggested rule: Important only when the uncounted memory grows with input the script controls; a bounded constant overhead is Minor and gets recorded as a known limit. Options: adopt the suggestion / keep every uncounted allocation Important / my own rule. Write the answer into the review and fixer prompts in the script before launching.
>    - **Leading-zero integers (open item 3).** Numbers with a leading zero are read as octal (`010` is 8; `scope (timeout_ms: 0100)` sets 64 ms). This predates Phase 3, and the skipped test `arv/parser/integer_base_test.go` reproduces it. Options: fix it now as decimal, test-first, before Phase 4 / make a leading zero a parse error / leave it as a known limit.
> 3. Use a workflow with the script at `~/delavalom-labs/arvlang-wt/workflow/arv-phases.js` and args `{"waves": [["p4"], ["p5"]]}`. The Phase 5 gate is a hard stop: if any of criteria 1–5 fails, stop and report.
> 4. If the gate passes, run the remaining waves and the final pass as in the handoff. Don't push until the final report; then push `feat/phases-3-8` and open one PR into `main` with the gate report linked.
