package evaluator

import (
	"fmt"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// deepFile makes a file about 1000 bytes deep (macOS's PATH_MAX is 1024)
// under 455 or so directories, and returns its path and the top directory.
func deepFile(t *testing.T) (path, dir string) {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	deep := dir + strings.Repeat("/a", (1000-len(dir)-6)/2)
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(deep+"/f", []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	return deep + "/f", dir
}

// Canonicalising a deep path lstats every prefix of it, about 620 KB for
// one read_file of a file 455 directories deep, and none of it was
// charged: 1000 calls churned 615 MiB under alloc_mb: 1 and succeeded,
// and spawn with 5000 such requests 606 MiB (review round 10). Both now
// charge it, so they stop near the limit. spawn canonicalises a request
// repeated as given only once, so 300 copies of one cost one walk; 50
// distinct ones (p, p/., p/./., ...) each cost their own.
func TestDeepPathCanonicalisingIsCharged(t *testing.T) {
	path, dir := deepFile(t)
	caps := grant(t, runtime.FSRead, dir)
	distinct := fmt.Sprintf(`let mk = fn(acc, s, n) { if (n == 0) { acc } else { mk(push(acc, fs_read(s)), s + "/.", n - 1) } }; `+
		`let cs = mk([], %q, 50); let g = fn() { 1 }; scope (alloc_mb: 1) { spawn g() with cs; "ok" }`, filepath.Dir(path))
	for _, tt := range []struct{ name, src, at string }{
		{"read_file", fmt.Sprintf(`let p = %q; scope (alloc_mb: 1) { for i in %s { read_file(p) }; "ok" }`, path, zeros(1000)), "read_file"},
		{"write_file", fmt.Sprintf(`let p = %q; scope (alloc_mb: 1) { for i in %s { write_file(p, "x") }; "ok" }`, path, zeros(1000)), "write_file"},
		{"spawn repeated", fmt.Sprintf(`let c = fs_read(%q); let cs = for i in %s { c }; let g = fn() { 1 }; scope (alloc_mb: 1) { spawn g() with cs; "ok" }`, filepath.Dir(path), zeros(300)), ""},
		{"spawn distinct", distinct, "spawn"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			before := goruntime.NumGoroutine()
			c := caps
			if tt.name == "write_file" {
				c = grant(t, runtime.FSWrite, dir)
			}
			var got value.Object
			used := heapDuring(func() { got, _ = runWith(t, tt.src, c) })
			if tt.at == "" {
				if got.Inspect() != "ok" {
					t.Fatalf("got %.300s, want ok", got.Inspect())
				}
			} else {
				wantBareAllocError(t, got, colOf(t, tt.src, tt.at))
			}
			settled(t, before)
			if used > 24<<20 {
				t.Errorf("allocated %d MiB under alloc_mb: 1, want under 24 MiB", used>>20)
			}
		})
	}
}
