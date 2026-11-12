package evaluator

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
)

// checkRegular refuses a file its procfs check reports, on every platform
// (mutation M30: TestProcFSIsRefused runs only on Linux). The check is
// swapped for one that reports files named "*.procfs", so read_file and
// write_file reach the refusal on macOS too, and the refused file is
// closed. Not parallel: it swaps a package variable.
func TestCheckRegularRefusesWhatProcFSReports(t *testing.T) {
	saved := procFS
	t.Cleanup(func() { procFS = saved })
	procFS = func(f *os.File) bool { return strings.HasSuffix(f.Name(), ".procfs") }

	dir := t.TempDir()
	fake := filepath.Join(dir, "environ.procfs")
	ok := filepath.Join(dir, "ok.txt")
	for _, p := range []string{fake, ok} {
		if err := os.WriteFile(p, []byte("sk-ant-PROBE"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := runWith(t, fmt.Sprintf(`read_file(%q)`, fake), grant(t, runtime.FSRead, dir))
	wantErrorHas(t, got, "procfs")
	got, _ = runWith(t, fmt.Sprintf(`write_file(%q, "x")`, fake), grant(t, runtime.FSWrite, dir))
	wantErrorHas(t, got, "procfs")
	if b, err := os.ReadFile(fake); err != nil || string(b) != "sk-ant-PROBE" {
		t.Fatalf("the refused file now holds %q, %v", b, err)
	}
	if got, _ := runWith(t, fmt.Sprintf(`read_file(%q)`, ok), grant(t, runtime.FSRead, dir)); got.Inspect() != "sk-ant-PROBE" {
		t.Fatalf("read_file(%q) = %s", ok, got.Inspect())
	}

	f, err := os.Open(fake)
	if err != nil {
		t.Fatal(err)
	}
	if g, err := checkRegular(f); g != nil || err == nil || !strings.Contains(err.Error(), "procfs") {
		t.Fatalf("checkRegular = %v, %v; want a procfs refusal", g, err)
	}
	if err := f.Close(); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("the refused file was left open: Close = %v", err)
	}
}
