# Phase 6, task p6.12: CLI: --record, --replay, --resume, --fsync

> **For agentic workers:** this file is one task, p6.12. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: tasks p6.1 to p6.11 are committed in your tree. Start every shell command with `cd <your worktree> &&`. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify`; keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0` (toolchain 1.26.2), so do not use `testing/synctest` or `sync.WaitGroup.Go`. `arv/runtime` stays stdlib-only.
>
> Every code block below was applied, in task order, to a scratch copy of the tree and passed `go vet ./...` and `go test -race ./...` (including `e2e/`). Design: `13-phase6-durable-replay.md`. Deviations from it, and why: `p6-plan-1.md`.

### Task p6.12: CLI: --record, --replay, --resume, --fsync

`splitLogFlags` takes the log flags out of `run`'s arguments before `parseRun`. Usage errors (exit 2): two of `--record`, `--replay`, `--resume`; `--fsync` without `--record` or `--resume`; `--replay` with `--allow`; `--replay` or `--resume` with a limit flag. `logArgs.setup`:
- `--record` creates the file with mode 0600 (it may hold secrets) and truncates it; `--fsync` syncs after each line;
- `--replay` reads and verifies the log, refuses an unfinished one before printing anything, warns if the script or directory changed, and prints `replay: grants: ...` to stderr;
- `--resume` opens the log read-write, verifies it, cuts a partial last line and appends after it.

**Files:**
- Modify: `main.go`
- Create: `main_log.go`
- Create (test): `main_log_test.go`

- [ ] **Step 1: Write the failing tests**

Create `main_log_test.go`:

```go
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func runArv(args ...string) (int, string, string) {
	var out, errOut bytes.Buffer
	code := runCLI(args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestLogFlagErrors(t *testing.T) {
	for _, tt := range []struct {
		args []string
		want string
	}{
		{[]string{"run", "--record", "a", "--replay", "b", "x.arv"}, "use one of --record, --replay and --resume"},
		{[]string{"run", "--fsync", "x.arv"}, "--fsync needs --record or --resume"},
		{[]string{"run", "--replay", "a", "--allow", "env=HOME", "x.arv"}, "--replay takes its grants from the log"},
		{[]string{"run", "--resume", "a", "--steps", "5", "x.arv"}, "take their limits from the log"},
	} {
		if code, _, stderr := runArv(tt.args...); code != 2 || !strings.Contains(stderr, tt.want) {
			t.Errorf("%v: exit %d, stderr %q; want 2 and %q", tt.args, code, stderr, tt.want)
		}
	}
}

// --record then --replay through the CLI: the same output, without the
// sleep, the grants named on stderr.
func TestRecordThenReplay(t *testing.T) {
	dir := t.TempDir()
	script, log := filepath.Join(dir, "p.arv"), filepath.Join(dir, "run.jsonl")
	src := `let f = fn() { random(1000000) }; sleep(1500); puts(now()); puts(await spawn f()); puts(getenv("HOME"))`
	if err := os.WriteFile(script, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	code, recorded, stderr := runArv("run", "--record", log, "--fsync", "--allow", "env=HOME", script)
	if code != 0 || stderr != "" {
		t.Fatalf("record: exit %d, %q", code, stderr)
	}
	if fi, err := os.Stat(log); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("log: %v, %v", fi, err) // it may hold secrets: getenv's values
	}
	start := time.Now()
	code, replayed, stderr := runArv("run", "--replay", log, script)
	if took := time.Since(start); took >= 1500*time.Millisecond {
		t.Errorf("replay took %s", took)
	}
	if code != 0 || replayed != recorded || !strings.Contains(stderr, `replay: grants: env("HOME")`) {
		t.Fatalf("replay: exit %d, printed %q (recorded %q), stderr %q", code, replayed, recorded, stderr)
	}
	os.WriteFile(script, []byte(src+"; puts(1)"), 0o644)
	code, _, stderr = runArv("run", "--replay", log, script)
	if code != 1 || !strings.Contains(stderr, "has changed since the log was recorded") || !strings.Contains(stderr, "replay diverged") {
		t.Fatalf("edited script: exit %d, %q", code, stderr)
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd <worktree> && go test -race . -run 'TestLogFlagErrors|TestRecordThenReplay'`

Expected: both fail: `error: unknown flag "--record"` (exit 2).

- [ ] **Step 3: Implement**

In `main.go`, replace:

```go
  --steps N            evaluate at most N nodes in all (default: no limit)
  --tasks N            at most N tasks alive at once; one more is an error (default 1000)
  --alloc-mb N         allocate at most N MiB of Arv values in all (default: no limit)`

// defaultTimeout keeps a forgotten script from running forever.
```

with:

```go
  --steps N            evaluate at most N nodes in all (default: no limit)
  --tasks N            at most N tasks alive at once; one more is an error (default 1000)
  --alloc-mb N         allocate at most N MiB of Arv values in all (default: no limit)
  --record LOG         write every effect to LOG (JSON Lines) as the run goes
  --fsync              with --record or --resume: fsync LOG after every effect
  --replay LOG         run again with every effect answered from LOG: nothing
                       live; the grants and limits come from LOG (no --allow)
  --resume LOG         answer effects from LOG while it has them, then run live
                       and append to LOG; give the grants the run had`

// defaultTimeout keeps a forgotten script from running forever.
```

In `main.go`, replace:

```go
		return 1
	}
	opts, file, err := parseRun(args[1:], cwd)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
```

with:

```go
		return 1
	}
	la, rest, err := splitLogFlags(args[1:])
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 2
	}
	opts, file, err := parseRun(rest, cwd)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
```

In `main.go`, replace:

```go
	}
	opts.Out = stdout
	if _, err := arv.Run(string(src), opts); err != nil {
		var ae *arv.Error
		if errors.As(err, &ae) && ae.Pos != "" {
```

with:

```go
	}
	opts.Out = stdout
	closeLog, err := la.setup(&opts, string(src), file, stderr)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	_, err = arv.Run(string(src), opts)
	if cerr := closeLog(); err == nil && cerr != nil {
		err = cerr
	}
	if err != nil {
		var ae *arv.Error
		if errors.As(err, &ae) && ae.Pos != "" {
```

Create `main_log.go`:

```go
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/delavalom/arvlang/arv"
	"github.com/delavalom/arvlang/arv/runtime"
)

// logArgs are run's effect log flags (Phase 6).
type logArgs struct {
	record, replay, resume string // a log file each; at most one set
	fsync                  bool
	allow, limits          bool // whether --allow, or a limit flag, was given
}

var limitFlags = map[string]bool{"--timeout": true, "--steps": true, "--tasks": true, "--alloc-mb": true}

// splitLogFlags takes the log flags out of run's arguments, leaving the
// rest for parseRun. --fsync takes no value; the others take a file.
func splitLogFlags(args []string) (logArgs, []string, error) {
	var la logArgs
	var rest []string
	for len(args) > 0 && strings.HasPrefix(args[0], "--") {
		flag := args[0]
		if flag == "--fsync" {
			la.fsync = true
			args = args[1:]
			continue
		}
		if len(args) < 2 {
			break
		}
		switch flag {
		case "--record":
			la.record = args[1]
		case "--replay":
			la.replay = args[1]
		case "--resume":
			la.resume = args[1]
		default:
			la.allow = la.allow || flag == "--allow"
			la.limits = la.limits || limitFlags[flag]
			rest = append(rest, args[0], args[1])
		}
		args = args[2:]
	}
	n := 0
	for _, f := range []string{la.record, la.replay, la.resume} {
		if f != "" {
			n++
		}
	}
	switch {
	case n > 1:
		return la, nil, errors.New("use one of --record, --replay and --resume")
	case la.fsync && la.record == "" && la.resume == "":
		return la, nil, errors.New("--fsync needs --record or --resume")
	case la.replay != "" && la.allow:
		return la, nil, errors.New("--replay takes its grants from the log; drop --allow")
	case (la.replay != "" || la.resume != "") && la.limits:
		return la, nil, errors.New("--replay and --resume take their limits from the log; drop the limit flags")
	}
	return la, append(rest, args...), nil
}

// setup opens the log la names and sets opts to use it. It returns what
// to close once the run is over.
func (la logArgs) setup(opts *arv.Options, src, file string, stderr io.Writer) (func() error, error) {
	switch {
	case la.record != "":
		f, err := os.OpenFile(la.record, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
		if err != nil {
			return nil, err
		}
		opts.Record = f
		if la.fsync {
			opts.RecordSync = f.Sync
		}
		return f.Close, nil
	case la.replay != "":
		f, err := os.Open(la.replay)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		l, err := runtime.ReadLog(f)
		if err == nil && l.End == nil {
			err = runtime.ErrUnfinished
		}
		if err != nil {
			return nil, err
		}
		opts.Replay, opts.Limits = l, runtime.Limits{}
		warn(stderr, l.Header, src, file)
		fmt.Fprintf(stderr, "replay: grants: %s\n", grants(l.Header))
		return func() error { return nil }, nil
	case la.resume != "":
		f, err := os.OpenFile(la.resume, os.O_RDWR, 0)
		if err != nil {
			return nil, err
		}
		l, err := runtime.ReadLog(f)
		if err == nil {
			err = f.Truncate(l.Size) // a partial last line: the crash cut it off
		}
		if err == nil {
			_, err = f.Seek(l.Size, io.SeekStart)
		}
		if err != nil {
			f.Close()
			return nil, err
		}
		opts.Resume, opts.Record, opts.Limits = l, f, runtime.Limits{}
		if la.fsync {
			opts.RecordSync = f.Sync
		}
		warn(stderr, l.Header, src, file)
		return f.Close, nil
	}
	return func() error { return nil }, nil
}

// warn says when the script or the directory is not the recorded run's:
// the run goes on, and a divergence will name what differs.
func warn(stderr io.Writer, h runtime.Header, src, file string) {
	if h.ScriptSHA256 != arv.ScriptSHA256(src) {
		fmt.Fprintf(stderr, "warning: %s has changed since the log was recorded\n", file)
	}
	if cwd, err := os.Getwd(); err == nil && h.Cwd != "" && cwd != h.Cwd {
		fmt.Fprintf(stderr, "warning: the log was recorded in %s; relative paths resolve against %s\n", h.Cwd, cwd)
	}
}

func grants(h runtime.Header) string {
	if len(h.Grants) == 0 {
		return "none"
	}
	s := make([]string, len(h.Grants))
	for i, g := range h.Grants {
		s[i] = runtime.Capability{Kind: g.Kind, Scope: g.Scope}.String()
	}
	return strings.Join(s, ", ")
}
```

- [ ] **Step 4: Run the tests, then the full checks**

Run: `cd <worktree> && go test -race . -run 'TestLogFlagErrors|TestRecordThenReplay'`, then `cd <worktree> && gofmt -l . && go vet ./... && go test -race ./...`

Expected: `ok` for every package, and `gofmt -l .` prints nothing.

- [ ] **Step 5: Commit**

```bash
cd <worktree> && git add main.go main_log.go main_log_test.go && git commit -F - <<'MSG'
feat(cli): run --record, --replay, --resume and --fsync

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
MSG
```
