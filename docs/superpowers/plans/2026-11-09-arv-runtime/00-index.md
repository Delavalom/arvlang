# Arv Runtime Implementation Plan (index)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn arvlang into a small language whose runtime enforces what a Go library can't: capability-based authority, budgets, and structured concurrency with supervision. Then add durable replay and LLM-agent effects on the same model.

**Architecture:** Keep the tree-walking interpreter already in the repo (`lang/monkeylexer`) and add a `runtime` package for per-task state. Every operation that touches the OS is a builtin that checks the calling task's capabilities. Every task runs on its own goroutine with its own interpreter, inside a scope that waits for its children and cancels them on failure.

**Tech Stack:** Go 1.22+ standard library only (`context`, `sync`, `net/http`, `path/filepath`). No third-party dependencies through Phase 5. Phase 7 calls the Anthropic Messages API over plain `net/http`.

---

## Files in this plan

Each file stays under 5,000 tokens because of the global pre-commit hook. Read `01-design.md` first; every task file refers to the invariants (I1–I8) defined there.

| File | Phase | What you get at the end | Detail level |
|---|---|---|---|
| `01-design.md` | — | Language surface, invariants, architecture, prior art | Design |
| `02-phase0-consolidate.md` | 0 | One interpreter in `arv/`, old experiments tagged, comments in the lexer | Bite-sized, validated |
| `03-phase1a-capabilities.md` | 1 | `runtime.Capability`, `CapSet`, `Task` | Bite-sized, validated |
| `04-phase1b-interpreter.md` | 1 | `Interpreter` bound to a task; depth limit; builtins receive the task | Bite-sized, validated |
| `05-phase1c-effects.md` | 1 | Capability values + `http_get`, `read_file`, `write_file`, `getenv` | Bite-sized, validated |
| `06-phase1d-run-and-cli.md` | 1 | `arv.Run` embedding API and `arv run --allow kind=scope FILE` | Bite-sized, validated |
| `07-phase2a-syntax.md` | 2 | `scope`, `spawn … with`, `await` parse | Bite-sized, validated |
| `08-phase2b-scopes.md` | 2 | Tasks run in parallel, scopes join and fail fast, `sleep` | Bite-sized, validated |
| `09-phase2c-fanout-demo.md` | 2 | Fan-out example and end-to-end test; README rewrite | Bite-sized, validated |
| `10-phase3-budgets.md` | 3 | Timeout / steps / tasks / allocation limits, `for … in`, error positions | Design + task outline |
| `11-phase4-supervision.md` | 4 | `fail_fast` / `collect` / `restart` strategies, `match` on errors | Design + task outline |
| `12-phase5-gate.md` | 5 | Uptime demo vs. a Go baseline, then a go/no-go decision | Design + task outline |
| `13-phase6-durable-replay.md` | 6 | Effect log; `--record`, `--replay`, `--resume` | Design + task outline |
| `14-phase7-agents.md` | 7 | `llm(...)` / `tool(...)` capabilities, `ask`, `agent`, token and dollar budgets | Design + task outline |
| `15-phase8-tooling.md` | 8 | `arv check`, custom effects for Go hosts, network hardening, speed | Design + task outline |

**"Validated"** means every code block in that file was compiled and tested before this plan was written. The Phase 0+1 tree and the Phase 0+1+2 tree each pass `go vet` and `go test -race -count=3` on Go 1.26 / darwin-arm64. **"Design + task outline"** phases get their own bite-sized plan once the phase before them is merged, because what Phase 5 teaches will change them. Writing their code now would be guessing.

## Execution order and branches

1. Phases run in order. Inside a phase, tasks run in order.
2. Work on `feat/runtime`, created in Task 0.1. One commit per task, as the steps say.
3. After Phase 2 (file 09), open a PR. Phases 3 and 4 can share a PR. Phase 5 must be its own PR, because its result decides whether Phases 6–8 happen.
4. Run `go test -race ./...` before every commit. Time-based tests use generous margins; if one flakes, widen the margin and say so in the commit. Don't delete the test.

## Definitions used everywhere

- **Task:** one unit of Arv execution: a goroutine plus its own `Interpreter` plus a `runtime.Task`.
- **Effect:** any builtin whose result depends on the outside world (network, files, environment, time, randomness, LLMs).
- **Capability:** a grant like `net("api.github.com")`. Tasks hold a `CapSet`. Arv values of type `CAPABILITY` only *name* grants; they give no authority until a parent with that authority hands them over at `spawn`.
- **Scope:** owns tasks. It doesn't finish before they do. The program body and every task body are implicit scopes.
