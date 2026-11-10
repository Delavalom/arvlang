# Phase 1c: Capability values and the first effects

**Goal:** Arv code can name capabilities (`net("api.github.com")`) and call four effects. Each effect checks the calling task's grants on a canonical request: `http_get` (re-checked on every redirect), `read_file`, `write_file` and `getenv`.

**Invariants touched:** I1, I3.

---

### Task 1.4: `CAPABILITY` values and a hash helper

**Files:**
- Modify: `arv/value/value.go`

There's no separate test: Task 1.5's `TestCapabilityValues` covers this through the language.

- [ ] **Step 1: Add the type constant.** In the `const (...)` block of `arv/value/value.go`, after `HASH_VAL`:

```go
	CAPABILITY_VAL   = "CAPABILITY"
```

- [ ] **Step 2: Append to `arv/value/value.go`**

```go
// Capability is an Arv value naming one grant, e.g. net("api.github.com").
// Holding the value grants nothing; spawn ... with [...] hands authority the
// parent already holds to a child, and effects check the task's set.
type Capability struct {
	Cap runtime.Capability
}

func (c *Capability) Type() ObjectType { return CAPABILITY_VAL }
func (c *Capability) Inspect() string  { return c.Cap.String() }

// NewHash builds a hash with string keys, for builtins returning records.
func NewHash(fields map[string]Object) *Hash {
	pairs := make(map[HashKey]HashPair, len(fields))
	for k, v := range fields {
		key := &String{Value: k}
		pairs[key.HashKey()] = HashPair{Key: key, Value: v}
	}
	return &Hash{Pairs: pairs}
}
```

- [ ] **Step 3: Build**

Run: `go build ./...`
Expected: no output.

(Commit together with Task 1.5.)

---

### Task 1.5: Effect builtins

**Files:**
- Create: `arv/evaluator/effects.go`
- Test: `arv/evaluator/effects_test.go`

Each test in this file is a security test, so read them as the spec:
- `TestHTTPGetNeedsNetCapability`: no grant means denied; a grant means a response (I1).
- `TestHTTPGetChecksRedirectTargets`: a granted host that redirects to another host gets denied. Go's default `http.Client` follows redirects anywhere; `clientFor` re-checks each hop (I3).
- `TestReadFileIsConfinedToItsGrant`: `..` escapes and symlinks pointing outside the grant are both denied (I3).
- `TestWriteFileNeedsWriteNotRead`: `fs_read` doesn't allow writing.
- `TestGetenvIsPerVariable`: `env("ARV_OK")` doesn't expose `ARV_SECRET`.

- [ ] **Step 1: Write the failing tests**

```go
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
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go test ./arv/evaluator/ -run 'HTTP|ReadFile|WriteFile|Getenv|CapabilityValues'`
Expected: FAIL. Every test reports `identifier not found: http_get` (or `net`, `read_file`, …).

- [ ] **Step 3: Implement**

```go
package evaluator

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// maxBody caps how much of an HTTP response or file an effect will read.
const maxBody = 10 << 20

// maxRedirects matches net/http's default limit.
const maxRedirects = 10

// clientFor returns an HTTP client that re-checks the task's net capability
// on every redirect, so a granted host cannot bounce the task elsewhere.
func clientFor(t *runtime.Task) *http.Client {
	return &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return errors.New("too many redirects")
			}
			want, err := runtime.New(runtime.Net, req.URL.Hostname())
			if err != nil {
				return err
			}
			return t.Require(want)
		},
	}
}

func init() {
	for name, kind := range map[string]runtime.Kind{
		"net": runtime.Net, "fs_read": runtime.FSRead, "fs_write": runtime.FSWrite, "env": runtime.Env,
	} {
		builtins[name] = &value.Builtin{Fn: capabilityConstructor(kind)}
	}
	builtins["http_get"] = &value.Builtin{Fn: httpGet}
	builtins["read_file"] = &value.Builtin{Fn: readFile}
	builtins["write_file"] = &value.Builtin{Fn: writeFile}
	builtins["getenv"] = &value.Builtin{Fn: getenv}
}

// capabilityConstructor builds net("host"), fs_read("path"), ... values.
// Relative paths resolve against the working directory.
func capabilityConstructor(kind runtime.Kind) value.BuiltinFunction {
	return func(t *runtime.Task, args ...value.Object) value.Object {
		scope, errObj := oneString(string(kind), args)
		if errObj != nil {
			return errObj
		}
		if kind == runtime.FSRead || kind == runtime.FSWrite {
			abs, err := filepath.Abs(scope)
			if err != nil {
				return newError("%s: %s", kind, err)
			}
			scope = abs
		}
		c, err := runtime.New(kind, scope)
		if err != nil {
			return newError("%s", err)
		}
		return &value.Capability{Cap: c}
	}
}

func httpGet(t *runtime.Task, args ...value.Object) value.Object {
	raw, errObj := oneString("http_get", args)
	if errObj != nil {
		return errObj
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return newError("http_get: invalid url %q", raw)
	}
	want, _ := runtime.New(runtime.Net, u.Hostname())
	if err := t.Require(want); err != nil {
		return newError("%s", err)
	}
	req, err := http.NewRequestWithContext(t.Ctx, http.MethodGet, raw, nil)
	if err != nil {
		return newError("http_get: %s", err)
	}
	resp, err := clientFor(t).Do(req)
	if err != nil {
		var denied *runtime.DeniedError
		if errors.As(err, &denied) {
			return newError("%s", denied)
		}
		return newError("http_get: %s", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return newError("http_get: reading body: %s", err)
	}
	return value.NewHash(map[string]value.Object{
		"status": &value.Integer{Value: int64(resp.StatusCode)},
		"body":   &value.String{Value: string(body)},
	})
}

func readFile(t *runtime.Task, args ...value.Object) value.Object {
	path, errObj := fsRequest(t, "read_file", runtime.FSRead, args)
	if errObj != nil {
		return errObj
	}
	f, err := os.Open(path)
	if err != nil {
		return newError("read_file: %s", err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxBody))
	if err != nil {
		return newError("read_file: %s", err)
	}
	return &value.String{Value: string(data)}
}

func writeFile(t *runtime.Task, args ...value.Object) value.Object {
	if len(args) != 2 {
		return newError("write_file: want 2 arguments (path, text), got %d", len(args))
	}
	text, ok := args[1].(*value.String)
	if !ok {
		return newError("write_file: text must be STRING, got %s", args[1].Type())
	}
	path, errObj := fsRequest(t, "write_file", runtime.FSWrite, args[:1])
	if errObj != nil {
		return errObj
	}
	if err := os.WriteFile(path, []byte(text.Value), 0o644); err != nil {
		return newError("write_file: %s", err)
	}
	return NIL
}

func getenv(t *runtime.Task, args ...value.Object) value.Object {
	name, errObj := oneString("getenv", args)
	if errObj != nil {
		return errObj
	}
	want, err := runtime.New(runtime.Env, name)
	if err != nil {
		return newError("getenv: %s", err)
	}
	if err := t.Require(want); err != nil {
		return newError("%s", err)
	}
	return &value.String{Value: os.Getenv(name)}
}

// fsRequest resolves the path argument and checks the task may touch it.
func fsRequest(t *runtime.Task, fn string, kind runtime.Kind, args []value.Object) (string, *value.Error) {
	raw, errObj := oneString(fn, args)
	if errObj != nil {
		return "", errObj
	}
	abs, err := filepath.Abs(raw)
	if err != nil {
		return "", newError("%s: %s", fn, err)
	}
	want, err := runtime.New(kind, abs) // canonicalises, resolving symlinks
	if err != nil {
		return "", newError("%s: %s", fn, err)
	}
	if err := t.Require(want); err != nil {
		return "", newError("%s", err)
	}
	return want.Scope, nil
}

func oneString(fn string, args []value.Object) (string, *value.Error) {
	if len(args) != 1 {
		return "", newError("%s: want 1 argument, got %d", fn, len(args))
	}
	s, ok := args[0].(*value.String)
	if !ok {
		return "", newError("%s: argument must be STRING, got %s", fn, args[0].Type())
	}
	return s.Value, nil
}
```

Notes:
- The effects register themselves in `init()` by adding to the existing `builtins` map. `builtins.go` stays the home of the pure builtins (`len`, `push`, `puts`, …).
- `fsRequest` returns the canonical path and the effect opens *that* path, so the path that was checked is the path that gets opened.
- Bodies are capped at 10 MiB. Time limits come from the task context (budgets arrive in Phase 3), plus a 30-second client timeout as a backstop.
- Error messages start with `capability denied:` and name the request, the task and what it holds. Phase 4's `match` will expose the kind as data.

- [ ] **Step 4: Run all tests with the race detector**

Run: `go vet ./... && go test -race ./...`
Expected: all `ok`.

- [ ] **Step 5: Commit**

```bash
git add arv/value arv/evaluator
git commit -m "feat: capability values and capability-checked effects

http_get re-checks net capability on every redirect; read_file and
write_file resolve symlinks before checking; getenv is per variable."
```
