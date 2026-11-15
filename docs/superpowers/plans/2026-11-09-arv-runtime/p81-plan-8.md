# Phase 8.1, task p81.8: `arv run --manifest`

> **For agentic workers:** this file is one task, p81.8. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: task p81.7 is committed in your tree. Start every shell command with `cd <your worktree> &&` and use absolute paths: the shell's working directory does not persist. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify` and never push; keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0` (toolchain 1.26.2): `t.Chdir`, `min` and `max` are available; do not use `testing/synctest` or `sync.WaitGroup.Go`. Before a long test run, check `df -h /System/Volumes/Data`; below 1 GB free, stop and report (only `go clean -cache` may be deleted). This machine is heavily loaded: keep wall-clock bounds generous.
>
> Every code block below was applied, in task order, to a scratch copy of `feat/phases-3-8` (860a158) and passed `gofmt -l .`, `go vet ./...` and `go test -race ./...` (including `e2e/`). Design: `15-phase8-tooling.md` §8.1. Deviations from it, and why: `p81-plan-1.md`. Phases 5E, 6, 7 and 8.2 are built at the same time and also touch the CLI, parser and evaluator: change only the files this task names.

### Task p81.8: `arv run --manifest`

`--manifest` takes no value, unlike every other `run` flag, so `splitManifest` takes it out before `parseRun` sees the arguments, reading them the way `parseRun` does (`--allow --manifest` stays a bad grant). After the file is read, `applyManifest` sets `opts.Caps` to the header's grants and refuses (exit 1, nothing evaluated) if `arv check` finds a site outside them. A note does not refuse: the run holds the header's grants and nothing else, so the runtime denies anything outside them when it is tried (`TestRunManifestIsTheWholeGrantSet`). A parse error is left to `arv.Run`, so it reads exactly as without `--manifest`. `--manifest` with `--allow` is a usage error (exit 2).

**Files:**
- Create: `manifest_cli.go`
- Modify: `main.go` (three hunks)
- Create (test): `manifest_cli_test.go`

- [ ] **Step 1: Write the failing test**

Create `manifest_cli_test.go`:

```go
package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSplitManifest(t *testing.T) {
	for _, tt := range []struct {
		in, rest []string
		manifest bool
	}{
		{[]string{"f.arv"}, []string{"f.arv"}, false},
		{[]string{"--manifest", "f.arv"}, []string{"f.arv"}, true},
		{[]string{"--timeout", "5s", "--manifest", "f.arv"}, []string{"--timeout", "5s", "f.arv"}, true},
		{[]string{"--allow", "--manifest", "f.arv"}, []string{"--allow", "--manifest", "f.arv"}, false},
		{[]string{"--manifest"}, nil, true},
	} {
		rest, manifest := splitManifest(tt.in)
		if !reflect.DeepEqual(rest, tt.rest) || manifest != tt.manifest {
			t.Errorf("splitManifest(%q) = %q, %v", tt.in, rest, manifest)
		}
	}
}

func TestRunManifestGrantsTheHeader(t *testing.T) {
	t.Setenv("ARV_P81", "hola")
	script := writeScript(t, "// arv:allow env=ARV_P81\nputs(getenv(\"ARV_P81\"));")
	if code, out, errOut := runArv("run", "--timeout", "30s", "--manifest", script); code != 0 || out != "hola\n" {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errOut)
	}
	// Without --manifest the header grants nothing.
	if code, _, errOut := runArv("run", script); code != 1 || !strings.Contains(errOut, "capability denied") {
		t.Fatalf("without --manifest: exit %d, stderr %q", code, errOut)
	}
}

// A need arv check cannot see still runs, holding the header's grants
// alone, so the runtime denies it.
func TestRunManifestIsTheWholeGrantSet(t *testing.T) {
	t.Setenv("ARV_P81_B", "secret")
	script := writeScript(t, "// arv:allow env=ARV_P81_A\nlet get = fn(name) { getenv(name) };\nputs(get(\"ARV_P81_B\"));")
	code, out, errOut := runArv("run", "--manifest", script)
	if code != 1 || out != "" || !strings.Contains(errOut, `capability denied: env("ARV_P81_B")`) {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errOut)
	}
}

func TestRunManifestRefusesBeforeRunning(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	script := writeScript(t, "// arv:allow env=A\nputs(\"ran\");\nwrite_file(\"made.txt\", \"x\");\ngetenv(\"B\");")
	code, out, errOut := runArv("run", "--manifest", script)
	want := "error: " + script + " needs fs_write=made.txt (write_file at 3:1), env=B (getenv at 4:1), outside its arv:allow manifest\n"
	if code != 1 || out != "" || errOut != want {
		t.Fatalf("exit %d, stdout %q\nstderr %q\nwant   %q", code, out, errOut, want)
	}
	if _, err := os.Stat(filepath.Join(dir, "made.txt")); !os.IsNotExist(err) {
		t.Fatalf("made.txt exists: %v", err)
	}
}

func TestRunManifestErrors(t *testing.T) {
	pure := writeScript(t, "puts(1 + 1);")
	if code, out, errOut := runArv("run", "--manifest", pure); code != 0 || out != "2\n" {
		t.Errorf("no header, pure program: exit %d, stdout %q, stderr %q", code, out, errOut)
	}
	if code, _, errOut := runArv("run", "--allow", "env=A", "--manifest", pure); code != 2 || !strings.Contains(errOut, "cannot be combined with --allow") {
		t.Errorf("with --allow: exit %d, stderr %q", code, errOut)
	}
	bad := writeScript(t, "// arv:allow fs_read=\nputs(1);")
	if code, _, errOut := runArv("run", "--manifest", bad); code != 1 || !strings.Contains(errOut, bad+": line 1: ") {
		t.Errorf("bad header: exit %d, stderr %q", code, errOut)
	}
	broken := writeScript(t, "// arv:allow env=A\nlet = ;")
	code, _, errOut := runArv("run", "--manifest", broken)
	if code != 1 || !strings.HasPrefix(errOut, "error: parse error: ") || !strings.Contains(errOut, broken+":2:") {
		t.Errorf("parse error: exit %d, stderr %q", code, errOut)
	}
	if code, _, errOut := runArv("run", "--manifest"); code != 2 || !strings.HasPrefix(errOut, "usage:") {
		t.Errorf("no file: exit %d, stderr %q", code, errOut)
	}
}
```

- [ ] **Step 2: Run and watch it fail**

Run: `go test . -run Manifest`
Expected: build failure: `undefined: splitManifest`.

- [ ] **Step 3: Implement**

Create `manifest_cli.go`:

```go
package main

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/delavalom/arvlang/arv"
	"github.com/delavalom/arvlang/arv/check"
)

// splitManifest takes --manifest, a flag without a value, out of run's
// arguments. It reads flags as parseRun does, every other flag taking a
// value, so "--allow --manifest" stays a (bad) grant.
func splitManifest(args []string) (rest []string, manifest bool) {
	for len(args) > 0 && strings.HasPrefix(args[0], "--") {
		if args[0] == "--manifest" {
			manifest = true
			args = args[1:]
			continue
		}
		if len(args) < 2 {
			break
		}
		rest = append(rest, args[0], args[1])
		args = args[2:]
	}
	return append(rest, args...), manifest
}

// applyManifest makes opts grant exactly src's arv:allow header, and
// refuses (exit 1), before anything runs, a program that arv check finds
// needs a grant outside it. A need arv check cannot see (a note) is not
// refused: the run holds the header's grants and nothing else, so the
// runtime denies it if it is outside. A program that does not parse is
// left to arv.Run, which reports it as it does without --manifest. It
// returns 0 to go on running.
func applyManifest(opts *arv.Options, src, file, cwd string, stderr io.Writer) int {
	if len(opts.Caps) > 0 {
		fmt.Fprintln(stderr, "error: --manifest grants exactly the file's arv:allow lines; it cannot be combined with --allow")
		return 2
	}
	entries, err := check.Manifest(src)
	if err == nil {
		opts.Caps, err = check.Grants(entries, cwd)
	}
	if err != nil {
		fmt.Fprintf(stderr, "error: %s: %s\n", file, err)
		return 1
	}
	r, err := check.Source(src)
	var pe *check.ParseError
	if errors.As(err, &pe) {
		return 0
	}
	if err == nil {
		err = checkManifest(r, entries, file, cwd)
	}
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	return 0
}
```

In `main.go`, replace:

```go
	opts, file, err := parseRun(args[1:], cwd)
```

with:

```go
	runArgs, manifest := splitManifest(args[1:])
	opts, file, err := parseRun(runArgs, cwd)
```

In `main.go`, replace:

```go
		return 1
	}
	opts.Out = stdout
```

with:

```go
		return 1
	}
	if manifest {
		if code := applyManifest(&opts, string(src), file, cwd, stderr); code != 0 {
			return code
		}
	}
	opts.Out = stdout
```

In `main.go`, replace:

```go
  --alloc-mb N         allocate at most N MiB of Arv values in all (default: no limit)`
```

with:

```go
  --alloc-mb N         allocate at most N MiB of Arv values in all (default: no limit)
  --manifest           grant exactly FILE's "// arv:allow kind=scope" header lines, and
                       refuse to run if arv check finds a need outside them`
```

- [ ] **Step 4: Run and watch it pass**

Run: `go test -race .`
Expected: `ok`. Then `gofmt -l .` (prints nothing), `go vet ./...` and `go test -race ./...`.

- [ ] **Step 5: Commit**

```bash
git add manifest_cli.go manifest_cli_test.go main.go
git commit -m "feat(cli): arv run --manifest grants exactly the header" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
