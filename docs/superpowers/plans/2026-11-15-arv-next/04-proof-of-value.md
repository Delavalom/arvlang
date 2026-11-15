# Proof of value: what a new language must show before people adopt it

Part of [`00-roadmap.md`](00-roadmap.md).

Adoption research finds that libraries, existing code and existing expertise drive adoption more than language features do ([Meyerovich & Rabkin, OOPSLA'13](https://lmeyerov.github.io/projects/socioplt/papers/oopsla2013.pdf)). Arv cannot win on libraries. It has to win on one guarantee nobody else gives, shown so clearly that a skeptic believes it in five minutes, and then make the first script cheap to write.

**Positioning in one line:** *the runtime decides what an agent script can touch, how much it can spend, and lets you replay exactly what it did.*

## 1. Flagship demos (Phases 10 and 14)

Each demo is a directory in `examples/` with a README, a recorded log that replays offline with no key, and an e2e test.

| # | Demo | What it proves | Builds on |
|---|---|---|---|
| D1 | **Uptime fan-out**: 200 URLs, 20 at a time, restarts, a report | 28 lines against Go's 115, the same wall time, and the escape probe denied (`gate-report.md`) | Exists: `examples/uptime.arv`, `examples/uptime-ergonomic.arv` |
| D2 | **Code mode**: a model writes Arv to answer a data question. The host runs it under a read-only grant, a $0.25 budget and recording | A model's code can't exceed the host's grant or budget, and every step can be replayed | Phase 14 |
| D3 | **Kill -9 agent**: a research agent killed mid-run, then resumed | No tool call or model charge is repeated (only the call in flight), and the replay is byte-identical | Exists in tests (Phase 6x7 e2e, `16ze`); needs a demo |
| D4 | **Runbook with approval**: Kubernetes host effects (`examples/host`) with `approve()` before `k8s_apply` | Host-defined capabilities, a human in the loop that survives a restart, and an audit log | `examples/host`, Phase 13d, Phase 14 |
| D5 | **Triage bot**: read 50 GitHub issues, classify each with `ask`, write labels to a file | Per-tool grants, a token budget, and a replayable CI fixture (`arv test`) | Phases 10, 11 |
| D6 | **Data pull with budgets**: fetch paginated JSON, transform it, write a CSV | `parse_json`, `http` with headers and a `secret` token, and `alloc_mb` | Phase 10 |

D1 to D6, plus four smaller scripts from design partners, are the "ten real scripts" that Phase 10 must make writable with no workaround.

## 2. The safety story, demonstrated

**`examples/attacks/`: what a malicious script cannot do.** Each attack is a script, the command that runs it, and the refusal the run prints, all checked by an e2e test.

| Attack | Arv's answer | Where enforced |
|---|---|---|
| Read `~/.ssh/id_rsa` with no grant, or with a grant for another directory | `capability denied` | I1, I3; `arv/runtime/canonical.go` |
| Escape a grant with `..`, a symlink, or a directory swapped for a symlink mid-run | Refused; `os.Root` opens beneath the grant | Phase 8.3; `arv/evaluator/fsopen.go` |
| Reach `169.254.169.254`, or a granted name that rebinds to `127.0.0.1` | Refused at dial time | Phase 8.3 |
| Read the API key through `getenv`, `/proc/self/environ`, or a reply that echoes it | Removed from the environment; procfs refused; the reply refused | Phase 7; `arv/llm/keycheck.go` |
| A child task widening its authority at `spawn` | `spawn … with` refused unless the parent holds it | I2 |
| Fork bomb, infinite loop, memory churn, spending bomb | `tasks`, `steps`/`timeout_ms`, `alloc_mb`, `tokens`/`usd_cents` | I6, Phase 3, Phase 7 |
| A prompt-injected agent calling a tool it was not given | The model gets a `capability denied` tool result, with no grant list | Phase 7 redaction |
| An agent's tool used to send data to an outside host | The tool's `http_get` needs its own `net` grant | I1, I2 |
| A malicious imported module | It holds nothing it was not passed | Phase 12 |

**What Arv cannot stop today.** Say this in the same doc, from 18a, so readers trust the rest:
- An fs grant that covers a file holding the key reads it.
- A hard link inside a grant to a file outside it is followed.
- A forged log can raise a resume's limits, until 13a.
- `alloc_mb` is not a live-memory limit, until 13c.
- Arv runs in the host's process. It is not a kernel sandbox: a bug in a Go host effect is outside its protection. For untrusted code at scale, also run it in a container, gVisor or Firecracker ([Firecracker, NSDI'20](https://www.usenix.org/conference/nsdi20/presentation/agache)).

**Mapping to OWASP.** "Excessive Agency" names three root causes: too much functionality, too many permissions, too much autonomy. Its mitigations include minimal tools and permissions, authorization enforced outside the model, and rate limits ([OWASP LLM06:2025](https://genai.owasp.org/llmrisk/llm062025-excessive-agency/)). Arv makes each of these a property of the runtime. Write a one-page mapping, and map the [OWASP Top 10 for Agentic Applications](https://genai.owasp.org/resource/2025-12-09/owasp-top-10-for-agentic-applications-for-2026) the same way, marking honestly what Arv does not address (goal hijack in itself, for example).

## 3. Comparison matrix

The cells are from each project's own docs, linked. **●** = enforced by the runtime, **◐** = partial or opt-in, **○** = not provided. Re-check every cell before publishing, and invite each project to correct its column.

| Guarantee | Arv | Deno | Node `--permission` | Python + LangGraph | Temporal / Restate | Starlark | Code Mode (CF) |
|---|---|---|---|---|---|---|---|
| No ambient authority | ● | ● | ◐ "seat belt", not for malicious code | ○ | ○ | ● hermetic | ● |
| Per-resource grants (host, path, env var) | ● | ● | ◐ fs paths only; net all-or-nothing | ○ | ○ | n/a | ● via bindings |
| Per-task attenuation inside one program | ● (I2) | ○ | ○ | ○ | ○ | ○ | ◐ per isolate |
| Dial-time SSRF and rebinding check | ● | ○ unconfirmed | ○ | ○ | ○ | n/a | ◐ `globalOutbound` |
| Symlink-safe path checks | ● | ◐ | ○ symlinks followed | ○ | ○ | n/a | n/a |
| Step, time and allocation budgets | ● | ○ | ○ | ○ | ◐ timeouts | ● steps | ◐ Workers CPU limits (unconfirmed for Code Mode) |
| Token and dollar budgets | ● | ○ | ○ | ○ | ○ | ○ | ○ |
| Structured concurrency and supervision | ● | ○ | ○ | ◐ graph | ◐ workflows | ○ | ○ |
| Replay of every nondeterministic effect | ● (I7) | ○ | ○ | ◐ checkpoints at super-steps | ● history or journal (SDK rules) | ● deterministic | ○ |
| Offline replay with no server | ● | ○ | ○ | ◐ | ○ needs server | n/a | ○ |
| Resume after SIGKILL | ● | ○ | ○ | ◐ | ● | ○ | ○ |
| Grants known before running | ● `arv check` | ○ | ○ | ○ | ○ | n/a | ○ |
| Embeddable in Go | ● | ○ | ○ | ○ | ◐ SDK | ● | ○ |

**Sources**
- Deno: [security docs](https://docs.deno.com/runtime/fundamentals/security/). `--allow-run` and FFI count as allow-all, and a deny-path bypass was found ([GHSA-8xpq-cjcf-3wh9](https://cve.circl.lu/vuln/ghsa-8xpq-cjcf-3wh9)).
- Node: [permissions](https://nodejs.org/api/permissions.html) (stable; symlinks followed; open file descriptors bypass it).
- LangGraph: [checkpointers](https://docs.langchain.com/oss/python/langgraph/checkpointers).
- Temporal: [determinism and history](https://docs.temporal.io/workflow-definition). Restate: [journal](https://docs.restate.dev/concepts/durable_execution).
- Starlark: [spec](https://github.com/bazelbuild/starlark/blob/master/spec.md).
- Cloudflare: [Code Mode](https://developers.cloudflare.com/agents/api-reference/codemode).
- Arv's cells: the README sections "Grants", "Budgets", "Agents" and "Record, replay and resume".

**Where the others win**
- Ecosystems: Python, npm.
- Durable engines run as clusters (Temporal, Restate).
- Kernel-level isolation: E2B, Modal, gVisor.
- Long-running workflows with versioning (Temporal: [versioning](https://docs.temporal.io/develop/go/versioning)).

Arv composes with the last of these: run Arv inside a microVM.

## 4. Case studies (Phase 10 onwards)

- Recruit **3 design partners**: an ops team (fan-out checks, deploy steps), an AI product team (agents with tools), and a security-minded platform team (code mode).
- For each, write one before-and-after report with:
  - lines of code;
  - grant lines against the authority actually used;
  - incidents or near-misses avoided (a denied call found in `arv log`);
  - cost per run under budgets;
  - time to debug a failure through replay, against their old tool.
- Publish them with consent, and keep the partners' scripts as e2e fixtures when they agree.

## 5. Docs and tutorials (Phase 11)

- **15-minute tutorial:** install, fan-out with grants, add a budget, record and replay, then an agent with one tool.
- **"Arv for Python agent developers":** one page mapping LangGraph or Agents SDK concepts to Arv.
- **Cookbook:** 20 recipes (paginate an API, retry with backoff, a fan-out report, an agent with an approval step, CI fixtures with `arv test`).
- **Reference:** the language, builtins, grants and budgets, generated where possible from `arv/check/builtins.go` so it cannot drift.
- **Security model:** V0's threat model, the attack suite above, and the limits.
- **Embedding guide:** `arv.Options`, host effects, `Covers`, recording host effects.
- **Playground:** run the examples in a browser with fake effects, and step through a recorded log.

## 6. Launch plan

1. **Private alpha** (2027 Q1, Phase 10 done): the 3 design partners, a weekly call, and their issues fixed first.
2. **Public v0.x launch** (2027 Q2), once all of these are ready:
   - Phase 11 tooling;
   - demos D1 to D6;
   - the attack suite;
   - the B6 benchmark, published with its code;
   - the matrix above, checked by each project;
   - V7's audit at least scheduled.
3. **The launch post:** "The runtime decides." Lead with D2 (code mode) and D3 (kill -9), then the attack suite, then honest numbers, losses included.
   - Deno 1.0 led with secure-by-default, a stability promise and a single binary, and published a benchmark it lost ([Deno 1.0](https://deno.com/blog/v1)).
   - Bun 1.0 named its compatibility gaps up front ([Bun 1.0](https://bun.sh/blog/bun-v1.0)).
4. **Channels:** Hacker News, Lobsters, r/golang, Gophers Slack, the MCP community, and a talk proposal for GopherCon and the AI Engineer conferences. A short video of D3 (kill -9, then a byte-identical replay).
5. **After launch:** issues triaged within 48 hours, a monthly release, and a public roadmap (this file).

## 7. Adoption metrics and the adoption gate (decision D7)

**Measured, with no telemetry in the binary**
- GitHub stars and forks.
- Unique cloners.
- Release downloads.
- Homebrew installs, once Phase 15 is done.
- pkg.go.dev "Imported by" for embedders.
- Issues and PRs from people other than the owner.
- Docs-site funnel: tutorial start to finish.
- Time to the first working script in 5 moderated user tests. The target is under 10 minutes.

**The gate at launch + 90 days.** Set before launch and not tuned afterwards, like the Phase 5 gate.

| # | Criterion | Threshold |
|---|---|---|
| 1 | Design partners still running Arv weekly | ≥ 2 of 3 |
| 2 | External embedders (Go imports outside the owner's repos) | ≥ 3 |
| 3 | Issues or PRs from non-owners | ≥ 25 |
| 4 | Median time to the first script in user tests | ≤ 10 min |
| 5 | Unresolved security reports older than 30 days | 0 |
| 6 | Owner: "Would I still start the next agent or ops script in Arv?" | yes |

**What follows from the gate**
- All six hold: go to v1.0 (Phase 15).
- Criteria 1 and 6 hold but the rest fail: narrow the scope to embedding (Arv as Go's agent sandbox) and run the gate again in 90 days.
- Criteria 1 and 6 fail: stop the language effort, and keep the runtime as a Go library.
