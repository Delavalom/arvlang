# Phase 8.1, task p81.10: End to end through the real binary

> **For agentic workers:** this file is one task, p81.10. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: task p81.9 is committed in your tree. Start every shell command with `cd <your worktree> &&` and use absolute paths: the shell's working directory does not persist. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify` and never push; keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0` (toolchain 1.26.2): `t.Chdir`, `min` and `max` are available; do not use `testing/synctest` or `sync.WaitGroup.Go`. Before a long test run, check `df -h /System/Volumes/Data`; below 1 GB free, stop and report (only `go clean -cache` may be deleted). This machine is heavily loaded: keep wall-clock bounds generous.
>
> Every code block below was applied, in task order, to a scratch copy of `feat/phases-3-8` (860a158) and passed `gofmt -l .`, `go vet ./...` and `go test -race ./...` (including `e2e/`). Design: `15-phase8-tooling.md` §8.1. Deviations from it, and why: `p81-plan-1.md`. Phases 5E, 6, 7 and 8.2 are built at the same time and also touch the CLI, parser and evaluator: change only the files this task names.

### Task p81.10: End to end through the real binary

Black-box cases in `e2e/` (the harness builds the binary and serves `{{SERVER}}` locally):

- **Acceptance (amended, see `p81-plan-1.md`):** `arv check` on every file in `examples/` prints exactly the pinned output. For `fanout.arv` the set is exactly its header line's `--allow net=api.github.com`. For `uptime.arv` it is narrower than its line (`fs_write=out/report.json` under the line's `fs_write=out`), and the net host, read from `targets.txt`, is a note. A new example without an entry fails the test.
- `arv check` never runs the program: a script that fetches from the server, writes a file, reads `HOME`, sleeps ten minutes and recurses forever is checked at once, with no request reaching the server and no file written.
- `run --manifest`: a covering header runs (one request, the file written); a header without the server's address is refused before any request; `arv check` says the same with exit 1; `--allow` with `--manifest` exits 2. Each case is its own subtest, so the server's per-test counters are separate.

**Files:**
- Create (test): `e2e/p81_check_test.go`, `e2e/testdata/p81_noeffects.arv`, `e2e/testdata/p81_manifest.arv`

- [ ] **Step 1: Write the tests**

Create `e2e/testdata/p81_noeffects.arv`:

```arv
// arv check reads this program; it must never run it.
http_get("{{SERVER}}/ok?id={{ID}}");
write_file("made.txt", "x");
puts(getenv("HOME"));
sleep(600000);
let spin = fn() { spin() };
spin();
```

Create `e2e/testdata/p81_manifest.arv`:

```arv
// arv:allow net={{GRANT}}
// arv:allow fs_write=out.txt
let r = http_get("{{SERVER}}/ok?id={{ID}}");
write_file("out.txt", r["body"]);
puts(r["status"]);
```

Create `e2e/p81_check_test.go`:

```go
package e2e

import (
	"net/url"
	"path/filepath"
	"strings"
	"testing"
)

// Phase 8.1, arv check and run --manifest through the real binary.

// serverHost is the test server's host:port.
func serverHost(t *testing.T) string {
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	return u.Host
}

// Acceptance (15-phase8-tooling.md 8.1, as amended in p81-plan-1.md): for
// every file in examples/, arv check prints the set its header's arv run
// line uses, except where that line grants more than the program text
// names (uptime writes out/report.json under the line's fs_write=out) or
// names what only a run knows (uptime's net host comes from targets.txt,
// and a note says so).
func TestP81ExamplesCheck(t *testing.T) {
	want := map[string]string{
		"fanout.arv": "--allow net=api.github.com\n" +
			"# http_get at 14:23: host not statically known\n",
		"uptime.arv": "--allow fs_read=targets.txt\n--allow fs_write=out/report.json\n" +
			"# http_get at 15:16: host not statically known\n" +
			"# net at 23:34: scope not statically known\n" +
			"# net at 34:34: scope not statically known\n",
	}
	paths, err := filepath.Glob("../examples/*.arv")
	if err != nil || len(paths) == 0 {
		t.Fatalf("examples: %v %v", paths, err)
	}
	for _, p := range paths {
		name := filepath.Base(p)
		out, ok := want[name]
		if !ok {
			t.Errorf("examples/%s has no expected arv check output here: add it", name)
			continue
		}
		abs, err := filepath.Abs(p)
		if err != nil {
			t.Fatal(err)
		}
		check(t, spec{name: name, args: []string{"check", abs}, stdout: out})
	}
}

func TestP81CheckNeverRunsTheProgram(t *testing.T) {
	dir := t.TempDir()
	check(t, spec{
		name: "noeffects", script: "p81_noeffects.arv", args: []string{"check", "{{SCRIPT}}"}, dir: dir,
		stdout: "--allow net=" + serverHost(t) + "\n--allow fs_write=made.txt\n--allow env=HOME\n",
		verify: func(t *testing.T) {
			if n := hits(t, "/ok"); n != 0 {
				t.Errorf("the server saw %d requests", n)
			}
			assertAbsent(t, filepath.Join(dir, "made.txt"))
		},
	})
}

func TestP81RunManifest(t *testing.T) {
	host := serverHost(t)
	ok, refused := t.TempDir(), t.TempDir()
	noHits := func(dir string) func(t *testing.T) {
		return func(t *testing.T) {
			if n := hits(t, "/ok"); n != 0 {
				t.Errorf("the server saw %d requests", n)
			}
			assertAbsent(t, filepath.Join(dir, "out.txt"))
		}
	}
	runAll(t, []spec{{
		name: "covered", script: "p81_manifest.arv", args: []string{"run", "--manifest", "{{SCRIPT}}"},
		vars: map[string]string{"GRANT": host}, dir: ok, stdout: "200\n",
		verify: func(t *testing.T) {
			if n := hits(t, "/ok"); n != 1 {
				t.Errorf("the server saw %d requests, want 1", n)
			}
			assertFile(t, filepath.Join(ok, "out.txt"), okBody)
		},
	}, {
		name: "outside", script: "p81_manifest.arv", args: []string{"run", "--manifest", "{{SCRIPT}}"},
		vars: map[string]string{"GRANT": "api.github.com"}, dir: refused, code: 1,
		stderrHas: []string{"needs net=" + host + " (http_get at 3:9), outside its arv:allow manifest"},
		verify:    noHits(refused),
	}, {
		name: "check says so too", script: "p81_manifest.arv", args: []string{"check", "{{SCRIPT}}"},
		vars: map[string]string{"GRANT": "api.github.com"}, code: 1,
		stdout:    "--allow net=" + host + "\n--allow fs_write=out.txt\n",
		stderrHas: []string{"outside its arv:allow manifest"},
	}, {
		name: "with --allow", script: "p81_manifest.arv",
		args: []string{"run", "--allow", "env=HOME", "--manifest", "{{SCRIPT}}"},
		vars: map[string]string{"GRANT": host}, code: 2,
		stderrHas: []string{"cannot be combined with --allow"},
	}})
}

// The usage text names both commands.
func TestP81Usage(t *testing.T) {
	res := run(t, spec{name: "usage", args: []string{"help"}})
	if res.code != 2 || !strings.Contains(res.stderr, "arv check FILE") || !strings.Contains(res.stderr, "--manifest") {
		t.Fatalf("%s", res)
	}
}
```

- [ ] **Step 2: Run them**

Run: `go test -race ./e2e/ -run P81`
Expected: `ok`. The code they test is already in place, so they pass at once.

- [ ] **Step 2b: Run them against a race-built binary too**

Run: `ARV_E2E_RACE=1 go test -race ./e2e/ -run P81`
Expected: `ok`.

- [ ] **Step 3: Check the whole tree**

Run: `gofmt -l .` (prints nothing), `go vet ./...` and `go test -race ./...`.

- [ ] **Step 4: Commit**

```bash
git add e2e/p81_check_test.go e2e/testdata/p81_noeffects.arv e2e/testdata/p81_manifest.arv
git commit -m "test(e2e): arv check and run --manifest through the binary" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
