# Phase 5E, task pE.7: `examples/uptime-ergonomic.arv`

> **For agentic workers:** this file is one task, pE.7. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: tasks pE.1 to pE.6 are committed on `phase/pE` in `/Users/delavalom/delavalom-labs/arvlang-wt/pE`. Start every shell command with `cd /Users/delavalom/delavalom-labs/arvlang-wt/pE &&`. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green (first check `df -h /System/Volumes/Data`: below 1 GB free, stop and report "blocked: disk"; the only thing you may delete is the build cache, `go clean -cache`). End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify`, never push. Keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0`; the toolchain is 1.26.2. Create files that contain `${` with the Write or Edit tool, not with a shell heredoc.
>
> Every code block below was applied, in task order, to a scratch copy of the tree and passed `go vet ./...` and `go test -race ./...` (including `e2e/`). Overview, guarantees G1-G6, decisions and deviations: `pE-plan-1.md`.

### Task pE.7: the gate scenario in the new syntax

`examples/uptime-ergonomic.arv` is `examples/uptime.arv` rewritten with `.field`, `&&`, `>=` / `<=` and interpolation. Against the same in-process farm it must write the same report, print the same line and make the same number of requests; only its error message is new (`20 of 200 targets are down, more than 5%`). `examples/uptime.arv` and `bench/` are the measured gate inputs and are not touched. The README documents the syntax, and the fuzz targets get seeds that use it (`FuzzLexer` got its seed in pE.1).

The farm test reuses `uptime_example_test.go`'s harness. `runUptime` reads `examples/uptime.arv` and then changes the working directory (`t.Chdir`) until the test ends, so this task splits out `runUptimeSrc`, which takes the script's text: the new test reads both scripts first, then runs each against its own farm. Each farm listens on its own port, so the reports are compared with the host cut from each URL.

**Files:**
- Create: `examples/uptime-ergonomic.arv`
- Modify (test): `uptime_example_test.go`
- Create (test): `uptime_ergonomic_test.go`
- Modify: `README.md`
- Modify (test seeds): `arv/parser/fuzz_test.go`, `arv/fuzz_test.go`

- [ ] **Step 1: Write the failing test**

Create `uptime_ergonomic_test.go`:

```go
package main

import (
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv"
	"github.com/delavalom/arvlang/bench/farm"
)

// examples/uptime-ergonomic.arv is the gate scenario in the Phase 5E
// syntax. Against the same farm it writes the same report, prints the
// same line and makes the same requests as examples/uptime.arv; only its
// error message is new.
func TestUptimeErgonomicMatchesUptime(t *testing.T) {
	plain, err := os.ReadFile("examples/uptime.arv")
	if err != nil {
		t.Fatal(err)
	}
	ergonomic, err := os.ReadFile("examples/uptime-ergonomic.arv")
	if err != nil {
		t.Fatal(err)
	}
	atFive := fastGate
	atFive.Fail = 0.05
	for _, tt := range []struct {
		name string
		c    farm.Config
		msg  string // the ergonomic version's error; "" for none
	}{
		{"gate", fastGate, "20 of 200 targets are down, more than 5%"},
		{"at 5%", atFive, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			want := runUptimeSrc(t, plain, tt.c)
			got := runUptimeSrc(t, ergonomic, tt.c)
			if err := farm.Compare(got.got, got.want); err != nil {
				t.Fatalf("report against the farm: %v", err)
			}
			if a, b := withoutHost(got.got), withoutHost(want.got); !reflect.DeepEqual(a, b) {
				t.Fatalf("report %v, uptime.arv's %v", a, b)
			}
			if got.out != want.out {
				t.Errorf("printed %q, uptime.arv printed %q", got.out, want.out)
			}
			if got.stats.Total != want.stats.Total {
				t.Errorf("farm saw %d requests, %d for uptime.arv", got.stats.Total, want.stats.Total)
			}
			var ae *arv.Error
			switch {
			case tt.msg == "" && got.err != nil:
				t.Errorf("run error %v, want none", got.err)
			case tt.msg != "" && (!errors.As(got.err, &ae) || ae.Kind != "user" || ae.Msg != tt.msg):
				t.Errorf("run error %v, want the user error %q", got.err, tt.msg)
			}
		})
	}
}

// withoutHost is rs with each URL cut to its path and no latency: each
// run has its own farm, on its own port.
func withoutHost(rs []farm.Result) []farm.Result {
	out := make([]farm.Result, len(rs))
	for i, r := range rs {
		r.URL = r.URL[strings.Index(r.URL, "/t/"):]
		r.LatencyMS = 0
		out[i] = r
	}
	return out
}

// The new syntax makes the script shorter, not longer.
func TestUptimeErgonomicIsShort(t *testing.T) {
	count := func(path string) int {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, line := range strings.Split(string(src), "\n") {
			if l := strings.TrimSpace(line); l != "" && !strings.HasPrefix(l, "//") {
				n++
			}
		}
		return n
	}
	plain, ergonomic := count("examples/uptime.arv"), count("examples/uptime-ergonomic.arv")
	if ergonomic > plain {
		t.Errorf("%d code lines, uptime.arv has %d", ergonomic, plain)
	}
	t.Logf("%d code lines (uptime.arv: %d)", ergonomic, plain)
}
```

In `uptime_example_test.go`, replace:

```go
	src, err := os.ReadFile("examples/uptime.arv")
	if err != nil {
		t.Fatal(err)
	}
	f, err := farm.New(c)
```

with:

```go
	src, err := os.ReadFile("examples/uptime.arv")
	if err != nil {
		t.Fatal(err)
	}
	return runUptimeSrc(t, src, c)
}

// runUptimeSrc is runUptime for the script src. It changes the working
// directory until the test ends, so read every script before the first
// call.
func runUptimeSrc(t *testing.T, src []byte, c farm.Config) uptimeRun {
	t.Helper()
	f, err := farm.New(c)
```

- [ ] **Step 2: Run it and watch it fail**

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/pE && go test -run 'TestUptime' .`
Expected: FAIL: `open examples/uptime-ergonomic.arv: no such file or directory`, in both tests. The four `TestUptimeExample*` tests still pass.

- [ ] **Step 3: Write the example**

Create `examples/uptime-ergonomic.arv`:

```arv
// Uptime checker: the Phase 5 gate scenario of examples/uptime.arv,
// written with the Phase 5E syntax (.field, string interpolation, && and
// ||, <= and >=). It needs the same grants and writes the same report;
// only its error message is new: it says how many targets are down.
//
//   arv run --allow fs_read=targets.txt --allow fs_write=out \
//           --allow net=127.0.0.1:PORT examples/uptime-ergonomic.arv
//
// Each check holds net access to its own URL's host and nothing else.

// One attempt. A dropped connection is an effect error: the restart
// scope in check runs the attempt again.
let probe = fn(url) {
  let start = clock_ms();
  let status = http_get(url).status;
  {"url": url, "status": status, "up": status >= 200 && status <= 299, "latency_ms": clock_ms() - start}
};

// One target: up to 3 attempts within 2 s. Still failing, it is down.
let check = fn(url, host) {
  match scope (supervise: "restart", retries: 2, backoff_ms: 100, timeout_ms: 2000) {
    await spawn probe(url) with [net(host)]
  } {
    ok(r) => r,
    err(e) => {"url": url, "status": 0, "up": false, "latency_ms": 0}
  }
};

let urls = lines(read_file("targets.txt"));
let results = scope (tasks: 20) {
  await for url in urls {
    let host = split(url, "/")[2];
    spawn check(url, host) with [net(host)]
  }
};
write_file("out/report.json", json(results));

let count_down = fn(rs, i) {
  if (i >= len(rs)) { 0 } else { count_down(rs, i + 1) + if (rs[i].up) { 0 } else { 1 } }
};
let down = count_down(results, 0);
puts(json({"down": down, "total": len(results)}));
if (down * 20 > len(results)) { error("${down} of ${len(results)} targets are down, more than 5%") }
```

Line for line against `examples/uptime.arv`: `http_get(url).status` for `http_get(url)["status"]`; `status >= 200 && status <= 299` for `if (status > 199) { status < 300 } else { false }`; `rs[i].up` for `rs[i]["up"]`; `i >= len(rs)` for `i == len(rs)` (the same here, since `i` counts up from 0); and the error message names the numbers.

- [ ] **Step 4: Document the syntax**

In `README.md`, replace:

```markdown
`split(s, sep)`, `json(v)` (compact JSON, hash keys in order) and `clock_ms()` (a monotonic clock, for latencies).
```

with:

```markdown
`split(s, sep)`, `json(v)` (compact JSON, hash keys in order), `str(v)` (`v` as `puts` prints it) and `clock_ms()` (a monotonic clock, for latencies).

`r.up` is `r["up"]`. `"${n} of ${len(xs)} down"` interpolates: each `${...}` holds an expression (no string literals inside it) and prints as `puts` would; `\${` is a literal `${`. Integers compare with `<`, `>`, `<=` and `>=`. `a && b` and `a || b` short-circuit: the right side runs only when the left does not decide, and, as in `if`, only `true` counts as true.
```

In `README.md`, replace:

```markdown
probes both for a key-stealing change (`make escape`).
```

with:

```markdown
probes both for a key-stealing change (`make escape`).

`examples/uptime-ergonomic.arv` is the same scenario written with the syntax above. It takes the same grants (`--allow fs_read=targets.txt --allow fs_write=out --allow net=127.0.0.1:PORT`) and writes the same report.
```

- [ ] **Step 5: Seed the fuzz targets with the new syntax**

In `arv/parser/fuzz_test.go`, replace:

```go
	"a => b = > c",
}
```

with:

```go
	"a => b = > c",
	`"a ${r.up} \${x} ${ {1: 2}[1] } ${n >= 1 && m <= 2 || !k}"`,
	`"${" "${}" "${x;}" "${ (x }"`,
	"r. r.5 1.5 a && || b & c | d",
	"x.y.z && a.b || c <= d >= e",
}
```

In `arv/fuzz_test.go`, replace:

```go
	`getenv("HOME")`,
}
```

with:

```go
	`getenv("HOME")`,
	`let r = {"up": true, "n": 3}; if (r.up && r.n >= 3 || false) { "n=${r.n} ${[r]}" }`,
	`let d = fn(n, a) { if (n == 0) { a } else { d(n - 1, [a, a]) } }; "${d(30, 1)}"`,
	`false && http_get("http://127.0.0.1:1/") || "${1 / 0}"`,
}
```

The second seed renders a value of 2^30 integers: `FuzzRun`'s 4 MiB `alloc_mb` stops it at the limit. The third reaches no network: `false &&` skips the `http_get`.

- [ ] **Step 6: Run the tests and watch them pass**

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/pE && go test -race -run 'TestUptime' -v . 2>&1 | grep -v '=== RUN'; go test ./arv/ ./arv/parser/ ./arv/lexer/`
Expected: PASS, with `27 code lines (uptime.arv: 28)` logged. The seeds run as ordinary tests (no `-fuzz` flag).

- [ ] **Step 7: Commit**

Run the pre-commit checks (header), then:

```bash
cd /Users/delavalom/delavalom-labs/arvlang-wt/pE && git add examples/uptime-ergonomic.arv uptime_example_test.go uptime_ergonomic_test.go README.md arv/parser/fuzz_test.go arv/fuzz_test.go && git commit -m "feat(examples): the uptime scenario in the Phase 5E syntax

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
