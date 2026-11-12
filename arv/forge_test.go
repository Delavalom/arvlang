package arv

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/delavalom/arvlang/arv/runtime"
)

// forge rewrites a log line by line, as someone who can write the file
// can: edit gets each line's body (the line without its hash, as JSON)
// and returns the new body, or nil to drop the line; the hash chain is
// then recomputed, so only the log's content can give the edit away.
func forge(t testing.TB, log []byte, edit func(i int, body []byte) []byte) []byte {
	t.Helper()
	var out bytes.Buffer
	prev := ""
	for i, line := range bytes.SplitAfter(log, []byte("\n")) {
		line = bytes.TrimSuffix(line, []byte("\n"))
		if len(line) == 0 {
			continue
		}
		body := line
		if k := bytes.LastIndex(line, []byte(`,"h":"`)); k >= 0 {
			body = append(append([]byte(nil), line[:k]...), '}')
		}
		if body = edit(i, body); body == nil {
			continue
		}
		sum := sha256.Sum256(append([]byte(prev), body...))
		prev = hex.EncodeToString(sum[:])
		out.Write(body[:len(body)-1])
		out.WriteString(`,"h":"` + prev + `"}` + "\n")
	}
	return out.Bytes()
}

// replayForged replays src against a forged log with no host context,
// as the CLI does, so nothing but the replay can end it (since review
// round 3 a replay that stalls diverges), and returns the error. It fails
// the test on a panic, or if the replay has not ended after 60 s.
func replayForged(t testing.TB, src string, log []byte, opts Options) error {
	t.Helper()
	l, err := runtime.ReadLog(bytes.NewReader(log))
	if err != nil {
		return err
	}
	opts.Replay = l
	done := make(chan error, 1)
	go func() {
		_, err := Run(src, opts)
		done <- err
	}()
	select {
	case err = <-done:
	case <-time.After(60 * time.Second):
		t.Fatal("a forged replay hung")
	}
	if err != nil && strings.Contains(err.Error(), "panicked") {
		t.Fatalf("replay panicked: %v", err)
	}
	return err
}

// A forged log whose chain is still valid is refused with a clean error,
// and nothing runs live (I7).
func TestForgedLogsAreRefused(t *testing.T) {
	srv, hits := countedServer(t)
	src := fmt.Sprintf(`puts(now()); puts(http_get(%q)["status"])`, srv.URL)
	_, log, err := recordRun(t, src, Options{Caps: loopback})
	if err != nil {
		t.Fatal(err)
	}
	header := func(old, new string) []byte {
		return forge(t, log, func(i int, b []byte) []byte {
			if i == 0 {
				return bytes.Replace(b, []byte(old), []byte(new), 1)
			}
			return b
		})
	}
	nowRes := regexp.MustCompile(`("kind":"now","req":\[\],"res":)\d+`)
	for name, tt := range map[string]struct {
		log  []byte
		want string
	}{
		"float result": {forge(t, log, func(_ int, b []byte) []byte { return nowRes.ReplaceAll(b, []byte("${1}1.5")) }), "the log's result for now does not decode"},
		"bogus grant":  {header(`"kind":"net"`, `"kind":"bogus"`), "not a canonical capability"},
		"uncanonical":  {header(`"scope":"127.0.0.1"`, `"scope":"127.0.0.1:0"`), "not a canonical capability"},
		// Scopes Literal accepts but rewrites (port 080 to 80, an
		// IPv4-mapped /128 to /32): the header must hold the canonical
		// form itself, not one Literal would normalise (mutation M24).
		"port not canonical":   {header(`"scope":"127.0.0.1"`, `"scope":"127.0.0.1:080"`), "not a canonical capability"},
		"prefix not canonical": {header(`"scope":"127.0.0.1"`, `"scope":"::ffff:127.0.0.1/128"`), "not a canonical capability"},
		"bad timeout":          {header(`"limits":{}`, `"limits":{"timeout_ms":-5}`), "the log's header"},
		// The first record begins after it ends (its at, in ms, is far
		// below, or 0 and left out when the run took under 1 ms, which
		// left the old edit, keyed on "at", with nothing to change).
		"times reversed": {forge(t, log, func(i int, b []byte) []byte {
			if i == 1 {
				return append(bytes.TrimSuffix(b, []byte("}")), []byte(`,"began":99999999}`)...)
			}
			return b
		}), "out of order"},
	} {
		err := replayForged(t, src, tt.log, Options{})
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: err = %v, want %q", name, err, tt.want)
		}
	}
	l, _ := runtime.ReadLog(bytes.NewReader(log))
	for name, opts := range map[string]Options{
		"replay and resume": {Replay: l, Resume: l},
		"replay and record": {Replay: l, Record: &bytes.Buffer{}},
		"resume, no record": {Resume: l, Caps: loopback},
	} {
		if _, err := Run(src, opts); err == nil || !strings.HasPrefix(err.Error(), "arv: ") {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if hits.Load() != 1 {
		t.Fatalf("%d requests: a forged replay went live", hits.Load())
	}
}

// Fuzz property: whatever a forged log holds, its chain recomputed so
// only its content can give it away, a replay of it never panics, never
// makes a request, never writes a file, and ends with no host deadline.
// The input is the log's lines without their hashes.
func FuzzForgedReplay(f *testing.F) {
	srv, hits := countedServer(f)
	dir := f.TempDir()
	in, out := filepath.Join(dir, "in.txt"), filepath.Join(dir, "out.txt")
	if err := os.WriteFile(in, []byte("data"), 0o644); err != nil {
		f.Fatal(err)
	}
	caps := []runtime.Capability{loopback[0], {Kind: runtime.FSRead, Scope: dir}, {Kind: runtime.FSWrite, Scope: dir}}
	src := fmt.Sprintf(`let g = fn() { http_get(%q)["body"] };
let h = spawn g() with [net("127.0.0.1")];
puts(read_file(%q)); puts(now()); puts(random(9)); puts(await h);
write_file(%q, "x"); scope (timeout_ms: 60000) { puts(clock_ms() > 0) }`, srv.URL, in, out)
	_, log, err := recordRun(f, src, Options{Caps: caps})
	if err != nil {
		f.Fatal(err)
	}
	if err := os.Remove(out); err != nil {
		f.Fatal(err)
	}
	strip := func(log []byte) []byte {
		return regexp.MustCompile(`,"h":"[0-9a-f]{64}"`).ReplaceAll(log, nil)
	}
	f.Add(strip(log))
	f.Add(strip(bytes.Replace(log, []byte(`"kind":"random"`), []byte(`"cancelled":true,"kind":"random"`), 1)))
	f.Add(strip(bytes.Replace(log, []byte(`"task":"root/0","seq":1`), []byte(`"task":"root/0","seq":1,"check":3`), 1)))
	f.Add(strip(bytes.Replace(log, []byte(`"res":[["net","127.0.0.1"]]`), []byte(`"res":[["fs_write","/"]]`), 1)))
	before := hits.Load()
	f.Fuzz(func(t *testing.T, bodies []byte) {
		forged := forge(t, bodies, func(_ int, b []byte) []byte { return b })
		start := time.Now()
		replayForged(t, src, forged, Options{})
		if took := time.Since(start); took > 30*time.Second {
			t.Fatalf("the replay took %s", took)
		}
		if _, err := os.Stat(out); hits.Load() != before || !os.IsNotExist(err) {
			t.Fatalf("a forged replay went live: %d requests, %s: %v", hits.Load()-before, out, err)
		}
	})
}
