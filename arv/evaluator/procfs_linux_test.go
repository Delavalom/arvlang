//go:build linux

package evaluator

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
)

// /proc/self/environ holds the environment the process started with,
// ANTHROPIC_API_KEY included even after os.Unsetenv. No file on procfs is
// read or written, however the path reaches it (review round 1).
func TestProcFSIsRefused(t *testing.T) {
	t.Setenv("ARV_PROCFS_PROBE", "sk-ant-PROBE")
	dir := t.TempDir()
	link := filepath.Join(dir, "env")
	if err := os.Symlink("/proc/self/environ", link); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ path, grant string }{
		{"/proc/self/environ", "/proc"},
		{"/proc/self/environ", "/"},
		{fmt.Sprintf("/proc/%d/environ", os.Getpid()), "/proc"},
		{"/proc/self/status", "/proc"},
		{link, "/"},
	} {
		got, _ := runWith(t, fmt.Sprintf(`read_file(%q)`, c.path), grant(t, runtime.FSRead, c.grant))
		wantErrorHas(t, got, "procfs")
	}
	got, _ := runWith(t, `write_file("/proc/self/comm", "x")`, grant(t, runtime.FSWrite, "/proc"))
	wantErrorHas(t, got, "procfs")
	// Other files still read.
	f := filepath.Join(dir, "ok.txt")
	if err := os.WriteFile(f, []byte("fine"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, _ := runWith(t, fmt.Sprintf(`read_file(%q)`, f), grant(t, runtime.FSRead, "/")); got.Inspect() != "fine" {
		t.Fatalf("read_file(%q) = %s", f, got.Inspect())
	}
}
