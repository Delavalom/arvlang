# Phase 8.4: speed, review round 2

Continuation of [`16p84-speed.md`](16p84-speed.md) and [`16p84b-speed-round1.md`](16p84b-speed-round1.md). One Minor finding. No Critical or Important ones.

| Finding | Severity | Disposition |
|---|---|---|
| `TestLogsCrossEngines` and the round-1 notes say the test catches a check that moves by one step. It does not. | Minor | Fixed: added a program with short passes, and corrected the comment and the round-1 notes. |

## The finding

Each pass of `loop` (the program that **fail-fast** and **timed** cancel) runs `unit()` first. That is 10^4 innermost iterations, tens of thousands of steps. Then it runs `getenv("HOME")`. The logged cancellation lands deep inside `unit()`. A drift of a few steps per pass never moves it across a `getenv` record, so the replay agrees anyway. The reviewer's mutant shows this. It adds an extra `Task.Tick` on every compiled `CallExpression`, and `TestLogsCrossEngines` passed under it. Under the same mutant in this round, `fail-fast` failed in 3 of 6 runs, and only in a resume. The failure was `replay diverged at task root/0, check 679: the log has cancelled at check 678`. As the reviewer says, exact-step fitting in `TestEnginesAgreeOnCorpus` covers the guarantee itself. This finding is about precision.

## The fix

`arv/engine_log_test.go` gains a fourth program, **fail-fast dense**. A task runs `dense` until its sibling's `error("boom")`, after 20 ms, cancels it:

```
let dense = fn() { for r in a { for s in a { for t in a { for u in a { for v in [1, 2, 3] {
  for x in a { 1 }; getenv("HOME") } } } } }; "never" };
```

A pass is a 10-iteration loop and a `getenv`. Here (load about 46) a recording logged about 3 300 to 3 600 `getenv` records before the cancellation. The task's cancelled check was ordinal 4 157 after 3 589 passes, so a pass is about 300 steps, a little more than one check (`checkEvery` is 256). A drift that adds up to about one pass by the logged check moves that check across a `getenv` record. That is about 0.1 step per pass. One extra step per pass moves it by thousands of steps.

`dense` stops after 30 000 passes. An uncancelled run logs 5.2 MB. A cancelled recording here is about 0.55-0.62 MB. So load cannot grow the log without bound. If `dense` finishes before `boom`, no check is logged, and `recordOnEngine` tries again, as it does for the other programs.

The test file's header comment now gives the precision of both loops instead of the one-step claim. The round-1 paragraph is corrected in place and points here.

## Checked against mutants

Mutants were tested in a scratch copy (`git archive HEAD`). An `ONLY` environment filter ran one program at a time.

- **Extra `Task.Tick` on every compiled `CallExpression`** (the reviewer's mutant; in `dense` that is one per pass). **fail-fast dense** failed in 8 of 8 runs, for example `fail-fast dense, recorded walk=true, read walk=false: replay: replay diverged at task root/0, check 2402: the log has cancelled at check 2394`. The drift was 8 to 13 checks.
- **Extra `Task.Tick` on every `ForExpression`.** It failed in 3 of 3 runs, with a drift of 13 to 17 checks.

On the real code the whole test takes 2.3 s, and also 2.3 s under `-race`. It passes under `-tags arv_treewalk`. `go test -race -count=1 ./...` is green (load 46).
