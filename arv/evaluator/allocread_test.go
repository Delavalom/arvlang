package evaluator

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

const allocErr = "budget exceeded: alloc (limit 1048576 bytes)"

// heapDuring is how many bytes f allocated on the heap, taken from the
// process's total: the evaluator tests run one at a time.
func heapDuring(f func()) uint64 {
	var before, after goruntime.MemStats
	goruntime.GC()
	goruntime.ReadMemStats(&before)
	f()
	goruntime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// wantBareAllocError checks got is the alloc_mb error itself, not one
// wrapped by the builtin, and that it has a position.
func wantBareAllocError(t *testing.T, got value.Object) {
	t.Helper()
	if e, ok := got.(*value.Error); !ok || e.Message != allocErr || e.Pos == "" {
		t.Fatalf("got %s, want %q with a position", got.Inspect(), allocErr)
	}
}

// bigFile makes a sparse file of maxBody bytes, the most an effect reads.
func bigFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "big")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := f.Truncate(maxBody); err != nil {
		t.Fatal(err)
	}
	return path
}

// A reading builtin charges alloc_mb as it reads, so it stops near the
// limit instead of reading its whole cap (10 MiB) and copying it before
// the result is charged. Reading a 10 MiB file used to allocate about
// 40 MiB under alloc_mb: 1.
func TestReadFileStopsAtAllocLimit(t *testing.T) {
	path := bigFile(t)
	caps := grant(t, runtime.FSRead, filepath.Dir(path))
	var got value.Object
	used := heapDuring(func() {
		got, _ = runWith(t, fmt.Sprintf(`scope (alloc_mb: 1) { len(read_file(%q)) }`, path), caps)
	})
	wantBareAllocError(t, got)
	if used > 8<<20 {
		t.Errorf("read_file under alloc_mb: 1 allocated %d bytes, want under 8 MiB", used)
	}
}

// Concurrent readers share the scope's pool, so together they stop near
// the limit too. 20 tasks reading 10 MiB each used to allocate about
// 800 MiB under alloc_mb: 1 before the first charge failed.
func TestConcurrentReadsStopAtAllocLimit(t *testing.T) {
	path := bigFile(t)
	caps := grant(t, runtime.FSRead, filepath.Dir(path))
	before := goruntime.NumGoroutine()
	var got value.Object
	used := heapDuring(func() {
		got, _ = runWith(t, fmt.Sprintf(`scope (alloc_mb: 1) { for i in %s { spawn read_file(%q) with [fs_read(%q)] } }`, zeros(20), path, filepath.Dir(path)), caps)
	})
	wantBareAllocError(t, got)
	settled(t, before)
	if used > 32<<20 {
		t.Errorf("20 concurrent read_file calls under alloc_mb: 1 allocated %d bytes, want under 32 MiB", used)
	}
}

func TestHTTPGetStopsAtAllocLimit(t *testing.T) {
	body := []byte(strings.Repeat("a", maxBody))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(body)
	}))
	defer srv.Close()
	caps := grant(t, runtime.Net, "127.0.0.1")
	var got value.Object
	used := heapDuring(func() {
		got, _ = runWith(t, fmt.Sprintf(`scope (alloc_mb: 1) { len(http_get(%q)["body"]) }`, srv.URL), caps)
	})
	wantBareAllocError(t, got)
	if used > 8<<20 {
		t.Errorf("http_get under alloc_mb: 1 allocated %d bytes, want under 8 MiB", used)
	}
}

// Charging while reading does not change what a result costs: a file's
// bytes, and a response's body plus its record's two pairs (see
// TestAllocBuiltinRecord), each charged once.
func TestReadingBuiltinsCostTheirResult(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(path, []byte(strings.Repeat("a", 100000)), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(strings.Repeat("a", 100000)))
	}))
	defer srv.Close()
	caps := runtime.NewCapSet(grant(t, runtime.FSRead, filepath.Dir(path)), grant(t, runtime.Net, "127.0.0.1"))
	for _, tt := range []struct {
		input string
		bytes int64
	}{
		{fmt.Sprintf(`len(read_file(%q))`, path), 100000},
		{fmt.Sprintf(`len(http_get(%q)["body"])`, srv.URL), 100000 + 2*hashPairSize},
	} {
		if got := runCapsIn(t, tt.bytes, caps, tt.input); got.Inspect() != "100000" {
			t.Errorf("%s under %d bytes: got %s", tt.input, tt.bytes, got.Inspect())
		}
		got := runCapsIn(t, tt.bytes-1, caps, tt.input)
		want := fmt.Sprintf("budget exceeded: alloc (limit %d bytes)", tt.bytes-1)
		if e, ok := got.(*value.Error); !ok || e.Message != want {
			t.Errorf("%s under %d bytes: got %s, want %q", tt.input, tt.bytes-1, got.Inspect(), want)
		}
	}
}

// runCapsIn evaluates input as the root task holding caps, under a root
// scope whose alloc budget is limit bytes.
func runCapsIn(t *testing.T, limit int64, caps runtime.CapSet, input string) value.Object {
	t.Helper()
	root := runtime.NewRoot(context.Background(), caps, io.Discard)
	in := New(root)
	b := runtime.Root(runtime.Limits{AllocBytes: limit})
	return in.withScope(frame{runtime.NewScope(root.Ctx), b}, func() value.Object {
		return in.Eval(parse(t, input), value.NewEnvironment())
	})
}
