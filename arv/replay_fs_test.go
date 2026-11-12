package arv

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
)

// I7 for the filesystem: a replay returns the logged read_file content
// and write_file result, and a spawn's fs grants from the log, after the
// files have changed or gone; it reads, writes and lstats nothing.
func TestReplayOfFileEffects(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	in, out, inner := filepath.Join(dir, "in.txt"), filepath.Join(dir, "out.txt"), filepath.Join(sub, "inner.txt")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	for f, s := range map[string]string{in: "recorded", inner: "inner recorded"} {
		if err := os.WriteFile(f, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	src := fmt.Sprintf(`let r = fn() { read_file(%q) };
puts(read_file(%q)); puts(write_file(%q, "new"));
puts(await spawn r() with [fs_read(%q)])`, inner, in, out, sub)
	caps := []runtime.Capability{{Kind: runtime.FSRead, Scope: dir}, {Kind: runtime.FSWrite, Scope: dir}}
	recorded, log, err := recordRun(t, src, Options{Caps: caps})
	if err != nil || recorded != "recorded\nnil\ninner recorded\n" {
		t.Fatalf("recording: %v, %q", err, recorded)
	}
	if err := os.WriteFile(in, []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(out); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(sub); err != nil {
		t.Fatal(err)
	}
	replayed, err := replayRun(t, src, log)
	if err != nil || replayed != recorded {
		t.Fatalf("replay: %v, %q; recorded %q", err, replayed, recorded)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatalf("the replay wrote %s: %v", out, err)
	}
	if _, err := os.Stat(sub); !os.IsNotExist(err) {
		t.Fatalf("%s: %v", sub, err)
	}
}
