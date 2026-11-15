# Phase 8.1, task p81.7: `arv check FILE`

> **For agentic workers:** this file is one task, p81.7. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: task p81.6 is committed in your tree. Start every shell command with `cd <your worktree> &&` and use absolute paths: the shell's working directory does not persist. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify` and never push; keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0` (toolchain 1.26.2): `t.Chdir`, `min` and `max` are available; do not use `testing/synctest` or `sync.WaitGroup.Go`. Before a long test run, check `df -h /System/Volumes/Data`; below 1 GB free, stop and report (only `go clean -cache` may be deleted). This machine is heavily loaded: keep wall-clock bounds generous.
>
> Every code block below was applied, in task order, to a scratch copy of `feat/phases-3-8` (860a158) and passed `gofmt -l .`, `go vet ./...` and `go test -race ./...` (including `e2e/`). Design: `15-phase8-tooling.md` §8.1. Deviations from it, and why: `p81-plan-1.md`. Phases 5E, 6, 7 and 8.2 are built at the same time and also touch the CLI, parser and evaluator: change only the files this task names.

### Task p81.7: `arv check FILE`

The command prints one `--allow kind=scope` line per need, then one `# fn at line:col: why` line per note (control characters in a note become `?`). Exit 0; 1 for an unreadable file, a parse error (worded as `run` words it, through `arv.ParseError.In`), a bad header, or a need outside the header; 2 for usage. The code lives in a new file, `check_cli.go`; `main.go` gets only the dispatch and one usage line, so merges with the other phases' flags stay small.

**Files:**
- Create: `check_cli.go`
- Modify: `main.go` (two hunks)
- Create (test): `check_cli_test.go`

- [ ] **Step 1: Write the failing test**

Create `check_cli_test.go`:

```go
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runArv runs the CLI with args and returns its exit code and output.
func runArv(args ...string) (int, string, string) {
	var out, errOut bytes.Buffer
	code := runCLI(args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestCheckPrintsGrantsAndNotes(t *testing.T) {
	script := writeScript(t, `let get = fn(url) { http_get(url) };
let t = spawn get("https://api.github.com/zen") with [net("api.github.com")];
puts(getenv("HOME"), read_file("data/in.txt"));`)
	code, out, errOut := runArv("check", script)
	want := "--allow net=api.github.com\n--allow fs_read=data/in.txt\n--allow env=HOME\n" +
		"# http_get at 1:21: host not statically known\n"
	if code != 0 || out != want || errOut != "" {
		t.Fatalf("exit %d\nstdout %q\nwant   %q\nstderr %q", code, out, want, errOut)
	}
}

// check reads the program; it never runs it.
func TestCheckDoesNotRunTheProgram(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	script := writeScript(t, `write_file("made.txt", "x"); puts("ran");
let f = fn() { f() }; f();`)
	code, out, errOut := runArv("check", script)
	if code != 0 || out != "--allow fs_write=made.txt\n" || errOut != "" {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errOut)
	}
	if _, err := os.Stat(filepath.Join(dir, "made.txt")); !os.IsNotExist(err) {
		t.Fatalf("check created made.txt: %v", err)
	}
}

// A scope that would not survive the shell, or a second line, is quoted.
func TestCheckQuotesOddScopes(t *testing.T) {
	script := writeScript(t, "getenv(\"A B\"); getenv(\"X\n--allow fs_read=/\");")
	code, out, _ := runArv("check", script)
	want := "--allow $'env=A B'\n--allow $'env=X\\x0a--allow fs_read=/'\n"
	if code != 0 || out != want {
		t.Fatalf("exit %d, stdout %q, want %q", code, out, want)
	}
}

func TestCheckWithAManifest(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	covered := writeScript(t, "// arv:allow net=api.github.com\n// arv:allow env=HOME\n"+
		`http_get("https://api.github.com/zen"); getenv("HOME");`)
	if code, out, errOut := runArv("check", covered); code != 0 || errOut != "" {
		t.Fatalf("covered: exit %d, stdout %q, stderr %q", code, out, errOut)
	}
	outside := writeScript(t, "// arv:allow net=api.github.com\n"+
		`http_get("https://api.github.com/zen");`+"\n"+`getenv("HOME"); read_file("x");`)
	code, out, errOut := runArv("check", outside)
	if code != 1 || out != "--allow net=api.github.com:443\n--allow fs_read=x\n--allow env=HOME\n" {
		t.Fatalf("outside: exit %d, stdout %q", code, out)
	}
	want := "error: " + outside + " needs env=HOME (getenv at 3:1), fs_read=x (read_file at 3:17), outside its arv:allow manifest\n"
	if errOut != want {
		t.Fatalf("stderr %q\nwant   %q", errOut, want)
	}
	bad := writeScript(t, "// arv:allow disk=/\nputs(1);")
	if code, _, errOut := runArv("check", bad); code != 1 || !strings.Contains(errOut, "line 1: ") {
		t.Fatalf("bad manifest: exit %d, stderr %q", code, errOut)
	}
}

func TestCheckUsageAndErrors(t *testing.T) {
	for _, args := range [][]string{{"check"}, {"check", "a.arv", "b.arv"}, {"check", "--allow", "net=x"}} {
		if code, _, errOut := runArv(args...); code != 2 || !strings.HasPrefix(errOut, "usage:") {
			t.Errorf("%q: exit %d, stderr %q", args, code, errOut)
		}
	}
	if code, _, errOut := runArv("check", filepath.Join(t.TempDir(), "missing.arv")); code != 1 || !strings.HasPrefix(errOut, "error: ") {
		t.Errorf("missing file: exit %d, stderr %q", code, errOut)
	}
	script := writeScript(t, "let = ;")
	code, _, errOut := runArv("check", script)
	if code != 1 || !strings.HasPrefix(errOut, "error: parse error: ") || !strings.Contains(errOut, script+":1:") {
		t.Errorf("parse error: exit %d, stderr %q", code, errOut)
	}
}
```

- [ ] **Step 2: Run and watch it fail**

Run: `go test . -run Check`
Expected: every `TestCheck*` fails: `runCLI` does not know `check`, so it prints the usage and exits 2 (`exit 2, stdout ""`).

- [ ] **Step 3: Implement**

Create `check_cli.go`:

```go
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/delavalom/arvlang/arv"
	"github.com/delavalom/arvlang/arv/check"
)

// checkCLI implements "arv check FILE": print the grants FILE can need, one
// "--allow kind=scope" line each, then a "# " line for each call whose
// need is not known before running. It never runs FILE. If FILE has an
// arv:allow header, a need outside it is an error (exit 1).
func checkCLI(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 || strings.HasPrefix(args[0], "--") {
		fmt.Fprintln(stderr, usage)
		return 2
	}
	file := args[0]
	src, err := os.ReadFile(file)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	r, err := checkSource(string(src), file)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	for _, n := range r.Needs {
		fmt.Fprintln(stdout, "--allow", n.Quoted())
	}
	for _, n := range r.Notes {
		fmt.Fprintln(stdout, "#", oneLine(n.String()))
	}
	entries, err := check.Manifest(string(src))
	if err != nil {
		fmt.Fprintf(stderr, "error: %s: %s\n", file, err)
		return 1
	}
	if entries == nil {
		return 0
	}
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	if err := checkManifest(r, entries, file, cwd); err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	return 0
}

// checkSource checks src, giving a parse error the file's name as run
// does.
func checkSource(src, file string) (*check.Report, error) {
	r, err := check.Source(src)
	var pe *check.ParseError
	if errors.As(err, &pe) {
		return nil, errors.New((&arv.ParseError{Errs: pe.Errs}).In(file))
	}
	return r, err
}

// checkManifest fails if r needs a grant outside the manifest entries.
func checkManifest(r *check.Report, entries []check.Entry, file, cwd string) error {
	grants, err := check.Grants(entries, cwd)
	if err != nil {
		return fmt.Errorf("%s: %w", file, err)
	}
	out := check.Outside(r, grants, cwd)
	if len(out) == 0 {
		return nil
	}
	needs := make([]string, len(out))
	for i, n := range out {
		needs[i] = fmt.Sprintf("%s (%s at %d:%d)", n.Quoted(), n.Fn, n.Line, n.Col)
	}
	return fmt.Errorf("%s needs %s, outside its arv:allow manifest", file, strings.Join(needs, ", "))
}

// oneLine replaces control characters, so a note is one line.
func oneLine(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return '?'
		}
		return r
	}, s)
}
```

In `main.go`, replace:

```go
	if args[0] != "run" {
		fmt.Fprintln(stderr, usage)
```

with:

```go
	if args[0] == "check" {
		return checkCLI(args[1:], stdout, stderr)
	}
	if args[0] != "run" {
		fmt.Fprintln(stderr, usage)
```

In `main.go`, replace:

```go
  arv run [flags] FILE   run a program
```

with:

```go
  arv run [flags] FILE   run a program
  arv check FILE         print the --allow grants FILE can need, without running it
```

- [ ] **Step 4: Run and watch it pass**

Run: `go test -race .`
Expected: `ok`. Then `gofmt -l .` (prints nothing), `go vet ./...` and `go test -race ./...`.

- [ ] **Step 5: Commit**

```bash
git add check_cli.go check_cli_test.go main.go
git commit -m "feat(cli): arv check prints the grants a program can need" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
