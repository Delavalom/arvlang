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
// wrapped by the builtin, at pos: the call or operator that ran out.
func wantBareAllocError(t *testing.T, got value.Object, pos string) {
	t.Helper()
	wantErrorAt(t, got, allocErr, pos)
}

// colOf is the position ("1:col") of the first match of what in the
// one-line program src.
func colOf(t *testing.T, src, what string) string {
	t.Helper()
	i := strings.Index(src, what)
	if i < 0 {
		t.Fatalf("%q not in the program", what)
	}
	return fmt.Sprintf("1:%d", i+1)
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
	before := goruntime.NumGoroutine()
	src := fmt.Sprintf(`scope (alloc_mb: 1) { len(read_file(%q)) }`, path)
	var got value.Object
	used := heapDuring(func() { got, _ = runWith(t, src, caps) })
	wantBareAllocError(t, got, colOf(t, src, "read_file"))
	settled(t, before)
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
	src := fmt.Sprintf(`scope (alloc_mb: 1) { for i in %s { spawn read_file(%q) with [fs_read(%q)] } }`, zeros(20), path, filepath.Dir(path))
	var got value.Object
	used := heapDuring(func() { got, _ = runWith(t, src, caps) })
	// A task spawned straight onto a builtin call reports at its spawn.
	// The parent shares the pool, so under load it can hit the limit
	// first, binding the next iteration ("for") or building its request
	// list ("[fs_read", "fs_read(").
	e, ok := got.(*value.Error)
	if !ok || e.Message != allocErr {
		t.Fatalf("got %.300s, want %q", got.Inspect(), allocErr)
	}
	at := map[string]bool{}
	for _, w := range []string{"spawn read_file", "for i", "[fs_read", "fs_read("} {
		at[colOf(t, src, w)] = true
	}
	if !at[e.Pos] {
		t.Fatalf("error at %s, want at the loop, the spawn or its request list", e.Pos)
	}
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
	before := goruntime.NumGoroutine()
	src := fmt.Sprintf(`scope (alloc_mb: 1) { len(http_get(%q)["body"]) }`, srv.URL)
	var got value.Object
	used := heapDuring(func() { got, _ = runWith(t, src, caps) })
	wantBareAllocError(t, got, colOf(t, src, "http_get"))
	settled(t, before)
	if used > 8<<20 {
		t.Errorf("http_get under alloc_mb: 1 allocated %d bytes, want under 8 MiB", used)
	}
}

// Charging while reading does not change what a result costs: a file's
// bytes, and a response's body plus its record's two pairs (see
// TestAllocBuiltinRecord), each charged once. read_file also costs
// canonicalising its path (canonCost).
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
		{fmt.Sprintf(`len(read_file(%q))`, path), 100000 + canonCost(t, path)},
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

// Response headers are read in full inside client.Do, before anything is
// charged, so their size is capped (maxHeaderBytes). With Go's default
// (10 MiB), 10 tasks fetching a 9 MiB header each allocated hundreds of
// MiB under alloc_mb: 1, and every call succeeded.
func TestHTTPGetBigHeadersStayBounded(t *testing.T) {
	big := strings.Repeat("a", 9<<20)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Big", big)
		w.Write([]byte("x"))
	}))
	defer srv.Close()
	caps := grant(t, runtime.Net, "127.0.0.1")
	before := goruntime.NumGoroutine()
	var got value.Object
	used := heapDuring(func() {
		got, _ = runWith(t, fmt.Sprintf(`scope (alloc_mb: 1) { let hs = for i in %s { spawn http_get(%q) with [net("127.0.0.1")] }; len(await hs) }`, zeros(10), srv.URL), caps)
	})
	srv.CloseClientConnections()
	if e, ok := got.(*value.Error); !ok || !strings.Contains(e.Message, "header") {
		t.Errorf("got %s, want an http_get error about the headers", got.Inspect())
	}
	settled(t, before)
	if used > 32<<20 {
		t.Errorf("10 http_get calls with 9 MiB headers allocated %d MiB, want under 32 MiB", used>>20)
	}
}

// Headers under the cap are still accepted.
func TestHTTPGetAcceptsHeadersUnderCap(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Big", strings.Repeat("a", maxHeaderBytes/2))
		w.Write([]byte("x"))
	}))
	defer srv.Close()
	got, _ := runWith(t, fmt.Sprintf(`http_get(%q)["body"]`, srv.URL), grant(t, runtime.Net, "127.0.0.1"))
	if got.Inspect() != "x" {
		t.Errorf("got %s, want x", got.Inspect())
	}
}

// canonCost is what canonicalising path charges (runtime.CanonicalCharged).
func canonCost(t *testing.T, path string) int64 {
	t.Helper()
	var n int64
	if _, err := runtime.CanonicalCharged(path, func(c int64) error { n += c; return nil }); err != nil {
		t.Fatal(err)
	}
	return n
}
