package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Review round 3: a forged cancellation (chain recomputed) made the
// binary abort with "fatal error: all goroutines are asleep - deadlock!"
// and exit 2. Now it is a clean divergence, exit 1.
func TestCLIForgedCancellationIsACleanError(t *testing.T) {
	dir := t.TempDir()
	script, log := filepath.Join(dir, "f.arv"), filepath.Join(dir, "f.log")
	if err := os.WriteFile(script, []byte("puts(\"a\");\nputs(\"b\");\n1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, stderr := runArv("run", "--record", log, script); code != 0 {
		t.Fatalf("record: exit %d, %s", code, stderr)
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if strings.Contains(line, `"req":["b"]`) {
			lines = append(lines, `{"task":"root","seq":2,"kind":"cancelled","check":2,"h":"x"}`)
			line = strings.Replace(line, `"seq":2`, `"seq":3`, 1)
		}
		lines = append(lines, line)
	}
	if err := os.WriteFile(log, rechain(t, strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	type result struct {
		code        int
		out, stderr string
	}
	done := make(chan result, 1)
	go func() {
		code, out, stderr := runArv("run", "--replay", log, script)
		done <- result{code, out, stderr}
	}()
	select {
	case r := <-done:
		if r.code != 1 || r.out != "a\n" || !strings.Contains(r.stderr, "replay diverged") {
			t.Fatalf("exit %d, out %q, err %q; want exit 1 and a divergence after a", r.code, r.out, r.stderr)
		}
	case <-time.After(60 * time.Second):
		t.Fatal("the replay hung")
	}
}
