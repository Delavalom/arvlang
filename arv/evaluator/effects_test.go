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
	got, _ = runWith(t, fmt.Sprintf(`read_file(%q)`, filepath.Join(allowed, "link.txt")), caps...)
	wantDenied(t, got, "secret.txt")
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
