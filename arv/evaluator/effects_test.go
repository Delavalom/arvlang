package evaluator

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

func grant(t *testing.T, k runtime.Kind, scope string) runtime.Capability {
	t.Helper()
	c, err := runtime.New(k, scope)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func wantDenied(t *testing.T, got value.Object, fragment string) {
	t.Helper()
	e, ok := got.(*value.Error)
	if !ok || !strings.Contains(e.Message, "capability denied") || !strings.Contains(e.Message, fragment) {
		t.Fatalf("got %v, want capability denied mentioning %q", got, fragment)
	}
}

func TestHTTPGetNeedsNetCapability(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "pong")
	}))
	defer srv.Close()
	src := fmt.Sprintf(`let r = http_get(%q); [r["status"], r["body"]]`, srv.URL)

	got, _ := runWith(t, src)
	wantDenied(t, got, `net("127.0.0.1")`)

	got, _ = runWith(t, src, grant(t, runtime.Net, "127.0.0.1"))
	if got.Inspect() != "[200, pong]" {
		t.Fatalf("got %s", got.Inspect())
	}
}

func TestReadFileIsConfinedToItsGrant(t *testing.T) {
	dir := t.TempDir()
	allowed := filepath.Join(dir, "data")
	if err := os.Mkdir(allowed, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(allowed, "a.txt"), []byte("hello"), 0o644)
	os.WriteFile(filepath.Join(dir, "secret.txt"), []byte("nope"), 0o644)
	// A symlink inside the grant that points outside it must not work.
	os.Symlink(filepath.Join(dir, "secret.txt"), filepath.Join(allowed, "link.txt"))
	caps := []runtime.Capability{grant(t, runtime.FSRead, allowed)}

	got, _ := runWith(t, fmt.Sprintf(`read_file(%q)`, filepath.Join(allowed, "a.txt")), caps...)
	if got.Inspect() != "hello" {
		t.Fatalf("read inside grant = %s", got.Inspect())
	}
	got, _ = runWith(t, fmt.Sprintf(`read_file(%q)`, filepath.Join(allowed, "..", "secret.txt")), caps...)
	wantDenied(t, got, "secret.txt")
	// The denial names the link as written, not where it points.
	got, _ = runWith(t, fmt.Sprintf(`read_file(%q)`, filepath.Join(allowed, "link.txt")), caps...)
	wantDenied(t, got, "link.txt")
	if strings.Contains(got.Inspect(), "secret.txt") {
		t.Fatalf("denial reveals the link's target: %s", got.Inspect())
	}
}

func TestWriteFileNeedsWriteNotRead(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "out.txt")
	src := fmt.Sprintf(`write_file(%q, "x")`, target)

	got, _ := runWith(t, src, grant(t, runtime.FSRead, dir))
	wantDenied(t, got, "fs_write")

	runWith(t, src, grant(t, runtime.FSWrite, dir))
	if data, _ := os.ReadFile(target); string(data) != "x" {
		t.Fatalf("file = %q", data)
	}
}

func TestGetenvIsPerVariable(t *testing.T) {
	t.Setenv("ARV_OK", "yes")
	t.Setenv("ARV_SECRET", "no")
	caps := []runtime.Capability{grant(t, runtime.Env, "ARV_OK")}

	got, _ := runWith(t, `getenv("ARV_OK")`, caps...)
	if got.Inspect() != "yes" {
		t.Fatalf("got %s", got.Inspect())
	}
	got, _ = runWith(t, `getenv("ARV_SECRET")`, caps...)
	wantDenied(t, got, "ARV_SECRET")
}

func TestCapabilityValues(t *testing.T) {
	got, _ := runWith(t, `[net("API.github.com"), env("HOME")]`)
	if got.Inspect() != `[net("api.github.com"), env("HOME")]` {
		t.Fatalf("got %s", got.Inspect())
	}
	got, _ = runWith(t, `net("")`)
	if _, ok := got.(*value.Error); !ok {
		t.Fatalf("empty scope should be an error, got %v", got)
	}
}

func TestHTTPGetChecksRedirectTargets(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "should not be reached")
	}))
	defer target.Close()
	// Same server, but addressed as "localhost", a host the task does not hold.
	elsewhere := strings.Replace(target.URL, "127.0.0.1", "localhost", 1)
	redirector := httptest.NewServer(http.RedirectHandler(elsewhere, http.StatusFound))
	defer redirector.Close()

	got, _ := runWith(t, fmt.Sprintf(`http_get(%q)`, redirector.URL), grant(t, runtime.Net, "127.0.0.1"))
	wantDenied(t, got, `net("localhost")`)
}

// Defence in depth: even if a path that passed the check is swapped for a
// symlink before the open, the write must not follow it.
func TestWriteNoFollowRefusesFinalSymlink(t *testing.T) {
	dir := t.TempDir()
	victim := filepath.Join(dir, "victim.txt")
	if err := os.WriteFile(victim, []byte("untouched"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(victim, link); err != nil {
		t.Fatal(err)
	}
	dangling := filepath.Join(dir, "dangling")
	if err := os.Symlink(filepath.Join(dir, "created.txt"), dangling); err != nil {
		t.Fatal(err)
	}

	if err := writeNoFollow(link, []byte("pwned")); err == nil {
		t.Error("writeNoFollow through a symlink succeeded")
	}
	if data, _ := os.ReadFile(victim); string(data) != "untouched" {
		t.Errorf("victim = %q, want untouched (no follow, no truncate)", data)
	}
	if err := writeNoFollow(dangling, []byte("pwned")); err == nil {
		t.Error("writeNoFollow through a dangling symlink succeeded")
	}
	if _, err := os.Lstat(filepath.Join(dir, "created.txt")); err == nil {
		t.Error("writeNoFollow created the dangling link's target")
	}

	// Normal files: create, then truncate and overwrite.
	plain := filepath.Join(dir, "plain.txt")
	for _, text := range []string{"a longer first version", "short"} {
		if err := writeNoFollow(plain, []byte(text)); err != nil {
			t.Fatal(err)
		}
		if data, _ := os.ReadFile(plain); string(data) != text {
			t.Fatalf("plain = %q, want %q", data, text)
		}
	}
}

// chdir switches the process cwd for one (non-parallel) test.
func chdir(t *testing.T, dir string) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(old) })
}

// One ".." rule: a relative request keeps its ".." for Canonical, which
// applies it after following symlinks, as the OS does. g/l -> else/dir, so
// g/l/../x is else/x (what cat reads), never g/x.
func TestReadFileDotDotAfterSymlink(t *testing.T) {
	dir := t.TempDir()
	for _, d := range []string{"g", "else/dir"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	os.WriteFile(filepath.Join(dir, "g", "x"), []byte("WRONG g/x"), 0o644)
	os.WriteFile(filepath.Join(dir, "else", "x"), []byte("right else/x"), 0o644)
	if err := os.Symlink(filepath.Join(dir, "else", "dir"), filepath.Join(dir, "g", "l")); err != nil {
		t.Fatal(err)
	}
	chdir(t, dir)
	g, elsewhere := grant(t, runtime.FSRead, filepath.Join(dir, "g")), grant(t, runtime.FSRead, filepath.Join(dir, "else"))

	for _, path := range []string{"g/l/../x", dir + "/g/l/../x"} {
		got, _ := runWith(t, fmt.Sprintf(`read_file(%q)`, path), g)
		wantDenied(t, got, "g/l/../x") // as written: the target (else/x) is not revealed
		got, _ = runWith(t, fmt.Sprintf(`read_file(%q)`, path), elsewhere)
		if got.Inspect() != "right else/x" {
			t.Fatalf("read_file(%q) under fs_read(else) = %s, want else/x", path, got.Inspect())
		}
	}
	// Capability values hold the path as written (I1: building one touches
	// no file), and spawn canonicalises them by the same rule as requests:
	// g/l/.. is else, so a child may get it from fs_read(else), not fs_read(g).
	spawnX := `await spawn read_file("else/x") with [fs_read("g/l/..")]`
	got, _ := runWith(t, spawnX, elsewhere)
	if got.Inspect() != "right else/x" {
		t.Fatalf(`spawn with fs_read("g/l/..") under fs_read(else) = %s, want else/x`, got.Inspect())
	}
	got, _ = runWith(t, spawnX, g)
	wantDenied(t, got, "g/l/..")
}

// An empty path must not mean the working directory.
func TestEmptyPathIsAnError(t *testing.T) {
	chdir(t, t.TempDir())
	for _, src := range []string{`fs_read("")`, `fs_write("")`, `read_file("")`, `write_file("", "x")`} {
		got, _ := runWith(t, src, grant(t, runtime.FSRead, "/"), grant(t, runtime.FSWrite, "/"))
		if e, ok := got.(*value.Error); !ok || !strings.Contains(e.Message, "empty") {
			t.Errorf("%s = %v, want an empty-path error", src, got)
		}
	}
}

func wantErrorHas(t *testing.T, got value.Object, fragment string) {
	t.Helper()
	if e, ok := got.(*value.Error); !ok || !strings.Contains(e.Message, fragment) {
		s := got.Inspect()
		if len(s) > 200 {
			s = s[:200] + "..."
		}
		t.Fatalf("got %s %s, want an error mentioning %q", got.Type(), s, fragment)
	}
}

// Over-limit bodies are an error, not silently truncated. Exactly maxBody is fine.
func TestReadFileSizeLimit(t *testing.T) {
	dir := t.TempDir()
	for name, size := range map[string]int64{"exact": maxBody, "over": maxBody + 1} {
		f, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := f.Truncate(size); err != nil { // sparse: no disk used
			t.Fatal(err)
		}
		f.Close()
	}
	caps := grant(t, runtime.FSRead, dir)
	got, _ := runWith(t, fmt.Sprintf(`len(read_file(%q))`, filepath.Join(dir, "exact")), caps)
	if got.Inspect() != fmt.Sprint(maxBody) {
		t.Fatalf("exactly maxBody: got %s", got.Inspect())
	}
	got, _ = runWith(t, fmt.Sprintf(`read_file(%q)`, filepath.Join(dir, "over")), caps)
	wantErrorHas(t, got, "read_file: body exceeds 10 MiB limit")
}

func TestHTTPGetSizeLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := maxBody
		if r.URL.Path == "/over" {
			n++
		}
		w.Write([]byte(strings.Repeat("a", n)))
	}))
	defer srv.Close()
	caps := grant(t, runtime.Net, "127.0.0.1")
	got, _ := runWith(t, fmt.Sprintf(`len(http_get(%q)["body"])`, srv.URL+"/exact"), caps)
	if got.Inspect() != fmt.Sprint(maxBody) {
		t.Fatalf("exactly maxBody: got %s", got.Inspect())
	}
	got, _ = runWith(t, fmt.Sprintf(`http_get(%q)`, srv.URL+"/over"), caps)
	wantErrorHas(t, got, "http_get: body exceeds 10 MiB limit")
}

func TestHTTPGetURLWithoutHost(t *testing.T) {
	got, _ := runWith(t, `http_get("http:///nohost")`, grant(t, runtime.Net, "*"))
	wantErrorHas(t, got, "http_get: URL has no host")
}
