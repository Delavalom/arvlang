package evaluator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
)

// write_file writes its text without copying it. It converted the text to
// a new []byte on every call, uncharged, so a loop writing a 1 MB string
// 1000 times allocated 965 MiB under alloc_mb: 1, and the scope succeeded.
func TestWriteFileDoesNotCopyItsText(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out")
	text := strings.Repeat("A", 1000000)
	src := `let s = "` + text + `"; let p = "` + path + `"; ` +
		`scope (alloc_mb: 1) { for i in ` + zeros(200) + ` { write_file(p, s) }; "ok" }`
	var got string
	used := heapDuring(func() {
		v, _ := runWith(t, src, grant(t, runtime.FSWrite, dir))
		got = v.Inspect()
	})
	if got != "ok" {
		t.Fatalf("got %.80s, want ok", got)
	}
	if used > 32<<20 {
		t.Errorf("200 writes of a 1 MB string allocated %d MiB", used>>20)
	}
	if b, err := os.ReadFile(path); err != nil || string(b) != text {
		t.Errorf("the file holds %d bytes (%v), want the text", len(b), err)
	}
}
