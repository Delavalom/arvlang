# Phase 8.1, task p81.1: `evaluator.BuiltinNames` and the classification of every builtin

> **For agentic workers:** this file is one task, p81.1. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: your tree is `feat/phases-3-8` at 860a158 (Phase 5 gate merged) with no Phase 8.1 work yet. Start every shell command with `cd <your worktree> &&` and use absolute paths: the shell's working directory does not persist. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify` and never push; keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0` (toolchain 1.26.2): `t.Chdir`, `min` and `max` are available; do not use `testing/synctest` or `sync.WaitGroup.Go`. Before a long test run, check `df -h /System/Volumes/Data`; below 1 GB free, stop and report (only `go clean -cache` may be deleted). This machine is heavily loaded: keep wall-clock bounds generous.
>
> Every code block below was applied, in task order, to a scratch copy of `feat/phases-3-8` (860a158) and passed `gofmt -l .`, `go vet ./...` and `go test -race ./...` (including `e2e/`). Design: `15-phase8-tooling.md` §8.1. Deviations from it, and why: `p81-plan-1.md`. Phases 5E, 6, 7 and 8.2 are built at the same time and also touch the CLI, parser and evaluator: change only the files this task names.

## Phase 8.1 overview

**Goal:** `arv check FILE` prints, without running FILE, the `--allow` set it can need, and says where it cannot tell. A `// arv:allow kind=scope` header makes the set explicit, and `arv run --manifest FILE` grants exactly that header and refuses to start if `arv check` finds a need outside it.

**Guarantees, and the tasks that test them:**
- `arv check` never evaluates the program, so it never performs an effect: no request, no file written, an infinite loop or a long `sleep` returns at once (p81.7, p81.10).
- The report is sound for what the program text requests: every grant the program can be admitted for (by an effect or at `spawn ... with`) is printed, or a `#` note names the call that hides it. Every occurrence of an effect or constructor name is classified, so a builtin passed around as a value is noted (p81.3); every builtin is classified, and a new one fails a test (p81.1); running random programs with exactly the printed grants meets no denial, and dropping any one meets one (p81.9).
- Check and runtime agree on each request's scope: host, port, normalisation, paths (p81.4, differential test).
- Every syntax form is searched, scope options, `supervise`, `match`, `for`, `spawn ... with` and hash keys included, even code that never runs; an unknown node fails the check, and a new node type or field in `arv/ast` fails a test (p81.2, p81.3).
- `--manifest` grants the header and nothing else; a need outside it stops the run before anything runs (p81.8, p81.10).
- Untrusted input: `arv check` never panics, its output is deterministic, and a grant always prints as one line (p81.4 quoting, p81.9 fuzzing).

**Invariants touched:** I1 (`--manifest` is another way for the host to grant; a header grants nothing without `--manifest`), I2 (constructors are reported because `spawn ... with` admits them against the parent), I3 (`Outside` compares through `CapSet.Admit`, canonical requests, as at run time). No new goroutines, so the leak rule is untouched; `arv/runtime` is not changed.

| Task | File | What |
|---|---|---|
| p81.1 | `p81-plan-1.md` | `evaluator.BuiltinNames`; `arv/check` classifies every builtin |
| p81.2 | `p81-plan-2.md` | `walk`: every node, explicit stack, unknown nodes fail; AST pinned |
| p81.3 | `p81-plan-3.md` | `Program`/`Source`: needs, sites, notes |
| p81.4 | `p81-plan-4.md` | the least set, shell quoting, differential test against the runtime |
| p81.5 | `p81-plan-5.md` | constant strings (`let` bound once, `+`) |
| p81.6 | `p81-plan-6.md` | `Manifest`, `Grants`, `Outside` |
| p81.7 | `p81-plan-7.md` | `arv check FILE` |
| p81.8 | `p81-plan-8.md` | `arv run --manifest FILE` |
| p81.9 | `p81-plan-9.md` | soundness property, fuzz target (2 min) |
| p81.10 | `p81-plan-10.md` | e2e: acceptance over `examples/`, no effects, `--manifest` |
| p81.11 | `p81-plan-11.md` | README, `16p81-check-notes.md` |

### Deviations from `15-phase8-tooling.md` §8.1, and why

1. **Acceptance amended.** "For every file in `examples/`, the printed set is exactly the flags its README line uses" holds for `fanout.arv` (`net=api.github.com`) and cannot hold for `uptime.arv`: its line grants `fs_write=out`, the directory, while the program text writes `out/report.json`, and its net host comes from `targets.txt` at run time. The least set is `fs_read=targets.txt` and `fs_write=out/report.json`, with notes for `http_get(url)` and `net(host)`. Changing the example's line would change the Phase 5 gate's flags, so the test (p81.10) pins each example's exact output instead, and requires an entry for any new example.
2. **"Literal" includes constant strings.** A name bound once in the whole program by `let` to a known string, and `+` of known strings, count as known (p81.5). It is sound (no scope can see another value) and makes the check useful on ordinary scripts. Bounded so it stays cheap on hostile input.
3. **Output format.** One `--allow kind=scope` line per grant, ready to paste, then one `# fn at line:col: why` line per call it cannot resolve (the design's `http_get at line 14: host not statically known`, with a column). A scope with unusual characters is printed as `$'...'`, so it is one shell word and one line.
4. **Paths as written, no directory merging.** `--allow` resolves relative paths against the working directory, as the program does, so the printed path names the same file. `out` and `out/report.json` both print: whether one lies under the other depends on symlinks, and `arv check` does not read the filesystem to print. Net and env needs merge by `Capability.Covers`, which is pure for them.
5. **Manifest details the design leaves open.** Only the header (comments and blank lines before the first code) is read; entries resolve against the working directory like `--allow`; `--manifest` cannot be combined with `--allow`; a note does not refuse the run (the runtime still confines it to the header); `arv check` on a file with a header also exits 1 for a need outside it. Comparing a need with the header uses `CapSet.Admit`, which reads filesystem metadata as `--allow` does; with no header, `arv check` reads only the file.
6. **Stated precision (known limits, in `16p81-check-notes.md`).** Redirect targets are chosen by the server and are not reported. A host name that resolves to an internal address also needs an IP grant at dial time, which only resolving (an effect) could tell. Dead code is reported. A shadowed builtin name is treated as the builtin (over-reports). Custom effects that a Go host registers (Phase 8.2) are invisible to the CLI's check.
7. **Code placement, to keep merges with 5E, 6, 7 and 8.2 small.** A new package `arv/check`, a new file `arv/evaluator/names.go`, and the CLI in new files `check_cli.go` and `manifest_cli.go`; `main.go` gets five small hunks. Tests that pin the builtin table and the AST make a later phase's new builtin, node or field fail loudly at merge instead of being missed.

### Task p81.1: `evaluator.BuiltinNames` and the classification of every builtin

`arv check` must know, for every builtin, whether calling it can be admitted for a grant. The evaluator gets one exported function, in a new file, listing its builtins. The new package `arv/check` classifies each: an effect checked at use (`http_get` needs `net(host:port)`, `read_file` `fs_read(path)`, `write_file` `fs_write(path)`, `getenv` `env(name)`), a capability constructor (`net`, `fs_read`, `fs_write`, `env`, whose values `spawn ... with` checks against the parent), or pure. A builtin that another phase adds later fails `TestEveryBuiltinIsClassified` until it is classified here, so the report can never silently miss it.

`urlScope` mirrors `http_get` (`arv/evaluator/httpget.go`, `netRequest`): same length cap, same scheme rule, the scheme's default port, normalised by `runtime.Literal`, which is pure for net scopes. `literalScope` validates as `runtime.Literal` does and keeps a path as written: the program and `--allow` both resolve a relative path against the working directory, so the printed grant names the same file. p81.4 checks both against the running interpreter.

**Files:**
- Create: `arv/evaluator/names.go`
- Create (test): `arv/evaluator/names_test.go`
- Create: `arv/check/builtins.go`
- Create (test): `arv/check/builtins_test.go`

- [ ] **Step 1: Write the failing tests**

Create `arv/evaluator/names_test.go`:

```go
package evaluator

import (
	"sort"
	"testing"
)

func TestBuiltinNamesListsEveryBuiltin(t *testing.T) {
	names := BuiltinNames()
	if len(names) != len(builtins) {
		t.Fatalf("BuiltinNames has %d names, the table %d", len(names), len(builtins))
	}
	if !sort.StringsAreSorted(names) {
		t.Errorf("BuiltinNames is not sorted: %v", names)
	}
	for _, want := range []string{"http_get", "read_file", "write_file", "getenv", "net", "len", "puts"} {
		i := sort.SearchStrings(names, want)
		if i == len(names) || names[i] != want {
			t.Errorf("BuiltinNames lacks %q", want)
		}
	}
}
```

Create `arv/check/builtins_test.go`:

```go
package check

import (
	"testing"

	"github.com/delavalom/arvlang/arv/evaluator"
	"github.com/delavalom/arvlang/arv/runtime"
)

// Every builtin is either an effect (or constructor) with a rule here, or
// pure. A builtin added by a later phase fails this test until arv check
// knows what it needs: an unclassified one would be a hole in the report.
func TestEveryBuiltinIsClassified(t *testing.T) {
	names := map[string]bool{}
	for _, name := range evaluator.BuiltinNames() {
		names[name] = true
		_, isEffect := effects[name]
		if isEffect == pure[name] {
			t.Errorf("builtin %q: add it to exactly one of effects and pure in arv/check/builtins.go", name)
		}
	}
	for name := range effects {
		if !names[name] {
			t.Errorf("effects names %q, which is not a builtin", name)
		}
	}
	for name := range pure {
		if !names[name] {
			t.Errorf("pure names %q, which is not a builtin", name)
		}
	}
}

func TestURLScope(t *testing.T) {
	for _, tt := range []struct{ url, want, err string }{
		{"https://api.github.com/zen", "api.github.com:443", ""},
		{"http://API.GitHub.com./x", "api.github.com:80", ""},
		{"http://127.0.0.1:8080/ok", "127.0.0.1:8080", ""},
		{"http://[::1]:9/", "[::1]:9", ""},
		{"ftp://example.com/", "", "invalid url"},
		{"http:///path", "", "URL has no host"},
		{"http://a b/", "", "invalid url"},
	} {
		got, err := urlScope("", tt.url)
		if tt.err != "" {
			if err == nil || err.Error() != tt.err {
				t.Errorf("urlScope(%q) = %q, %v; want error %q", tt.url, got, err, tt.err)
			}
			continue
		}
		if err != nil || got != tt.want {
			t.Errorf("urlScope(%q) = %q, %v; want %q", tt.url, got, err, tt.want)
		}
	}
}

func TestLiteralScopeKeepsPathsAsWritten(t *testing.T) {
	for _, tt := range []struct {
		kind      runtime.Kind
		arg, want string
		bad       bool
	}{
		{"fs_read", "targets.txt", "targets.txt", false},
		{"fs_write", "out/../x", "out/../x", false},
		{"fs_read", "/etc/hosts", "/etc/hosts", false},
		{"fs_read", "", "", true},
		{"net", "API.github.com", "api.github.com", false},
		{"net", "a b", "", true},
		{"env", "HOME", "HOME", false},
		{"env", "", "", true},
	} {
		got, err := literalScope(tt.kind, tt.arg)
		if (err != nil) != tt.bad || got != tt.want {
			t.Errorf("literalScope(%s, %q) = %q, %v", tt.kind, tt.arg, got, err)
		}
	}
}
```

- [ ] **Step 2: Run and watch it fail**

Run: `go test ./arv/evaluator/ ./arv/check/`
Expected: build failures: `undefined: evaluator.BuiltinNames` and `undefined: effects` in `arv/check`, and `undefined: BuiltinNames` in `arv/evaluator`.

- [ ] **Step 3: Implement**

Create `arv/evaluator/names.go`:

```go
package evaluator

import "sort"

// BuiltinNames returns the name of every builtin a program can call,
// sorted. arv check (package check) classifies each one by the grant it
// needs, and its tests fail when a builtin is added here without a
// classification there.
func BuiltinNames() []string {
	names := make([]string, 0, len(builtins))
	for name := range builtins {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
```

Create `arv/check/builtins.go`:

```go
// Package check finds, without running a program, the grants it can need:
// the --allow set that arv check prints. It reads the syntax tree only. It
// never evaluates the program, so it never performs an effect.
package check

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"path/filepath"

	"github.com/delavalom/arvlang/arv/runtime"
)

// effect describes a builtin that can be admitted for a grant: an effect
// (http_get, read_file, write_file, getenv), checked against the task's
// grants when it runs, or a capability constructor (net, fs_read,
// fs_write, env), whose value spawn ... with checks against the parent's.
type effect struct {
	kind  runtime.Kind
	nargs int    // the arguments a valid call takes; the first names the scope
	what  string // what that argument names, for a note: "host", "path", ...
	// scope turns the argument into the scope it needs, or fails as the
	// runtime would, before admitting anything.
	scope func(kind runtime.Kind, arg string) (string, error)
}

var effects = map[string]effect{
	"http_get":   {runtime.Net, 1, "host", urlScope},
	"read_file":  {runtime.FSRead, 1, "path", literalScope},
	"write_file": {runtime.FSWrite, 2, "path", literalScope},
	"getenv":     {runtime.Env, 1, "name", literalScope},
	"net":        {runtime.Net, 1, "scope", literalScope},
	"fs_read":    {runtime.FSRead, 1, "scope", literalScope},
	"fs_write":   {runtime.FSWrite, 1, "scope", literalScope},
	"env":        {runtime.Env, 1, "scope", literalScope},
}

// pure are the builtins that need no grant. sleep and clock_ms read the
// clock and puts writes to the run's output, all without a capability.
var pure = map[string]bool{
	"clock_ms": true, "error": true, "json": true, "last": true, "len": true,
	"lines": true, "push": true, "puts": true, "sleep": true, "split": true,
}

// literalScope is arg as a scope of kind, validated as runtime.Literal
// validates it. A net scope comes back normalised (lowercase, no trailing
// dot), as the runtime compares it. A path comes back as written: the
// program and --allow both resolve a relative path against the working
// directory, so the printed grant names the same file.
func literalScope(kind runtime.Kind, arg string) (string, error) {
	probe := arg
	isPath := kind == runtime.FSRead || kind == runtime.FSWrite
	if isPath && arg != "" && !filepath.IsAbs(arg) {
		probe = "/" + arg // Literal wants it absolute; the base does not matter
	}
	c, err := runtime.Literal(kind, probe)
	if err != nil {
		return "", err
	}
	if isPath {
		return arg, nil
	}
	return c.Scope, nil
}

// maxURLLen is http_get's limit on a URL.
const maxURLLen = 8 << 10

// defaultPorts are the ports a URL without one reaches, as in http_get.
var defaultPorts = map[string]string{"http": "80", "https": "443"}

// urlScope is the net scope http_get(arg) is checked against: the URL's
// host and the port it reaches, as http_get computes it before the first
// request. It does not follow redirects; each one is checked at run time.
func urlScope(_ runtime.Kind, arg string) (string, error) {
	if len(arg) > maxURLLen {
		return "", fmt.Errorf("URL is %d bytes long (max %d)", len(arg), maxURLLen)
	}
	u, err := url.Parse(arg)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return "", errors.New("invalid url")
	}
	if u.Hostname() == "" {
		return "", errors.New("URL has no host")
	}
	port := u.Port()
	if port == "" {
		port = defaultPorts[u.Scheme]
	}
	return literalScope(runtime.Net, net.JoinHostPort(u.Hostname(), port))
}
```

- [ ] **Step 4: Run and watch it pass**

Run: `go test -race ./arv/evaluator/ ./arv/check/`
Expected: `ok`. Then `gofmt -l .` (prints nothing), `go vet ./...` and `go test -race ./...`.

- [ ] **Step 5: Commit**

```bash
git add arv/evaluator/names.go arv/evaluator/names_test.go arv/check
git commit -m "feat(check): classify every builtin by the grant it needs" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
