package evaluator

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
)

// readCapped made a 32 KiB chunk before its first read and charged only
// the bytes read, so each read_file of a 2-byte file allocated over 32 KiB
// for 2 charged bytes. It now sizes its first chunk from the file's size
// (or the response's Content-Length) plus one byte to see the end.
func TestReadSmallFileAllocatesLittle(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "small")
	if err := os.WriteFile(p, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	src := fmt.Sprintf(`let p = "%s"; for i in %s { read_file(p) }; "ok"`, p, zeros(1000))
	var out string
	used := heapDuring(func() {
		got, _ := runWith(t, src, grant(t, runtime.FSRead, dir))
		out = got.Inspect()
	})
	if out != "ok" {
		t.Fatalf("got %.200s", out)
	}
	// Checking and opening the path costs a few KiB a call.
	if used > 16<<20 {
		t.Errorf("1000 reads of a 2-byte file allocated %d MiB", used>>20)
	}
}

// A size hint that is wrong only costs another chunk: the read is whole.
func TestReadCappedWrongHint(t *testing.T) {
	for _, tt := range []struct {
		size int
		hint int64
	}{{0, 0}, {2, 2}, {10, 2}, {readChunk + 5, 3}, {2, readChunk * 4}, {readChunk * 3, -1}} {
		src := strings.Repeat("z", tt.size)
		got, err := readCapped(strings.NewReader(src), tt.hint, nil)
		if err != nil || got != src {
			t.Errorf("size %d hint %d: got %d bytes, %v", tt.size, tt.hint, len(got), err)
		}
	}
}
