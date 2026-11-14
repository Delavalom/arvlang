package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// Review round 3 (Minor): --record no longer empties its file before the
// script parses, so a typo keeps the earlier recording.
func TestRecordOfABadScriptKeepsTheOldLog(t *testing.T) {
	dir := t.TempDir()
	good, bad, log := filepath.Join(dir, "f.arv"), filepath.Join(dir, "bad.arv"), filepath.Join(dir, "keep.log")
	os.WriteFile(good, []byte(`puts("a")`), 0o644)
	os.WriteFile(bad, []byte(`puts(`), 0o644)
	if code, _, stderr := runArv("run", "--record", log, good); code != 0 {
		t.Fatalf("record: exit %d, %s", code, stderr)
	}
	before, _ := os.ReadFile(log)
	if code, _, _ := runArv("run", "--record", log, bad); code != 1 {
		t.Fatalf("a bad script: exit %d, want 1", code)
	}
	if after, _ := os.ReadFile(log); len(before) == 0 || string(after) != string(before) {
		t.Fatalf("the log was %d bytes, now %d", len(before), len(after))
	}
}

// Review round 3 (Minor): --fsync installs the sync, for --record and
// for --resume, and it runs after every line.
func TestFsyncRunsAfterEveryLine(t *testing.T) {
	var n atomic.Int64
	old := syncOf
	defer func() { syncOf = old }()
	syncOf = func(f *os.File) func() error { return func() error { n.Add(1); return f.Sync() } }
	dir := t.TempDir()
	script, log := filepath.Join(dir, "f.arv"), filepath.Join(dir, "f.log")
	os.WriteFile(script, []byte(`puts("a"); puts("b")`), 0o644)
	if code, _, stderr := runArv("run", "--fsync", "--record", log, script); code != 0 || n.Load() != 4 {
		t.Fatalf("record: exit %d, %s; %d syncs, want 4 (header, two lines, end)", code, stderr, n.Load())
	}
	// Cut the end line off, and resume: one line more, synced.
	data, _ := os.ReadFile(log)
	lines := strings.SplitAfter(string(data), "\n")
	os.WriteFile(log, []byte(strings.Join(lines[:len(lines)-2], "")), 0o600)
	n.Store(0)
	if code, out, stderr := runArv("run", "--fsync", "--resume", log, script); code != 0 || out != "a\nb\n" || n.Load() != 1 {
		t.Fatalf("resume: exit %d, %q, %s; %d syncs, want 1 (the end)", code, out, stderr, n.Load())
	}
}

// Review round 3 (Minor): --resume of a finished log replays it, says so
// with the grants, and leaves the file as it was.
func TestResumeOfAFinishedLog(t *testing.T) {
	dir := t.TempDir()
	script, log := filepath.Join(dir, "n.arv"), filepath.Join(dir, "n.log")
	os.WriteFile(script, []byte(`puts(now()); puts(random(100000))`), 0o644)
	code, out, stderr := runArv("run", "--record", log, script)
	if code != 0 {
		t.Fatalf("record: exit %d, %s", code, stderr)
	}
	before, _ := os.ReadFile(log)
	code, rout, stderr := runArv("run", "--resume", log, script)
	after, _ := os.ReadFile(log)
	if code != 0 || rout != out || string(after) != string(before) || !strings.Contains(stderr, "replay: the run finished; grants: none") {
		t.Fatalf("resume: exit %d, %q (want %q), err %q, file changed: %v", code, rout, out, stderr, string(after) != string(before))
	}
}
