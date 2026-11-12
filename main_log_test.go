package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func runArv(args ...string) (int, string, string) {
	var out, errOut bytes.Buffer
	code := runCLI(args, &out, &errOut)
	return code, out.String(), errOut.String()
}

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
