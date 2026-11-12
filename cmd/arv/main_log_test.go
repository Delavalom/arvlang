package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLogFlagErrors(t *testing.T) {
	for _, tt := range []struct {
		args []string
		want string
	}{
		{[]string{"run", "--record", "a", "--replay", "b", "x.arv"}, "use one of --record, --replay and --resume"},
		{[]string{"run", "--fsync", "x.arv"}, "--fsync needs --record or --resume"},
		{[]string{"run", "--replay", "a", "--allow", "env=HOME", "x.arv"}, "--replay takes its grants from the log"},
		{[]string{"run", "--resume", "a", "--steps", "5", "x.arv"}, "take their limits from the log"},
	} {
		if code, _, stderr := runArv(tt.args...); code != 2 || !strings.Contains(stderr, tt.want) {
			t.Errorf("%v: exit %d, stderr %q; want 2 and %q", tt.args, code, stderr, tt.want)
		}
	}
}

// --record then --replay through the CLI: the same output, without the
// sleep, the grants named on stderr.
func TestRecordThenReplay(t *testing.T) {
	dir := t.TempDir()
	script, log := filepath.Join(dir, "p.arv"), filepath.Join(dir, "run.jsonl")
	src := `let f = fn() { random(1000000) }; sleep(1500); puts(now()); puts(await spawn f()); puts(getenv("HOME"))`
	if err := os.WriteFile(script, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	code, recorded, stderr := runArv("run", "--record", log, "--fsync", "--allow", "env=HOME", script)
	if code != 0 || stderr != "" {
		t.Fatalf("record: exit %d, %q", code, stderr)
	}
	if fi, err := os.Stat(log); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("log: %v, %v", fi, err) // it may hold secrets: getenv's values
	}
	start := time.Now()
	code, replayed, stderr := runArv("run", "--replay", log, script)
	if took := time.Since(start); took >= 1500*time.Millisecond {
		t.Errorf("replay took %s", took)
	}
	if code != 0 || replayed != recorded || !strings.Contains(stderr, `replay: grants: env("HOME")`) {
		t.Fatalf("replay: exit %d, printed %q (recorded %q), stderr %q", code, replayed, recorded, stderr)
	}
	os.WriteFile(script, []byte(src+"; puts(1)"), 0o644)
	code, _, stderr = runArv("run", "--replay", log, script)
	if code != 1 || !strings.Contains(stderr, "has changed since the log was recorded") || !strings.Contains(stderr, "replay diverged") {
		t.Fatalf("edited script: exit %d, %q", code, stderr)
	}
}

// --resume after a crash in the middle of a write: the partial last line
// is cut off, the resumed records follow the last whole line, and the log
// then replays (p6.12). The variable is shorter when resumed, so what the
// resumed run appends is shorter than the partial line: left there, the
// rest of it would break the log.
func TestResumeCutsAPartialLine(t *testing.T) {
	dir := t.TempDir()
	script, log := filepath.Join(dir, "p.arv"), filepath.Join(dir, "run.jsonl")
	src := `sleep(1); puts("a"); puts(len(getenv("ARV_P6_LONG"))); puts("c")`
	if err := os.WriteFile(script, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ARV_P6_LONG", strings.Repeat("x", 8000))
	code, _, stderr := runArv("run", "--record", log, "--allow", "env=ARV_P6_LONG", script)
	if code != 0 {
		t.Fatalf("record: exit %d, %q", code, stderr)
	}
	data, _ := os.ReadFile(log)
	lines := strings.SplitAfter(string(data), "\n")
	crashed := strings.Join(lines[:3], "") + lines[3][:len(lines[3])/2] // header, sleep, puts a, half of getenv
	if err := os.WriteFile(log, []byte(crashed), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, _, stderr := runArv("run", "--resume", log, script); code != 1 || !strings.Contains(stderr, "resume needs the grants the run was recorded with") {
		t.Fatalf("resume without --allow: exit %d, %q", code, stderr)
	}
	t.Setenv("ARV_P6_LONG", "y")
	code, resumed, stderr := runArv("run", "--resume", log, "--allow", "env=ARV_P6_LONG", script)
	if code != 0 || resumed != "a\n1\nc\n" {
		t.Fatalf("resume: exit %d, printed %q, %q", code, resumed, stderr)
	}
	code, replayed, stderr := runArv("run", "--replay", log, script)
	if code != 0 || replayed != resumed {
		t.Fatalf("replay of the resumed log: exit %d, printed %q, %q", code, replayed, stderr)
	}
}

// A replay from another directory warns that relative paths resolve
// against it, not the recorded one.
func TestReplayWarnsOfAnotherDirectory(t *testing.T) {
	dir := t.TempDir()
	script, log := filepath.Join(dir, "p.arv"), filepath.Join(dir, "run.jsonl")
	if err := os.WriteFile(script, []byte(`puts(now())`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	if code, _, stderr := runArv("run", "--record", log, script); code != 0 {
		t.Fatalf("record: exit %d, %q", code, stderr)
	}
	if _, _, stderr := runArv("run", "--replay", log, script); strings.Contains(stderr, "warning") {
		t.Fatalf("replay in the same directory: %q", stderr)
	}
	t.Chdir(t.TempDir())
	code, _, stderr := runArv("run", "--replay", log, script)
	if code != 0 || !strings.Contains(stderr, "warning: the log was recorded in ") || !strings.Contains(stderr, "relative paths resolve against") {
		t.Fatalf("replay elsewhere: exit %d, %q", code, stderr)
	}
}

// --record onto an existing file leaves it with mode 0600, as a new one:
// the log may hold secrets (getenv's values).
func TestRecordMakesAnOldFilePrivate(t *testing.T) {
	dir := t.TempDir()
	script, log := filepath.Join(dir, "p.arv"), filepath.Join(dir, "old.jsonl")
	if err := os.WriteFile(script, []byte(`puts(len(getenv("HOME")))`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(log, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, stderr := runArv("run", "--record", log, "--allow", "env=HOME", script); code != 0 {
		t.Fatalf("record: exit %d, %q", code, stderr)
	}
	if fi, err := os.Stat(log); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("log: %v, %v", fi.Mode(), err)
	}
}

// A replay whose header grants are not valid fails before it names them.
func TestReplayChecksTheGrantsBeforeNamingThem(t *testing.T) {
	dir := t.TempDir()
	script, log := filepath.Join(dir, "p.arv"), filepath.Join(dir, "run.jsonl")
	if err := os.WriteFile(script, []byte(`1`), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, stderr := runArv("run", "--record", log, "--allow", "env=HOME", script); code != 0 {
		t.Fatalf("record: exit %d, %q", code, stderr)
	}
	data, _ := os.ReadFile(log)
	if err := os.WriteFile(log, rechain(t, strings.Replace(string(data), `"kind":"env"`, `"kind":"bogus"`, 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := runArv("run", "--replay", log, script)
	if code != 1 || !strings.Contains(stderr, "not a canonical capability") || strings.Contains(stderr, "replay: grants") {
		t.Fatalf("exit %d, %q", code, stderr)
	}
}

// rechain recomputes a log's hash chain after an edit.
func rechain(t *testing.T, log string) []byte {
	t.Helper()
	var out strings.Builder
	prev := ""
	for _, line := range strings.SplitAfter(log, "\n") {
		line = strings.TrimSuffix(line, "\n")
		k := strings.LastIndex(line, `,"h":"`)
		if k < 0 {
			continue
		}
		body := line[:k] + "}"
		sum := sha256.Sum256([]byte(prev + body))
		prev = hex.EncodeToString(sum[:])
		out.WriteString(line[:k] + `,"h":"` + prev + `"}` + "\n")
	}
	return []byte(out.String())
}
