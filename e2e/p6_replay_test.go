package e2e

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Phase 6 through the binary: --record, --replay and --resume (I7).

// logRun runs the binary in dir with args and checks no panic reached
// stderr: a bad log is a clean error, never a crash.
func logRun(t *testing.T, dir string, args ...string) result {
	t.Helper()
	res := run(t, spec{args: args, dir: dir})
	if strings.Contains(res.stderr, "panic") || strings.Contains(res.stderr, "goroutine ") {
		t.Fatalf("the binary panicked\n%s", res)
	}
	return res
}

// p6Script writes src to dir/name with {{SERVER}} and {{ID}} filled in.
func p6Script(t *testing.T, dir, name, src string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	mustWrite(t, path, expand(t, src, map[string]string{"ID": testID(t)}))
	return path
}

const p6Fanout = `let caps = [net("127.0.0.1")];
let get = fn(n) { puts(n); http_get("{{SERVER}}/ok?id={{ID}}&n=" + n)["body"] };
let ts = [spawn get("1") with caps, spawn get("2") with caps, spawn get("3") with caps];
sleep(6000);
puts(random(1000000000));
puts(now());
for t in ts { puts(await t) }`

// Record, then replay with no --allow: the same stdout, no request, the
// sleep answered from the log, the grants named from the log's header.
func TestP6ReplayIsOffline(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	script := p6Script(t, dir, "fanout.arv", p6Fanout)
	log := filepath.Join(dir, "run.jsonl")
	rec := logRun(t, dir, "run", "--record", log, "--allow", "net=127.0.0.1", script)
	drainServer(t)
	if rec.code != 0 || rec.stderr != "" || hits(t, "/ok") != 3 {
		t.Fatalf("record: %d requests\n%s", hits(t, "/ok"), rec)
	}
	rep := logRun(t, dir, "run", "--replay", log, script)
	drainServer(t)
	if rep.code != 0 || rep.stdout != rec.stdout || rep.stderr != "replay: grants: net(\"127.0.0.1\")\n" {
		t.Fatalf("replay: want stdout %q\n%s", rec.stdout, rep)
	}
	if hits(t, "/ok") != 3 {
		t.Fatalf("the replay made %d live requests", hits(t, "/ok")-3)
	}
	if rep.duration >= 6*time.Second { // the record took at least 6s
		t.Fatalf("the replay slept: %s", rep.duration)
	}
}

// An edited script diverges: the error names the task, the effect and
// both requests, and nothing runs live.
func TestP6EditedScriptDiverges(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := `puts(http_get("{{SERVER}}/ok?id={{ID}}&v=1")["status"])`
	script := p6Script(t, dir, "get.arv", src)
	log := filepath.Join(dir, "run.jsonl")
	if rec := logRun(t, dir, "run", "--record", log, "--allow", "net=127.0.0.1", script); rec.code != 0 {
		t.Fatalf("record\n%s", rec)
	}
	p6Script(t, dir, "get.arv", strings.Replace(src, "v=1", "v=2", 1))
	rep := logRun(t, dir, "run", "--replay", log, script)
	for _, want := range []string{"has changed since the log was recorded", "replay diverged at task root, effect 1",
		"v=1", "v=2"} {
		if !strings.Contains(rep.stderr, want) {
			t.Errorf("stderr lacks %q", want)
		}
	}
	if rep.code != 1 || rep.stdout != "" || hits(t, "/ok") != 1 {
		t.Fatalf("%d requests\n%s", hits(t, "/ok"), rep)
	}
}

// A restart scope retries a flaky request; the replay prints the same
// tries without a request.
func TestP6ReplayOfRestarts(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	script := p6Script(t, dir, "flaky.arv", `let f = fn() {
  let r = http_get("{{SERVER}}/flaky?id={{ID}}&k=a&fail=2");
  puts(r["status"]);
  if (r["status"] == 200) { r["body"] } else { error("down") }
};
puts(scope (supervise: "restart", retries: 3, backoff_ms: 1, retry_on: ["user"]) { await spawn f() with [net("127.0.0.1")] })`)
	log := filepath.Join(dir, "run.jsonl")
	rec := logRun(t, dir, "run", "--record", log, "--allow", "net=127.0.0.1", script)
	if rec.code != 0 || rec.stdout != "500\n500\n200\nup\n" {
		t.Fatalf("record\n%s", rec)
	}
	rep := logRun(t, dir, "run", "--replay", log, script)
	if rep.code != 0 || rep.stdout != rec.stdout || len(flakyTimes(t, "a")) != 3 {
		t.Fatalf("replay: %d requests\n%s", len(flakyTimes(t, "a")), rep)
	}
}

// SIGKILL while a request is in flight, then --resume: the recorded
// requests are not made again and the output is the uninterrupted run's.
func TestP6ResumeAfterSIGKILL(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	script := p6Script(t, dir, "steps.arv", `puts(http_get("{{SERVER}}/ok?id={{ID}}&n=1")["status"]);
puts(http_get("{{SERVER}}/ok?id={{ID}}&n=2")["status"]);
puts(http_get("{{SERVER}}/wait?id={{ID}}&k=go&n=1")["body"]);
puts("done")`)
	log := filepath.Join(dir, "run.jsonl")
	cmd := exec.Command(arvBin, "run", "--record", log, "--fsync", "--allow", "net=127.0.0.1", script)
	cmd.Env, cmd.Dir = []string{}, dir
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(gateTimeout)
	for hits(t, "/wait") == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	cmd.Process.Kill()
	cmd.Wait()
	if hits(t, "/wait") != 1 || out.String() != "200\n200\n" {
		t.Fatalf("before the kill: %d waits, output %q", hits(t, "/wait"), out.String())
	}
	arrive(testID(t), "go") // the gate is open for the resumed run
	res := logRun(t, dir, "run", "--resume", log, "--allow", "net=127.0.0.1", script)
	if res.code != 0 || res.stdout != "200\n200\nopen\ndone\n" || res.stderr != "" {
		t.Fatalf("resume\n%s", res)
	}
	if hits(t, "/ok") != 2 || hits(t, "/wait") != 2 {
		t.Fatalf("resume repeated work: %d /ok, %d /wait", hits(t, "/ok"), hits(t, "/wait"))
	}
	rep := logRun(t, dir, "run", "--replay", log, script)
	if rep.code != 0 || rep.stdout != res.stdout || hits(t, "/wait") != 2 {
		t.Fatalf("replay of the resumed log\n%s", rep)
	}
}

// A tampered, truncated or foreign log is a clean error and runs nothing.
func TestP6BadLogs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	script := p6Script(t, dir, "get.arv", `puts(http_get("{{SERVER}}/ok?id={{ID}}")["body"])`)
	log := filepath.Join(dir, "run.jsonl")
	if rec := logRun(t, dir, "run", "--record", log, "--allow", "net=127.0.0.1", script); rec.code != 0 {
		t.Fatalf("record\n%s", rec)
	}
	good, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.SplitAfter(string(good), "\n")
	for _, tt := range []struct{ name, log, want string }{
		{"a changed result", strings.Replace(string(good), okBody, "hello from KO", 1), "hash chain broken"},
		{"a dropped line", lines[0] + strings.Join(lines[2:], ""), "hash chain broken"},
		{"cut before the end", strings.Join(lines[:len(lines)-2], ""), "did not finish"},
		{"cut mid-line", string(good[:len(good)-10]), "did not finish"},
		{"not a log", "hello\n", "not a log line"},
		{"empty", "", "empty"},
	} {
		bad := filepath.Join(dir, "bad.jsonl")
		mustWrite(t, bad, tt.log)
		res := logRun(t, dir, "run", "--replay", bad, script)
		if res.code != 1 || res.stdout != "" || !strings.HasPrefix(res.stderr, "error: ") || !strings.Contains(res.stderr, tt.want) {
			t.Errorf("%s: want a clean error with %q\n%s", tt.name, tt.want, res)
		}
	}
	if hits(t, "/ok") != 1 {
		t.Fatalf("a bad log ran %d live requests", hits(t, "/ok")-1)
	}
}
