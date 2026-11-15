package e2e

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Phase 6: tampered, truncated and mismatched logs through the binary.
// Every one is a clean error (or, for a log from another directory or
// script, a warning and then a divergence), never a panic, and never a
// live effect.

// rechain recomputes a log's hash chain after its lines were edited, as
// a forger who can write the file would.
func rechain(log []byte) []byte {
	var out bytes.Buffer
	prev := ""
	for _, line := range bytes.SplitAfter(log, []byte("\n")) {
		line = bytes.TrimSuffix(line, []byte("\n"))
		if len(line) == 0 {
			continue
		}
		body := line
		if k := bytes.LastIndex(line, []byte(`,"h":"`)); k >= 0 {
			body = append(bytes.Clone(line[:k]), '}')
		}
		sum := sha256.Sum256(append([]byte(prev), body...))
		prev = hex.EncodeToString(sum[:])
		out.Write(body[:len(body)-1])
		out.WriteString(`,"h":"` + prev + "\"}\n")
	}
	return out.Bytes()
}

// p6Recorded records script (a program making one request per n) in a
// fresh directory and returns the directory, the script and the log.
func p6Recorded(t *testing.T, src string) (string, string, []byte) {
	t.Helper()
	dir := t.TempDir()
	script := p6Script(t, dir, "p.arv", src)
	log := filepath.Join(dir, "run.jsonl")
	if rec := logRun(t, dir, "run", "--record", log, "--allow", "net=127.0.0.1", script); rec.code != 0 {
		t.Fatalf("record\n%s", rec)
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	return dir, script, data
}

// One changed byte anywhere in any line (a field, a value or the hash)
// is refused by --replay and by --resume, before anything runs, and the
// resume leaves the file as it was.
func TestP6EveryLineIsChained(t *testing.T) {
	t.Parallel()
	dir, script, good := p6Recorded(t, `puts(http_get("{{SERVER}}/ok?id={{ID}}&n=1")["body"]);
let h = spawn fn() { http_get("{{SERVER}}/ok?id={{ID}}&n=2")["status"] }() with [net("127.0.0.1")];
puts(await h)`)
	lines := bytes.SplitAfter(good, []byte("\n"))
	lines = lines[:len(lines)-1] // the empty string after the last newline
	bad := filepath.Join(dir, "bad.jsonl")
	for i, line := range lines {
		for _, at := range []int{2, len(line) / 2, len(line) - 5} { // a key, the middle, the hash
			edited := bytes.Clone(good)
			off := len(bytes.Join(lines[:i], nil)) + at
			edited[off] ^= 0x01
			for _, mode := range []string{"--replay", "--resume"} {
				mustWrite(t, bad, string(edited))
				args := []string{"run", mode, bad}
				if mode == "--resume" {
					args = append(args, "--allow", "net=127.0.0.1")
				}
				res := logRun(t, dir, append(args, script)...)
				if res.code != 1 || res.stdout != "" || !strings.HasPrefix(res.stderr, "error: ") {
					t.Errorf("line %d, byte %d, %s: want a clean error\n%s", i+1, at, mode, res)
				}
				if now, _ := os.ReadFile(bad); !bytes.Equal(now, edited) {
					t.Errorf("line %d, byte %d, %s changed the file", i+1, at, mode)
				}
			}
		}
	}
	if hits(t, "/ok") != 2 {
		t.Fatalf("a tampered log ran %d live requests", hits(t, "/ok")-2)
	}
}

// Logs whose chain is whole but whose lines are not a run: swapped
// lines, a duplicated line, a log of version 1, a second header. Each is
// refused before anything runs.
func TestP6StructurallyBadLogs(t *testing.T) {
	t.Parallel()
	dir, script, good := p6Recorded(t, `puts(http_get("{{SERVER}}/ok?id={{ID}}&n=1")["body"]);
puts(http_get("{{SERVER}}/ok?id={{ID}}&n=2")["body"])`)
	lines := strings.SplitAfter(string(good), "\n")
	swap := append([]string{}, lines...)
	swap[1], swap[3] = swap[3], swap[1]
	bad := filepath.Join(dir, "bad.jsonl")
	for _, tt := range []struct{ name, log, want string }{
		{"swapped lines, chained", string(rechain([]byte(strings.Join(swap, "")))), "where 1 comes next"},
		{"swapped lines", strings.Join(swap, ""), "hash chain broken"},
		{"a line twice, chained", string(rechain([]byte(lines[0] + lines[1] + lines[1] + strings.Join(lines[2:], "")))), "comes next"},
		{"version 1", string(rechain([]byte(strings.Replace(string(good), `{"v":3,`, `{"v":1,`, 1)))), "record the run again"},
		{"version 2", string(rechain([]byte(strings.Replace(string(good), `{"v":3,`, `{"v":2,`, 1)))), "record the run again"},
		{"a second header", string(rechain([]byte(lines[0] + lines[0] + strings.Join(lines[1:], "")))), "second header"},
		{"bytes after the end", string(good) + "{}", "after the end"},
	} {
		mustWrite(t, bad, tt.log)
		for _, mode := range []string{"--replay", "--resume"} {
			args := []string{"run", mode, bad}
			if mode == "--resume" {
				args = append(args, "--allow", "net=127.0.0.1")
			}
			res := logRun(t, dir, append(args, script)...)
			if res.code != 1 || res.stdout != "" || !strings.HasPrefix(res.stderr, "error: ") || !strings.Contains(res.stderr, tt.want) {
				t.Errorf("%s, %s: want a clean error with %q\n%s", tt.name, mode, tt.want, res)
			}
		}
	}
	if hits(t, "/ok") != 2 {
		t.Fatalf("a bad log ran %d live requests", hits(t, "/ok")-2)
	}
}

// A log from another directory warns, and replays (nothing in it is a
// relative path); a log of another script warns, then diverges at the
// first effect that differs, naming both.
func TestP6MismatchedLogs(t *testing.T) {
	t.Parallel()
	dir, script, good := p6Recorded(t, `puts(http_get("{{SERVER}}/ok?id={{ID}}&n=1")["body"])`)
	other := t.TempDir()
	log := filepath.Join(other, "run.jsonl")
	mustWrite(t, log, string(good))
	rep := logRun(t, other, "run", "--replay", log, script)
	if rep.code != 0 || rep.stdout != okBody+"\n" || !strings.Contains(rep.stderr, "warning: the log was recorded in ") ||
		!strings.Contains(rep.stderr, filepath.Base(dir)) {
		t.Fatalf("a replay in another directory\n%s", rep)
	}
	otherScript := p6Script(t, dir, "q.arv", `puts(getenv("HOME"))`)
	rep = logRun(t, dir, "run", "--replay", log, otherScript)
	for _, want := range []string{"has changed since the log was recorded", "replay diverged at task root, effect 1", "http_get(", "getenv(\"HOME\")"} {
		if !strings.Contains(rep.stderr, want) {
			t.Errorf("stderr lacks %q", want)
		}
	}
	if rep.code != 1 || rep.stdout != "" || hits(t, "/ok") != 1 {
		t.Fatalf("a replay of another script: %d requests\n%s", hits(t, "/ok"), rep)
	}
}
