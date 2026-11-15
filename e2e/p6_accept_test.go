package e2e

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Phase 6 acceptance tests through the binary, beyond p6_replay_test.go:
// the crash after 3 of 5 effects, restart attempts in the log, file and
// environment effects answered from the log, the flags a replay or a
// resume refuses, and a log cut mid-line.

// logLine is the part of an effect log line these tests look at.
type logLine struct {
	Task string          `json:"task"`
	Seq  int             `json:"seq"`
	Kind string          `json:"kind"`
	Req  json.RawMessage `json:"req"`
	End  json.RawMessage `json:"end"`
}

// readLogLines parses every whole line of the log at path.
func readLogLines(t *testing.T, path string) []logLine {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out []logLine
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(nil, 1<<24)
	for sc.Scan() {
		var l logLine
		if err := json.Unmarshal(sc.Bytes(), &l); err != nil {
			continue // a partial last line
		}
		out = append(out, l)
	}
	return out
}

// kinds counts the log's records of kind k.
func kinds(lines []logLine, k string) int {
	n := 0
	for _, l := range lines {
		if l.Kind == k {
			n++
		}
	}
	return n
}

// startArv starts the binary in dir; the caller kills or waits for it.
func startArv(t *testing.T, dir string, env []string, args ...string) (*exec.Cmd, *bytes.Buffer) {
	t.Helper()
	cmd := exec.Command(arvBin, args...)
	cmd.Env, cmd.Dir = append([]string{}, env...), dir
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	return cmd, &out
}

// waitHits waits until the server has seen n requests for path, or fails.
func waitHits(t *testing.T, path string, n int) {
	t.Helper()
	deadline := time.Now().Add(gateTimeout)
	for hits(t, path) < n {
		if time.Now().After(deadline) {
			t.Fatalf("the server saw %d requests for %s, want %d", hits(t, path), path, n)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

const p6FiveSteps = `let get = fn(p) { http_get("{{SERVER}}" + p + "id={{ID}}")["body"] };
puts(get("/ok?n=1&"));
puts(get("/ok?n=2&"));
puts(get("/ok?n=3&"));
puts(get("/wait?k={{K}}&n=1&"));
puts(get("/ok?n=5&"));
puts("done")`

// The design's acceptance test: a run killed after 3 of 5 sequential
// effects resumes with exactly 2 new requests (the one in flight, made
// again, and the last), and prints what an uninterrupted run prints.
func TestP6ResumeAfterThreeOfFive(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	whole := filepath.Join(dir, "whole.arv")
	mustWrite(t, whole, expand(t, p6FiveSteps, map[string]string{"K": "open"}))
	arrive(testID(t), "open")
	ref := logRun(t, dir, "run", "--allow", "net=127.0.0.1", whole)
	if ref.code != 0 || ref.stderr != "" || hits(t, "/ok") != 4 || hits(t, "/wait") != 1 {
		t.Fatalf("uninterrupted run\n%s", ref)
	}

	script := filepath.Join(dir, "steps.arv")
	mustWrite(t, script, expand(t, p6FiveSteps, map[string]string{"K": "shut"}))
	log := filepath.Join(dir, "run.jsonl")
	cmd, out := startArv(t, dir, nil, "run", "--record", log, "--allow", "net=127.0.0.1", script)
	waitHits(t, "/wait", 2) // the fourth request is in flight
	cmd.Process.Kill()
	cmd.Wait()
	drainServer(t)
	lines := readLogLines(t, log)
	if got := kinds(lines, "http_get"); got != 3 || lines[len(lines)-1].End != nil || out.String() != strings.Repeat(okBody+"\n", 3) {
		t.Fatalf("before the kill: %d requests logged, output %q", got, out.String())
	}
	okBefore := hits(t, "/ok")

	arrive(testID(t), "shut")
	res := logRun(t, dir, "run", "--resume", log, "--allow", "net=127.0.0.1", script)
	if res.code != 0 || res.stderr != "" || res.stdout != ref.stdout {
		t.Fatalf("resume: want stdout %q\n%s", ref.stdout, res)
	}
	if n := hits(t, "/ok") - okBefore + hits(t, "/wait") - 2; n != 2 {
		t.Fatalf("the resume made %d new requests, want 2", n)
	}
	if got := kinds(readLogLines(t, log), "http_get"); got != 5 {
		t.Fatalf("the resumed log holds %d requests, want 5", got)
	}
	rep := logRun(t, dir, "run", "--replay", log, script)
	if rep.code != 0 || rep.stdout != ref.stdout || hits(t, "/ok") != okBefore+1 || hits(t, "/wait") != 3 {
		t.Fatalf("replay of the resumed log\n%s", rep)
	}
}

// Restart attempts are logged as root/0, root/0#2, root/0#3, in order,
// and a replay answers them in that order.
func TestP6RestartAttemptsInTheLog(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	script := p6Script(t, dir, "flaky.arv", `let f = fn() {
  let r = http_get("{{SERVER}}/flaky?id={{ID}}&k=b&fail=2");
  if (r["status"] == 200) { r["body"] } else { error("down") }
};
puts(scope (supervise: "restart", retries: 5, backoff_ms: 1, retry_on: ["user"]) { await spawn f() with [net("127.0.0.1")] })`)
	log := filepath.Join(dir, "run.jsonl")
	rec := logRun(t, dir, "run", "--record", log, "--allow", "net=127.0.0.1", script)
	if rec.code != 0 || rec.stdout != "up\n" {
		t.Fatalf("record\n%s", rec)
	}
	var tasks []string
	for _, l := range readLogLines(t, log) {
		if l.Kind == "http_get" {
			tasks = append(tasks, l.Task)
		}
	}
	if strings.Join(tasks, " ") != "root/0 root/0#2 root/0#3" {
		t.Fatalf("attempts logged as %q", tasks)
	}
	rep := logRun(t, dir, "run", "--replay", log, script)
	if rep.code != 0 || rep.stdout != rec.stdout || len(flakyTimes(t, "b")) != 3 {
		t.Fatalf("replay: %d requests\n%s", len(flakyTimes(t, "b")), rep)
	}
}

// Files and the environment are effects too: a replay with no --allow
// installs the header's grants, reads no file, writes no file and sees
// the recorded environment, not its own.
func TestP6ReplayAnswersFilesAndEnv(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	data := filepath.Join(dir, "data")
	in, out := filepath.Join(data, "in.txt"), filepath.Join(data, "out.txt")
	mustWrite(t, in, "recorded input")
	script := filepath.Join(dir, "fs.arv")
	mustWrite(t, script, `let r = read_file("data/in.txt");
write_file("data/out.txt", r + " " + getenv("ARV_P6_E"));
let h = spawn fn() { read_file("data/out.txt") }() with [fs_read("data")];
puts(await h);
puts(clock_ms() > -1)`)
	log := filepath.Join(dir, "run.jsonl")
	args := []string{"--allow", "fs_read=data", "--allow", "fs_write=data", "--allow", "env=ARV_P6_E"}
	rec := run(t, spec{args: append(append([]string{"run", "--record", log}, args...), script), dir: dir, env: []string{"ARV_P6_E=first"}})
	if rec.code != 0 || rec.stdout != "recorded input first\ntrue\n" {
		t.Fatalf("record\n%s", rec)
	}
	if err := os.RemoveAll(data); err != nil {
		t.Fatal(err)
	}
	rep := run(t, spec{args: []string{"run", "--replay", log, script}, dir: dir, env: []string{"ARV_P6_E=second"}})
	if rep.code != 0 || rep.stdout != rec.stdout {
		t.Fatalf("replay: want %q\n%s", rec.stdout, rep)
	}
	for _, want := range []string{"replay: grants: ", "fs_read(", "fs_write(", `env("ARV_P6_E")`} {
		if !strings.Contains(rep.stderr, want) {
			t.Errorf("replay stderr lacks %q", want)
		}
	}
	assertAbsent(t, out)
	assertAbsent(t, data)
}

// The flags a replay or a resume refuses: each is a clean error before
// anything runs. A replay takes its grants and limits from the log; a
// resume needs the recorded grants and takes the recorded limits.
func TestP6LogFlagsRefused(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	script := p6Script(t, dir, "get.arv", `puts(http_get("{{SERVER}}/ok?id={{ID}}")["status"])`)
	log := filepath.Join(dir, "run.jsonl")
	if rec := logRun(t, dir, "run", "--record", log, "--allow", "net=127.0.0.1", script); rec.code != 0 {
		t.Fatalf("record\n%s", rec)
	}
	saved, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		args []string
		want string
	}{
		{[]string{"--replay", log, "--allow", "net=127.0.0.1"}, "drop --allow"},
		{[]string{"--replay", log, "--timeout", "5s"}, "drop the limit flags"},
		{[]string{"--resume", log, "--allow", "net=127.0.0.1", "--steps", "9"}, "drop the limit flags"},
		{[]string{"--record", log, "--replay", log}, "one of --record"},
		{[]string{"--fsync", "--replay", log}, "--fsync needs"},
		{[]string{"--resume", log, "--allow", "net=localhost"}, "resume needs the grants"},
		{[]string{"--resume", log}, "resume needs the grants"},
		{[]string{"--replay", filepath.Join(dir, "missing.jsonl")}, "missing.jsonl"},
	} {
		res := logRun(t, dir, append(append([]string{"run"}, tt.args...), script)...)
		// A finished log names its grants on a line before the error.
		clean := strings.HasPrefix(res.stderr, "error: ") || strings.HasPrefix(res.stderr, "replay: the run finished") && strings.Contains(res.stderr, "\nerror: ")
		if res.code == 0 || res.stdout != "" || !clean || !strings.Contains(res.stderr, tt.want) {
			t.Errorf("%q: want a clean error with %q\n%s", tt.args, tt.want, res)
		}
	}
	if hits(t, "/ok") != 1 {
		t.Fatalf("a refused run made %d requests", hits(t, "/ok")-1)
	}
	if now, _ := os.ReadFile(log); !bytes.Equal(now, saved) {
		t.Fatal("a refused run changed the log")
	}
	// A finished log resumes as a replay: nothing live, nothing appended.
	res := logRun(t, dir, "run", "--resume", log, "--allow", "net=127.0.0.1", script)
	if res.code != 0 || res.stdout != "200\n" || !strings.Contains(res.stderr, "replay: the run finished") || hits(t, "/ok") != 1 {
		t.Fatalf("resume of a finished run\n%s", res)
	}
	if now, _ := os.ReadFile(log); !bytes.Equal(now, saved) {
		t.Fatal("the resume of a finished run changed the log")
	}
}

// A log cut in the middle of a line (a crash while writing it) resumes:
// the partial line is dropped and the effect it held runs again.
func TestP6ResumeAfterAPartialLine(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	script := p6Script(t, dir, "two.arv", `puts(http_get("{{SERVER}}/ok?id={{ID}}&n=1")["body"]);
puts(http_get("{{SERVER}}/ok?id={{ID}}&n=2")["body"])`)
	log := filepath.Join(dir, "run.jsonl")
	rec := logRun(t, dir, "run", "--record", log, "--allow", "net=127.0.0.1", script)
	if rec.code != 0 || hits(t, "/ok") != 2 {
		t.Fatalf("record\n%s", rec)
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	// Keep the header, the first request and its puts, then half of
	// the second request's line.
	lines := strings.SplitAfter(string(data), "\n")
	var keep strings.Builder
	n := 0
	for _, l := range lines {
		if strings.Contains(l, `"kind":"http_get"`) {
			n++
			if n == 2 {
				keep.WriteString(l[:len(l)/2])
				break
			}
		}
		keep.WriteString(l)
	}
	mustWrite(t, log, keep.String())
	rep := logRun(t, dir, "run", "--replay", log, script)
	if rep.code != 1 || !strings.Contains(rep.stderr, "did not finish") {
		t.Fatalf("a replay of a cut log\n%s", rep)
	}
	for _, allow := range [][]string{nil, {"--allow", "net=localhost"}, {"--allow", "net=127.0.0.1", "--allow", "env=HOME"}} {
		bad := logRun(t, dir, append(append([]string{"run", "--resume", log}, allow...), script)...)
		if bad.code != 1 || bad.stdout != "" || !strings.HasPrefix(bad.stderr, "error: ") || !strings.Contains(bad.stderr, "resume needs the grants") {
			t.Fatalf("a resume with other grants\n%s", bad)
		}
	}
	// A refused resume leaves the log as it was, partial line and all.
	if now, _ := os.ReadFile(log); string(now) != keep.String() || hits(t, "/ok") != 2 {
		t.Fatalf("a refused resume ran: %d requests, log changed: %v", hits(t, "/ok"), string(now) != keep.String())
	}
	res := logRun(t, dir, "run", "--resume", log, "--allow", "net=127.0.0.1", script)
	if res.code != 0 || res.stdout != rec.stdout || hits(t, "/ok") != 3 {
		t.Fatalf("resume: %d requests\n%s", hits(t, "/ok"), res)
	}
	if rep := logRun(t, dir, "run", "--replay", log, script); rep.code != 0 || rep.stdout != rec.stdout || hits(t, "/ok") != 3 {
		t.Fatalf("replay of the resumed log\n%s", rep)
	}
}
