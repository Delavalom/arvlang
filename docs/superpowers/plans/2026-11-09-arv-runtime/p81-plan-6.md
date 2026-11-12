# Phase 8.1, task p81.6: The `arv:allow` header: `Manifest`, `Grants`, `Outside`

> **For agentic workers:** this file is one task, p81.6. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: task p81.5 is committed in your tree. Start every shell command with `cd <your worktree> &&` and use absolute paths: the shell's working directory does not persist. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify` and never push; keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0` (toolchain 1.26.2): `t.Chdir`, `min` and `max` are available; do not use `testing/synctest` or `sync.WaitGroup.Go`. Before a long test run, check `df -h /System/Volumes/Data`; below 1 GB free, stop and report (only `go clean -cache` may be deleted). This machine is heavily loaded: keep wall-clock bounds generous.
>
> Every code block below was applied, in task order, to a scratch copy of `feat/phases-3-8` (860a158) and passed `gofmt -l .`, `go vet ./...` and `go test -race ./...` (including `e2e/`). Design: `15-phase8-tooling.md` §8.1. Deviations from it, and why: `p81-plan-1.md`. Phases 5E, 6, 7 and 8.2 are built at the same time and also touch the CLI, parser and evaluator: change only the files this task names.

### Task p81.6: The `arv:allow` header: `Manifest`, `Grants`, `Outside`

The header is the lines before the first line of code, each blank or a `//` comment. An entry is `// arv:allow kind=scope`, one per line, no spaces in it; anything else is an error naming the line. A comment after the header is not read even if it says `arv:allow`: it may be inside a multi-line string, and the lexer does not keep comments. `Grants` parses entries exactly as `--allow` does (`runtime.Parse`, relative paths against the working directory). `Outside` decides, for each distinct site, as the runtime would: `runtime.CapSet.Admit` on the request made absolute. Like `--allow`, that reads filesystem metadata (lstat, readlink), never a file's contents.

**Files:**
- Create: `arv/check/manifest.go`
- Create (test): `arv/check/manifest_test.go`

- [ ] **Step 1: Write the failing test**

Create `arv/check/manifest_test.go`:

```go
package check

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestManifestReadsTheHeader(t *testing.T) {
	src := "// Fetch a page.\r\n" +
		"//\n" +
		"// arv:allow net=api.github.com\n" +
		"\n" +
		"  //arv:allow\tfs_write=./out  \n" +
		"// arv:allowed is not the directive\n" +
		"let x = 1;\n" +
		"// arv:allow env=HOME\n"
	got, err := Manifest(src)
	if err != nil {
		t.Fatal(err)
	}
	want := []Entry{{3, "net=api.github.com"}, {5, "fs_write=./out"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Manifest = %+v, want %+v", got, want)
	}
}

func TestManifestRejectsMalformedEntries(t *testing.T) {
	for _, src := range []string{
		"// arv:allow\nputs(1);",
		"// arv:allow   \n",
		"// arv:allow net=a net=b\n",
	} {
		if _, err := Manifest(src); err == nil || !strings.Contains(err.Error(), "line 1: arv:allow wants one kind=scope") {
			t.Errorf("Manifest(%q) = %v", src, err)
		}
	}
}

func TestNoHeaderNoEntries(t *testing.T) {
	for _, src := range []string{"", "puts(1);", "puts(1);\n// arv:allow net=x\n"} {
		got, err := Manifest(src)
		if err != nil || got != nil {
			t.Errorf("Manifest(%q) = %v, %v", src, got, err)
		}
	}
}

func TestGrantsParseAsAllowDoes(t *testing.T) {
	dir := t.TempDir()
	caps, err := Grants([]Entry{{1, "net=API.github.com"}, {2, "fs_read=data"}}, dir)
	if err != nil {
		t.Fatal(err)
	}
	canon, _ := filepath.EvalSymlinks(dir) // macOS: /var is /private/var
	if caps[0].String() != `net("api.github.com")` || caps[1].Scope != filepath.Join(canon, "data") {
		t.Errorf("Grants = %v", caps)
	}
	if _, err := Grants([]Entry{{7, "disk=/"}}, dir); err == nil || !strings.HasPrefix(err.Error(), "line 7: ") {
		t.Errorf("Grants(disk=/) = %v", err)
	}
}

func TestOutside(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "out"), 0o755); err != nil {
		t.Fatal(err)
	}
	r, err := Source(`http_get("https://api.github.com/zen");
http_get("https://evil.example/");
write_file("out/report.json", "x");
read_file("secret.txt");
getenv("HOME");
http_get("https://evil.example/again");`)
	if err != nil {
		t.Fatal(err)
	}
	grants, err := Grants([]Entry{{1, "net=api.github.com"}, {2, "fs_write=out"}, {3, "env=HOME"}}, dir)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, n := range Outside(r, grants, dir) {
		got = append(got, n.String()+" "+n.Fn)
	}
	want := []string{"net=evil.example:443 http_get", "fs_read=secret.txt read_file"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Outside = %q, want %q", got, want)
	}
}
```

- [ ] **Step 2: Run and watch it fail**

Run: `go test ./arv/check/`
Expected: build failure: `undefined: Manifest`, `undefined: Entry`, `undefined: Grants`, `undefined: Outside`.

- [ ] **Step 3: Implement**

Create `arv/check/manifest.go`:

```go
package check

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/delavalom/arvlang/arv/runtime"
)

// Entry is one "// arv:allow kind=scope" line of a program's header.
type Entry struct {
	Line int
	Text string // kind=scope, as --allow takes it
}

const directive = "arv:allow"

// Manifest returns the arv:allow entries of src's header: the lines
// before the first line of code, each blank or a // comment. A comment
// after the header is not read, even if it says arv:allow: it may be part
// of a string. Each entry is exactly one kind=scope, with no spaces.
func Manifest(src string) ([]Entry, error) {
	var entries []Entry
	for i, line := range strings.Split(src, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "//") {
			break
		}
		c := strings.TrimSpace(line[2:])
		if !strings.HasPrefix(c, directive) {
			continue
		}
		rest := c[len(directive):]
		if rest != "" && rest[0] != ' ' && rest[0] != '\t' {
			continue // arv:allowed, say: not the directive
		}
		rest = strings.TrimSpace(rest)
		if rest == "" || strings.ContainsAny(rest, " \t") {
			return nil, fmt.Errorf("line %d: %s wants one kind=scope, got %q", i+1, directive, rest)
		}
		entries = append(entries, Entry{Line: i + 1, Text: rest})
	}
	return entries, nil
}

// Grants turns entries into grants exactly as --allow does
// (runtime.Parse): a relative path is resolved against cwd.
func Grants(entries []Entry, cwd string) ([]runtime.Capability, error) {
	caps := make([]runtime.Capability, 0, len(entries))
	for _, e := range entries {
		c, err := runtime.Parse(e.Text, cwd)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", e.Line, err)
		}
		caps = append(caps, c)
	}
	return caps, nil
}

// Outside returns the first site of each grant in r that grants do not
// cover, in source order. It decides as the runtime does, with
// runtime.CapSet.Admit, after resolving a relative path against cwd as
// the program would: like --allow, that reads filesystem metadata (lstat,
// readlink) to resolve symlinks, and nothing else.
func Outside(r *Report, grants []runtime.Capability, cwd string) []Need {
	set := runtime.NewCapSet(grants...)
	seen := map[string]bool{}
	var out []Need
	for _, s := range r.Sites {
		if seen[s.String()] {
			continue
		}
		seen[s.String()] = true
		scope := s.Scope
		if (s.Kind == runtime.FSRead || s.Kind == runtime.FSWrite) && !filepath.IsAbs(scope) {
			scope = runtime.Resolve(cwd, scope)
		}
		lit, err := runtime.Literal(s.Kind, scope)
		if err == nil {
			_, err = set.Admit(lit)
		}
		if err != nil {
			out = append(out, s)
		}
	}
	return out
}
```

- [ ] **Step 4: Run and watch it pass**

Run: `go test -race ./arv/check/`
Expected: `ok`. Then `gofmt -l .` (prints nothing), `go vet ./...` and `go test -race ./...`.

- [ ] **Step 5: Commit**

```bash
git add arv/check
git commit -m "feat(check): read and apply an arv:allow header" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
