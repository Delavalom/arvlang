package check

import (
	"errors"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/delavalom/arvlang/arv"
	"github.com/delavalom/arvlang/arv/runtime"
)

// soundLimits bound a fuzzed run: it may loop, sleep or grow.
var soundLimits = runtime.Limits{Timeout: 2 * time.Second, Steps: 200000, AllocBytes: 8 << 20, LiveTasks: 20}

// confined reports whether granting n is safe for a fuzzer to do on a
// shared machine: a relative path that stays inside the working directory,
// the local server by IP literal, or an environment variable (reading one
// prints nothing: the run's output is discarded).
func confined(n Need, port string) bool {
	switch n.Kind {
	case runtime.FSRead, runtime.FSWrite:
		if n.Scope == "" || filepath.IsAbs(n.Scope) || strings.ContainsRune(n.Scope, 0) {
			return false
		}
		for _, part := range strings.Split(filepath.ToSlash(n.Scope), "/") {
			if part == ".." {
				return false
			}
		}
		return true
	case runtime.Net:
		return n.Scope == "127.0.0.1:"+port || n.Scope == "0.0.0.0:"+port
	case runtime.Env:
		return true
	}
	return false
}

// soundFixture makes a fresh working directory with the files the
// generated programs read.
func soundFixture(t *testing.T) string {
	dir := t.TempDir()
	t.Chdir(dir)
	for n := 0; n < 4; n++ {
		if err := os.WriteFile(fmt.Sprintf("r%d.txt", n), []byte("data"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// assertSound runs src with exactly the grants arv check reports, when
// that is safe to do and arv check claims to know every need (no note but
// "never allowed" ones), and fails if the runtime asks for a grant outside
// them (the request itself; the program may hand a task less than it
// uses): soundness on arbitrary programs. It returns whether src ran.
func assertSound(t *testing.T, src, port, dir string) bool {
	r, err := Source(src)
	if err != nil {
		return false
	}
	for _, n := range r.Notes {
		if !strings.HasPrefix(n.Msg, "never allowed") {
			return false
		}
	}
	entries := make([]Entry, 0, len(r.Needs))
	for _, n := range r.Needs {
		if !confined(n, port) {
			return false
		}
		entries = append(entries, Entry{Text: n.String()})
	}
	caps, err := Grants(entries, dir)
	if err != nil {
		return false // a grant --allow refuses: the run would not start (16p81c)
	}
	before := goruntime.NumGoroutine()
	_, err = arv.Run(src, arv.Options{Caps: caps, Limits: soundLimits})
	var ae *arv.Error
	if errors.As(err, &ae) && ae.Kind == runtime.KindCapability {
		// The program may deny itself, by spawning a task with less
		// than it uses (I2). Soundness asks only that what it requested
		// is covered by the reported grants.
		want, ok := deniedWant(ae.Msg)
		if !ok {
			t.Fatalf("cannot read the denied request from %q\nprogram: %q", ae.Msg, src)
		}
		if !coveredBy(want, caps) {
			t.Fatalf("arv check reported %v (notes %v), but the run requested %s: %v\nprogram: %q", r.Needs, r.Notes, want, err, src)
		}
	}
	if n := settledGoroutines(before); n > before {
		t.Fatalf("goroutines: %d before the run, %d after: %q", before, n, src)
	}
	return true
}

// deniedWant reads the requested capability out of a denial's message,
// "capability denied: kind("scope") ..." (runtime.DeniedError).
func deniedWant(msg string) (runtime.Capability, bool) {
	_, rest, ok := strings.Cut(msg, "capability denied: ")
	kind, rest, ok2 := strings.Cut(rest, "(")
	if !ok || !ok2 {
		return runtime.Capability{}, false
	}
	q, err := strconv.QuotedPrefix(rest)
	if err != nil {
		return runtime.Capability{}, false
	}
	scope, err := strconv.Unquote(q)
	return runtime.Capability{Kind: runtime.Kind(kind), Scope: scope}, err == nil
}

// coveredBy reports whether a grant in caps covers want, compared on
// canonical scopes, as admission compares them (a denial may name the
// path as requested, through a symlink such as /var -> /private/var).
func coveredBy(want runtime.Capability, caps []runtime.Capability) bool {
	if canon, err := runtime.New(want.Kind, want.Scope); err == nil {
		want = canon
	}
	for _, c := range caps {
		if c.Covers(want) {
			return true
		}
	}
	return false
}

// localServer serves "ok" on 127.0.0.1 and returns its port.
func localServer(tb testing.TB) string {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) }))
	tb.Cleanup(srv.Close)
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
	return port
}

// FuzzCheckSound: for any program whose needs arv check claims to know,
// running it with exactly those grants is never denied. "{{P}}" in the
// input stands for a local server's port. The seeds are programs from the
// soundness generator (sound_test.go), which reach every syntax form that
// carries authority: spawn ... with, scope options, supervise, match.
func FuzzCheckSound(f *testing.F) {
	port := localServer(f)
	r := rand.New(rand.NewSource(8181))
	for i := 0; i < 24; i++ {
		g := &gen{r: r, port: "{{P}}", needs: map[string]bool{}}
		body, _ := g.expr(1 + r.Intn(3))
		f.Add(g.prelude() + body + ";")
	}
	f.Add(`let f = fn() { write_file("w1.txt", read_file("r1.txt")) }; await spawn f() with [fs_read("r1.txt"), fs_write("w1.txt")];`)
	f.Add(`if (false) { http_get("http://127.0.0.1:{{P}}/") } else { getenv("ARV_P81_X") };`)
	f.Add(`read_file(""); http_get("ftp://x/"); getenv("A");`)
	f.Fuzz(func(t *testing.T, src string) {
		src = strings.ReplaceAll(src, "{{P}}", port)
		assertSound(t, src, port, soundFixture(t))
	})
}

// The fuzz harness is not vacuous: the generator's programs pass its
// filter and run, and a program arv check under-reports would fail it.
func TestAssertSoundRunsGeneratedPrograms(t *testing.T) {
	port := localServer(t)
	r := rand.New(rand.NewSource(18))
	for i := 0; i < 20; i++ {
		g := &gen{r: r, port: port, needs: map[string]bool{}}
		body, _ := g.expr(1 + r.Intn(3))
		if src := g.prelude() + body + ";"; !assertSound(t, src, port, soundFixture(t)) {
			t.Fatalf("the harness skipped a generated program: %s", src)
		}
	}
	if assertSound(t, `let f = fn(u) { http_get(u) }; f("http://127.0.0.1:`+port+`/");`, port, soundFixture(t)) {
		t.Fatal("a program with a note ran")
	}
	if assertSound(t, `read_file("/etc/hosts");`, port, soundFixture(t)) {
		t.Fatal("an absolute path was granted")
	}
}

// The oracle reads a real denial and tells a covered request from one
// outside the grants.
func TestDeniedWantAndCoveredBy(t *testing.T) {
	caps, err := Grants([]Entry{{Text: "net=127.0.0.1:80"}, {Text: "env=A"}}, "/")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		src     string
		covered bool
	}{
		{`let f = fn() { getenv("A") }; await spawn f();`, true}, // the child holds nothing (I2)
		{`getenv("B");`, false},
		{`http_get("http://localhost:80/");`, false},
	} {
		_, err := arv.Run(tc.src, arv.Options{Caps: caps})
		var ae *arv.Error
		if !errors.As(err, &ae) || ae.Kind != runtime.KindCapability {
			t.Fatalf("%s: %v, want a denial", tc.src, err)
		}
		want, ok := deniedWant(ae.Msg)
		if !ok || coveredBy(want, caps) != tc.covered {
			t.Fatalf("%s: %q read as %v (%v), covered %v, want %v", tc.src, ae.Msg, want, ok, coveredBy(want, caps), tc.covered)
		}
	}
}
