# Phase 8.1, task p81.4: The least set, shell quoting, and agreement with the runtime

> **For agentic workers:** this file is one task, p81.4. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: task p81.3 is committed in your tree. Start every shell command with `cd <your worktree> &&` and use absolute paths: the shell's working directory does not persist. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify` and never push; keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0` (toolchain 1.26.2): `t.Chdir`, `min` and `max` are available; do not use `testing/synctest` or `sync.WaitGroup.Go`. Before a long test run, check `df -h /System/Volumes/Data`; below 1 GB free, stop and report (only `go clean -cache` may be deleted). This machine is heavily loaded: keep wall-clock bounds generous.
>
> Every code block below was applied, in task order, to a scratch copy of `feat/phases-3-8` (860a158) and passed `gofmt -l .`, `go vet ./...` and `go test -race ./...` (including `e2e/`). Design: `15-phase8-tooling.md` §8.1. Deviations from it, and why: `p81-plan-1.md`. Phases 5E, 6, 7 and 8.2 are built at the same time and also touch the CLI, parser and evaluator: change only the files this task names.

### Task p81.4: The least set, shell quoting, and agreement with the runtime

Two net or env needs where one covers the other print once: `net("api.github.com")` covers the `api.github.com:443` that `http_get("https://api.github.com/zen")` needs. The decision is `runtime.Capability.Covers`, the runtime's own check, which reads nothing outside the program for these kinds; the wildcard still does not cover a loopback IP. Paths are not merged by directory: whether `out/a` lies under `out` depends on symlinks, which `arv check` does not read, so only exact duplicates go.

`Need.Quoted` prints `kind=scope` bare when it holds only the characters grants usually have, and otherwise as `$'...'` with control characters escaped, so a scope can never print as two lines (a path with a newline cannot forge a second `--allow` line).

`TestNeedsMatchTheRuntimesRequests` is the differential test: for each call, running it with no grants is denied naming exactly the capability check reports (a path made absolute with `runtime.Resolve`, as the runtime does), and a call check says is never allowed fails some other way. No grant is held, so nothing touches the network or a file.

**Files:**
- Create: `arv/check/least.go`
- Modify: `arv/check/check.go` (two lines)
- Create (test): `arv/check/least_test.go`, `arv/check/runtime_test.go`

- [ ] **Step 1: Write the failing tests**

Create `arv/check/least_test.go`:

```go
package check

import (
	"reflect"
	"testing"
)

func TestLeastSetDropsCoveredNetAndEnvNeeds(t *testing.T) {
	needs, _ := checked(t, `
let t = spawn f() with [net("api.github.com"), net("*.example.com"), env("*")];
http_get("https://api.github.com/zen");
http_get("http://api.github.com:8080/");
http_get("https://www.example.com/");
http_get("http://127.0.0.1:9/");
getenv("HOME");`)
	want := []string{"net=*.example.com", "net=127.0.0.1:9", "net=api.github.com", "env=*"}
	if !reflect.DeepEqual(needs, want) {
		t.Errorf("needs = %q, want %q", needs, want)
	}
}

// The wildcard covers every host name but no internal address, as at run
// time, so the loopback request keeps its own grant.
func TestWildcardDoesNotCoverLoopback(t *testing.T) {
	needs, _ := checked(t, `spawn f() with [net("*")]; http_get("http://127.0.0.1/"); http_get("http://example.com/");`)
	want := []string{"net=*", "net=127.0.0.1:80"}
	if !reflect.DeepEqual(needs, want) {
		t.Errorf("needs = %q, want %q", needs, want)
	}
}

// Paths are compared as written: only exact duplicates go.
func TestPathsAreNotMergedBySubdirectory(t *testing.T) {
	needs, _ := checked(t, `spawn f() with [fs_write("out")]; write_file("out/a", ""); write_file("out/a", "");`)
	want := []string{"fs_write=out", "fs_write=out/a"}
	if !reflect.DeepEqual(needs, want) {
		t.Errorf("needs = %q, want %q", needs, want)
	}
}

func TestSitesKeepEveryCall(t *testing.T) {
	r, err := Source(`getenv("A"); getenv("A");`)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Sites) != 2 || len(r.Needs) != 1 || r.Sites[1].Col != 14 {
		t.Errorf("sites = %+v, needs = %+v", r.Sites, r.Needs)
	}
}

func TestQuoted(t *testing.T) {
	for _, tt := range []struct{ scope, want string }{
		{"api.github.com", "net=api.github.com"},
		{"[::1]:443", "net=[::1]:443"},
		{"my file", `$'net=my file'`},
		{"a'b", `$'net=a\'b'`},
		{"x\n--allow fs_read=/", `$'net=x\x0a--allow fs_read=/'`},
		{"*.example.com", `$'net=*.example.com'`},
	} {
		if got := (Need{Kind: "net", Scope: tt.scope}).Quoted(); got != tt.want {
			t.Errorf("Quoted(%q) = %s, want %s", tt.scope, got, tt.want)
		}
	}
}
```

Create `arv/check/runtime_test.go`:

```go
package check

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv"
	"github.com/delavalom/arvlang/arv/runtime"
)

// arv check and the runtime agree on each call: when check reports a need,
// running the call with no grants is denied naming exactly that
// capability (a path made absolute against the working directory, as the
// runtime does); when check says the call is never allowed, the runtime
// fails it some other way. No grant is held, so nothing reaches the
// network or a file.
func TestNeedsMatchTheRuntimesRequests(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	spawn := func(c string) string { return `let f = fn() { 1 }; await spawn f() with [` + c + `]` }
	for _, src := range []string{
		`http_get("https://API.GitHub.com./zen")`,
		`http_get("http://127.0.0.1:8080/")`,
		`http_get("http://[::1]/")`,
		`http_get("http://user@example.com/")`,
		`http_get("http://example.com:0/")`,
		`http_get("http://exa mple.com/")`,
		`http_get("http://a_b.example/")`,
		`http_get("ftp://example.com/")`,
		`http_get("http:///p")`,
		`http_get("http://10.0.0.1:99999/")`,
		`read_file("rel/x.txt")`,
		`read_file("/nonexistent-p81/../x")`,
		`read_file("")`,
		`write_file("out/f.json", "t")`,
		`getenv("HOME")`,
		`getenv("")`,
		spawn(`net("API.example.com")`),
		spawn(`net("*.example.com")`),
		spawn(`net("10.0.0.0/8")`),
		spawn(`net("h.example:443")`),
		spawn(`net("h.example:0")`),
		spawn(`net("*")`),
		spawn(`fs_read("rel")`),
		spawn(`fs_write("/tmp")`),
		spawn(`env("A")`),
	} {
		r, err := Source(src)
		if err != nil {
			t.Fatalf("%s: %v", src, err)
		}
		_, runErr := arv.Run(src, arv.Options{})
		var ae *arv.Error
		if !errors.As(runErr, &ae) {
			t.Errorf("%s: run = %v, want an Arv error", src, runErr)
			continue
		}
		switch {
		case len(r.Sites) == 1 && len(r.Notes) == 0:
			n := r.Sites[0]
			scope := n.Scope
			if n.Kind == runtime.FSRead || n.Kind == runtime.FSWrite {
				scope = runtime.Resolve(cwd, scope)
			}
			want := "capability denied: " + runtime.Capability{Kind: n.Kind, Scope: scope}.String()
			if ae.Kind != runtime.KindCapability || !strings.Contains(ae.Msg, want) {
				t.Errorf("%s: check needs %s, run failed with %s: %s", src, n, ae.Kind, ae.Msg)
			}
		case len(r.Sites) == 0 && len(r.Notes) == 1 && strings.Contains(r.Notes[0].Msg, "never allowed"):
			if ae.Kind == runtime.KindCapability {
				t.Errorf("%s: check says %s, run was denied: %s", src, r.Notes[0], ae.Msg)
			}
		default:
			t.Errorf("%s: report %+v, want one need or one never-allowed note", src, r)
		}
	}
}
```

- [ ] **Step 2: Run and watch it fail**

Run: `go test ./arv/check/`
Expected: build failure: `(Need{…}).Quoted undefined (type Need has no field or method Quoted)`. (`runtime_test.go` would pass on its own: it pins behaviour p81.1 and p81.3 already have, and stays as the regression test.)

- [ ] **Step 3: Implement**

Create `arv/check/least.go`:

```go
package check

import (
	"strings"

	"github.com/delavalom/arvlang/arv/runtime"
)

// least drops each net or env need that another need covers: net("x")
// covers the x:443 that http_get("https://x/") needs, so granting x is
// enough. It decides with runtime.Capability.Covers, the check the runtime
// itself makes, which for these kinds reads nothing outside the program.
// Paths are kept as written, and only exact duplicates are dropped (by
// distinct): whether one path lies under another depends on symlinks,
// and arv check does not look at the filesystem. If two needs cover each
// other, the first is kept.
func least(needs []Need) []Need {
	var out []Need
	for i, n := range needs {
		if !covered(needs, i) {
			out = append(out, n)
		}
	}
	return out
}

func covered(needs []Need, i int) bool {
	n := needs[i]
	if n.Kind != runtime.Net && n.Kind != runtime.Env {
		return false
	}
	nc := runtime.Capability{Kind: n.Kind, Scope: n.Scope}
	for j, m := range needs {
		if j == i || m.Kind != n.Kind {
			continue
		}
		mc := runtime.Capability{Kind: m.Kind, Scope: m.Scope}
		if mc.Covers(nc) && (j < i || !nc.Covers(mc)) {
			return true
		}
	}
	return false
}

// Quoted is the need in --allow form, quoted for a POSIX shell when it
// holds anything but the characters a grant usually has. Control
// characters are escaped ($'...'), so a scope can never print as more
// than one line.
func (n Need) Quoted() string { return quoteArg(n.String()) }

func quoteArg(s string) string {
	plain := s != ""
	for i := 0; i < len(s); i++ {
		if !safeByte(s[i]) {
			plain = false
			break
		}
	}
	if plain {
		return s
	}
	var b strings.Builder
	b.WriteString("$'")
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '\\' || c == '\'':
			b.WriteByte('\\')
			b.WriteByte(c)
		case c < 0x20 || c >= 0x7f:
			const hex = "0123456789abcdef"
			b.WriteString(`\x`)
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&15])
		default:
			b.WriteByte(c)
		}
	}
	b.WriteByte('\'')
	return b.String()
}

func safeByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
		strings.IndexByte("_-.,/:=@%+[]", c) >= 0
}
```

In `arv/check/check.go`, replace:

```go
	Needs []Need // one per distinct grant, sorted by kind, then scope
```

with:

```go
	Needs []Need // the least set of grants that covers Sites, sorted by kind, then scope
```

In `arv/check/check.go`, replace:

```go
	return &Report{Needs: distinct(a.sites), Sites: a.sites, Notes: a.notes}
```

with:

```go
	return &Report{Needs: least(distinct(a.sites)), Sites: a.sites, Notes: a.notes}
```

- [ ] **Step 4: Run and watch it pass**

Run: `go test -race ./arv/check/`
Expected: `ok`. Then `gofmt -l .` (prints nothing), `go vet ./...` and `go test -race ./...`.

- [ ] **Step 5: Commit**

```bash
git add arv/check
git commit -m "feat(check): print the least set, quoted, and match the runtime" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
