# Phase 1d: Embedding API and `arv run`

**Goal:** A Go host can call `arv.Run(src, arv.Options{Caps: …})`, and a person can run `arv run --allow net=api.github.com script.arv`. With no `--allow`, a script can compute and print, and nothing more (I1).

---

### Task 1.6: `arv.Run`

**Files:**
- Create: `arv/arv.go`
- Test: `arv/arv_test.go`

- [ ] **Step 1: Write the failing tests**

```go
package arv

import (
	"bytes"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
)

func TestRunReturnsValueAndOutput(t *testing.T) {
	var out bytes.Buffer
	got, err := Run(`puts("start"); let add = fn(a, b) { a + b }; add(2, 3)`, Options{Out: &out})
	if err != nil {
		t.Fatal(err)
	}
	if got.Inspect() != "5" || out.String() != "start\n" {
		t.Fatalf("got %s, out %q", got.Inspect(), out.String())
	}
}

func TestRunSurfacesErrors(t *testing.T) {
	if _, err := Run(`let = 1`, Options{}); err == nil || !strings.HasPrefix(err.Error(), "parse error") {
		t.Fatalf("want parse error, got %v", err)
	}
	if _, err := Run(`getenv("HOME")`, Options{}); err == nil || !strings.Contains(err.Error(), "capability denied") {
		t.Fatalf("want capability denied, got %v", err)
	}
	home, _ := runtime.New(runtime.Env, "HOME")
	if _, err := Run(`getenv("HOME")`, Options{Caps: []runtime.Capability{home}}); err != nil {
		t.Fatalf("granted env: %v", err)
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go test ./arv/`
Expected: build failure, `undefined: Run`.

- [ ] **Step 3: Implement**

```go
// Package arv is the embedding API: run Arv source with an explicit set of
// capabilities. Nothing reaches the OS unless the host grants it here.
package arv

import (
	"context"
	"errors"
	"io"
	"strings"

	"github.com/delavalom/arvlang/arv/evaluator"
	"github.com/delavalom/arvlang/arv/lexer"
	"github.com/delavalom/arvlang/arv/parser"
	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// Options configure one program run.
type Options struct {
	Ctx  context.Context      // defaults to context.Background()
	Caps []runtime.Capability // authority granted to the root task
	Out  io.Writer            // where puts writes; defaults to io.Discard
}

// Run parses and evaluates src. A parse error or an Arv error value that
// reaches the top level is returned as a Go error.
func Run(src string, opts Options) (value.Object, error) {
	if opts.Ctx == nil {
		opts.Ctx = context.Background()
	}
	if opts.Out == nil {
		opts.Out = io.Discard
	}
	p := parser.New(lexer.New(src))
	program := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		return nil, errors.New("parse error: " + strings.Join(errs, "; "))
	}
	root := runtime.NewRoot(opts.Ctx, runtime.NewCapSet(opts.Caps...), opts.Out)
	result := evaluator.New(root).Eval(program, value.NewEnvironment())
	if e, ok := result.(*value.Error); ok {
		return result, errors.New(e.Message)
	}
	return result, nil
}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./arv/`
Expected: `ok`.

- [ ] **Step 5: Commit**

```bash
git add arv/arv.go arv/arv_test.go
git commit -m "feat: arv.Run embedding API with explicit capabilities"
```

---

### Task 1.7: `arv run [--allow kind=scope]... FILE`

**Files:**
- Replace: `main.go`
- Test: `main_test.go`
- Modify: `.gitignore` (add `bin/`)

All logic lives in `runCLI(args, stdout, stderr) int`, so it's testable without spawning a process. Relative `fs_read=data` grants resolve against the current directory. Exit codes: `0` ok, `1` the program failed, `2` usage error.

- [ ] **Step 1: Write the failing tests**

```go
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeScript(t *testing.T, src string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "main.arv")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRunCLI(t *testing.T) {
	t.Setenv("ARV_GREETING", "hola")
	script := writeScript(t, "// greet\nputs(getenv(\"ARV_GREETING\"));")

	var out, errOut bytes.Buffer
	if code := runCLI([]string{"run", script}, &out, &errOut); code != 1 {
		t.Fatalf("without --allow: exit %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "capability denied") {
		t.Fatalf("stderr = %q", errOut.String())
	}

	out.Reset()
	errOut.Reset()
	if code := runCLI([]string{"run", "--allow", "env=ARV_GREETING", script}, &out, &errOut); code != 0 {
		t.Fatalf("with --allow: exit %d, stderr %q", code, errOut.String())
	}
	if out.String() != "hola\n" {
		t.Fatalf("stdout = %q", out.String())
	}
}

func TestRunCLIRejectsBadFlags(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := runCLI([]string{"run", "--allow", "disk=/", "x.arv"}, &out, &errOut); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	if code := runCLI([]string{"build"}, &out, &errOut); code != 2 {
		t.Fatalf("unknown command exit %d, want 2", code)
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go test . -run RunCLI`
Expected: build failure, `undefined: runCLI`.

- [ ] **Step 3: Replace `main.go`**

```go
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/delavalom/arvlang/arv"
	"github.com/delavalom/arvlang/arv/repl"
	"github.com/delavalom/arvlang/arv/runtime"
)

const usage = `usage:
  arv                                  start the REPL (no capabilities)
  arv run [--allow kind=scope]... FILE run a program

kinds: net=HOST (or *.domain, *), fs_read=PATH, fs_write=PATH, env=NAME`

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Arv REPL. Ctrl-D to exit.")
		repl.Start(os.Stdin, os.Stdout)
		return
	}
	os.Exit(runCLI(os.Args[1:], os.Stdout, os.Stderr))
}

// runCLI implements every non-REPL command and returns the exit code.
func runCLI(args []string, stdout, stderr io.Writer) int {
	if args[0] != "run" {
		fmt.Fprintln(stderr, usage)
		return 2
	}
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	var caps []runtime.Capability
	rest := args[1:]
	for len(rest) >= 2 && rest[0] == "--allow" {
		c, err := runtime.Parse(rest[1], cwd)
		if err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 2
		}
		caps = append(caps, c)
		rest = rest[2:]
	}
	if len(rest) != 1 {
		fmt.Fprintln(stderr, usage)
		return 2
	}
	src, err := os.ReadFile(rest[0])
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	if _, err := arv.Run(string(src), arv.Options{Caps: caps, Out: stdout}); err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	return 0
}
```

- [ ] **Step 4: Run all tests, then try the binary**

Run: `go test -race ./... && echo 'bin/' >> .gitignore && go build -o bin/arv .`

Build to `bin/arv`, not `./arv`: `arv/` is a directory, and `go build -o arv` would put the binary inside it.

Then:

```bash
printf 'puts(getenv("HOME"));\n' > /tmp/home.arv
bin/arv run /tmp/home.arv                 # error: capability denied: env("HOME") in task root (holds: []) ; exit 1
bin/arv run --allow env=HOME /tmp/home.arv  # prints your home directory ; exit 0
```

- [ ] **Step 5: Commit**

```bash
git add main.go main_test.go .gitignore
git commit -m "feat(cli): arv run with --allow grants"
```

**Phase 1 is done when:** `go test -race ./...` passes, and the two commands above behave as shown.
