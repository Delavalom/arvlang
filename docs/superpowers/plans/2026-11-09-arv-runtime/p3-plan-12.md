# Phase 3, task p3.12: CLI flags and `arv.Options.Limits`

> **For agentic workers:** this file is one task, p3.12. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: tasks p3.1 to p3.11 are committed in your tree. Start every shell command with `cd <your worktree> &&`. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify`; keep Go files under 15 KB. `go.mod` says 1.22.5, so do not use `os.Root`, `t.Chdir`, `testing/synctest` or `sync.WaitGroup.Go`.
>
> Every code block below was applied, in task order, to a scratch copy of the tree and passed `go vet ./...` and `go test -race ./...` (including `e2e/`). Design: `10-phase3-budgets.md`. Deviations from it, and why: `p3-plan-1.md`.

### Task p3.12: CLI flags and `arv.Options.Limits`

`arv.Options` gains `Limits runtime.Limits`, and `arv.Run` passes it to `RunLimited`. `arv run` accepts `--allow`, `--timeout`, `--steps`, `--tasks` and `--alloc-mb` in any order before the file.
- **Defaults:** `--timeout` is 10 minutes, and `0` turns it off. `--tasks N` sets the run-wide live-task cap (`Limits.LiveTasks`, default 1000). `--steps` and `--alloc-mb` have no default limit.
- **Bad values** exit 2 with `error: --flag: …`.
- **An unknown flag** prints `error: unknown flag "--x"` followed by the usage, which keeps the Phase 1 e2e check (`usage:`, exit 2) passing.

**Files:**
- Modify: `arv/arv.go`
- Modify (test): `arv/arv_test.go`
- Modify: `main.go`
- Create (test): `main_limits_test.go`

- [ ] **Step 1: Write the failing tests**

At the end of `arv/arv_test.go`, after

```go
		t.Fatalf("got %q at %q, want capability denied at 3:3", ae.Msg, ae.Pos)
	}
}
```

add:

```go
// Options.Limits is the run's budget.
func TestRunLimits(t *testing.T) {
	src := "let f = fn(n) { if (n == 0) { 0 } else { f(n - 1) } }; f(5000)"
	if _, err := Run(src, Options{Limits: runtime.Limits{Steps: 1000}}); err == nil || err.Error() != "budget exceeded: steps (limit 1000)" {
		t.Fatalf("err = %v, want the steps budget error", err)
	}
	if got, err := Run(src, Options{}); err != nil || got.Inspect() != "0" {
		t.Fatalf("no limits: %v, %v", got, err)
	}
	deep := `let f = fn(n) { if (n == 0) { "no cap" } else { await spawn f(n - 1) } }; f(100)`
	_, err := Run(deep, Options{Limits: runtime.Limits{LiveTasks: 10}})
	if err == nil || err.Error() != "budget exceeded: tasks (limit 10 live tasks)" {
		t.Fatalf("err = %v, want the live-task cap", err)
	}
}
```


Create `main_limits_test.go`:

```go
package main

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// cli runs the CLI and returns the exit code, stdout and stderr.
func cli(args ...string) (int, string, string) {
	var out, errOut bytes.Buffer
	code := runCLI(args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestBudgetFlags(t *testing.T) {
	countdown := writeScript(t, "let f = fn(n) { if (n == 0) { 0 } else { f(n - 1) } };\nputs(f(5000));")
	spawner := writeScript(t, "let f = fn(n) { if (n == 0) { 0 } else { await spawn f(n - 1) } };\nf(100)")
	grow := writeScript(t, `let grow = fn(n, acc) { if (n == 0) { acc } else { grow(n - 1, push(acc, "x")) } };`+"\nputs(len(grow(5000, [])));")
	sleeper := writeScript(t, "sleep(5000)")
	tests := []struct {
		args []string
		want string // stderr fragment
	}{
		{[]string{"--steps", "1000", countdown}, "error: budget exceeded: steps (limit 1000) (at " + countdown + ":"},
		{[]string{"--tasks", "5", spawner}, "error: budget exceeded: tasks (limit 5 live tasks) (at " + spawner + ":1:48)"},
		{[]string{"--alloc-mb", "1", grow}, "error: budget exceeded: alloc (limit 1048576 bytes) (at " + grow + ":"},
		{[]string{"--timeout", "100ms", sleeper}, "error: budget exceeded: timeout (limit 100 ms) (at " + sleeper + ":1:1)"},
	}
	for _, tt := range tests {
		start := time.Now()
		code, _, stderr := cli(append([]string{"run"}, tt.args...)...)
		if code != 1 || !strings.HasPrefix(stderr, tt.want) {
			t.Errorf("%v: exit %d, stderr %q; want exit 1, stderr starting %q", tt.args, code, stderr, tt.want)
		}
		if d := time.Since(start); d > 4*time.Second {
			t.Errorf("%v took %s", tt.args, d)
		}
	}
	// the same programs run to the end without the flags
	if code, out, stderr := cli("run", countdown); code != 0 || out != "0\n" {
		t.Errorf("countdown without --steps: exit %d, out %q, err %q", code, out, stderr)
	}
	if code, out, stderr := cli("run", grow); code != 0 || out != "5000\n" {
		t.Errorf("grow without --alloc-mb: exit %d, out %q, err %q", code, out, stderr)
	}
}

func TestBudgetFlagsInAnyOrder(t *testing.T) {
	t.Setenv("ARV_GREETING", "hola")
	script := writeScript(t, `puts(getenv("ARV_GREETING"))`)
	code, out, stderr := cli("run", "--steps", "100000", "--allow", "env=ARV_GREETING", "--timeout", "1m", "--tasks", "3", script)
	if code != 0 || out != "hola\n" {
		t.Fatalf("exit %d, out %q, err %q", code, out, stderr)
	}
}

func TestBadBudgetFlags(t *testing.T) {
	script := writeScript(t, "1")
	for _, args := range [][]string{
		{"--steps", "0"}, {"--steps", "many"}, {"--tasks", "-1"}, {"--tasks", "2147483648"},
		{"--timeout", "soon"}, {"--timeout", "-1s"}, {"--alloc-mb", "8796093022208"}, {"--bogus", "1"},
	} {
		code, _, stderr := cli(append(append([]string{"run"}, args...), script)...)
		if code != 2 || !strings.HasPrefix(stderr, "error: "+args[0]) && !strings.HasPrefix(stderr, "error: unknown flag") {
			t.Errorf("%v: exit %d, stderr %q; want exit 2 and an error naming the flag", args, code, stderr)
		}
	}
}

// The run has a 10 minute timeout unless --timeout says otherwise; 0 means
// no timeout.
func TestDefaultTimeout(t *testing.T) {
	opts, file, err := parseRun([]string{"x.arv"}, "/")
	if err != nil || file != "x.arv" || opts.Limits.Timeout != 10*time.Minute {
		t.Fatalf("got %+v, %q, %v; want a 10m timeout", opts.Limits, file, err)
	}
	opts, _, err = parseRun([]string{"--timeout", "0", "x.arv"}, "/")
	if err != nil || opts.Limits.Timeout != 0 {
		t.Fatalf("--timeout 0: got %v, %v; want no timeout", opts.Limits.Timeout, err)
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd <worktree> && go test . ./arv/`

Expected: build failures `undefined: parseRun` (root package) and `unknown field Limits in struct literal of type Options` (arv).

- [ ] **Step 3: Implement**

In `arv/arv.go`, replace

```go

// Options configure one program run.
type Options struct {
	Ctx  context.Context      // defaults to context.Background()
	Caps []runtime.Capability // authority granted to the root task; validated by runtime.New
	Out  io.Writer            // where puts writes; defaults to io.Discard
}

// afterParse, when set (only by tests), runs between parsing and the root
```

with

```go

// Options configure one program run.
type Options struct {
	Ctx    context.Context      // defaults to context.Background()
	Caps   []runtime.Capability // authority granted to the root task; validated by runtime.New
	Out    io.Writer            // where puts writes; defaults to io.Discard
	Limits runtime.Limits       // the run's budget; zero values mean no limit, except LiveTasks (runtime.DefaultLiveTasks)
}

// afterParse, when set (only by tests), runs between parsing and the root
```

In `arv/arv.go`, replace

```go
		afterParse()
	}
	root := runtime.NewRoot(opts.Ctx, runtime.NewCapSet(caps...), opts.Out)
	result = evaluator.New(root).Run(program, value.NewEnvironment())
	if e, ok := result.(*value.Error); ok {
		return result, &Error{Msg: e.Message, Pos: e.Pos}
	}
```

with

```go
		afterParse()
	}
	root := runtime.NewRoot(opts.Ctx, runtime.NewCapSet(caps...), opts.Out)
	result = evaluator.New(root).RunLimited(program, value.NewEnvironment(), opts.Limits)
	if e, ok := result.(*value.Error); ok {
		return result, &Error{Msg: e.Message, Pos: e.Pos}
	}
```


Replace the whole of `main.go` with:

```go
package main

import (
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/delavalom/arvlang/arv"
	"github.com/delavalom/arvlang/arv/repl"
	"github.com/delavalom/arvlang/arv/runtime"
)

const usage = `usage:
  arv                    start the REPL (no capabilities)
  arv run [flags] FILE   run a program

flags:
  --allow kind=scope   grant a capability; repeat for more
                       kinds: net=HOST (or *.domain, *), fs_read=PATH, fs_write=PATH, env=NAME
  --timeout D          stop the run after D, e.g. 30s (default 10m; 0 for no limit)
  --steps N            evaluate at most N nodes in all (default: no limit)
  --tasks N            run at most N tasks at once (default 1000)
  --alloc-mb N         allocate at most N MiB of Arv values in all (default: no limit)`

// defaultTimeout keeps a forgotten script from running forever.
const defaultTimeout = 10 * time.Minute

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
	opts, file, err := parseRun(args[1:], cwd)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 2
	}
	if file == "" {
		fmt.Fprintln(stderr, usage)
		return 2
	}
	src, err := os.ReadFile(file)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	opts.Out = stdout
	if _, err := arv.Run(string(src), opts); err != nil {
		var ae *arv.Error
		if errors.As(err, &ae) && ae.Pos != "" {
			fmt.Fprintf(stderr, "error: %s (at %s:%s)\n", ae.Msg, file, ae.Pos)
			return 1
		}
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	return 0
}

// parseRun reads run's flags, in any order, then the file name. file is ""
// when the arguments do not end in exactly one file.
func parseRun(args []string, cwd string) (opts arv.Options, file string, err error) {
	opts.Limits.Timeout = defaultTimeout
	for len(args) >= 2 && strings.HasPrefix(args[0], "--") {
		flag, val := args[0], args[1]
		args = args[2:]
		var n int64
		switch flag {
		case "--allow":
			c, err := runtime.Parse(val, cwd)
			if err != nil {
				return opts, "", err
			}
			opts.Caps = append(opts.Caps, c)
		case "--timeout":
			d, err := time.ParseDuration(val)
			if err != nil || d < 0 {
				return opts, "", fmt.Errorf("--timeout: want a duration like 30s or 10m, got %q", val)
			}
			opts.Limits.Timeout = d
		case "--steps":
			if n, err = positive(flag, val, math.MaxInt64); err != nil {
				return opts, "", err
			}
			opts.Limits.Steps = n
		case "--tasks":
			if n, err = positive(flag, val, math.MaxInt32); err != nil {
				return opts, "", err
			}
			opts.Limits.LiveTasks = int(n)
		case "--alloc-mb":
			if n, err = positive(flag, val, math.MaxInt64>>20); err != nil {
				return opts, "", err
			}
			opts.Limits.AllocBytes = n << 20
		default:
			return opts, "", fmt.Errorf("unknown flag %q\n%s", flag, usage)
		}
	}
	if len(args) != 1 {
		return opts, "", nil
	}
	return opts, args[0], nil
}

// positive parses val as an integer from 1 to max.
func positive(flag, val string, max int64) (int64, error) {
	n, err := strconv.ParseInt(val, 10, 64)
	if err != nil || n < 1 || n > max {
		return 0, fmt.Errorf("%s: want a whole number from 1 to %d, got %q", flag, max, val)
	}
	return n, nil
}
```

- [ ] **Step 4: Run the tests, then the full checks**

Run: `cd <worktree> && go test . ./arv/`, then `cd <worktree> && gofmt -l . && go vet ./... && go test -race ./...`

Expected: `ok` for every package, and `gofmt -l .` prints nothing.

- [ ] **Step 5: Commit**

```bash
cd <worktree> && git add arv/arv.go arv/arv_test.go main.go main_limits_test.go && git commit -F - <<'MSG'
feat(cli): --timeout, --steps, --tasks and --alloc-mb set the run's budget

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
MSG
```
