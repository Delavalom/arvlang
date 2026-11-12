package e2e

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// Phase 8.2 e2e: host effects exist only for Go hosts, so these tests
// build e2e/p82host (a host whose effects each test one guarantee) and run
// it as a process, the way the arv binary is run elsewhere in this suite.

var (
	p82Once sync.Once
	p82Bin  string
	p82Err  error
)

// p82Host builds e2e/p82host once (with buildFlags), next to
// the arv binary so TestMain's cleanup removes it.
func p82Host(t *testing.T) string {
	t.Helper()
	p82Once.Do(func() {
		p82Bin = filepath.Join(filepath.Dir(arvBin), "p82host")
		args := append([]string{"build", "-o", p82Bin}, buildFlags()...)
		cmd := exec.Command("go", append(args, "github.com/delavalom/arvlang/e2e/p82host")...)
		if out, err := cmd.CombinedOutput(); err != nil {
			p82Err = fmt.Errorf("building e2e/p82host: %v\n%s", err, out)
		}
	})
	if p82Err != nil {
		t.Fatal(p82Err)
	}
	return p82Bin
}

// hostSpec is one run of p82host and what it must produce.
type hostSpec struct {
	name  string
	src   string   // the Arv program; {{SERVER}} and {{ID}} are expanded
	caps  []string // each becomes -cap KIND=SCOPE
	flags []string // more flags (-steps, -alloc-mb, -timeout-ms, -setup)
	gates bool     // pass -server and -id, for effects that call the server

	code      int      // expected exit code
	stdout    string   // expected stdout, exactly
	errHas    []string // substrings stderr must contain
	errNot    []string // substrings neither stdout nor stderr may contain
	kind      string   // the error's kind ("kind: X" line); "" on success
	calls     []string // the Fn call log, sorted, exactly; nil means none
	anyCalls  bool     // skip the exact call check (verify checks it)
	verify    func(t *testing.T, r hostResult)
	timeout   time.Duration // kill the host after this; default 2 minutes
	maxRunFor time.Duration // the run must end within this (generous)
}

// hostResult is what one run of p82host produced.
type hostResult struct {
	result
	calls []string // the Fn call log, sorted
}

func runHost(t *testing.T, s hostSpec) hostResult {
	t.Helper()
	bin := p82Host(t)
	work := t.TempDir()
	script := filepath.Join(work, "main.arv")
	if err := os.WriteFile(script, []byte(expand(t, s.src, nil)), 0o644); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(work, "calls.log")
	args := []string{"-log", logPath}
	for _, c := range s.caps {
		args = append(args, "-cap", c)
	}
	if s.gates {
		args = append(args, "-server", srv.URL, "-id", testID(t))
	}
	args = append(append(args, s.flags...), script)
	timeout := s.timeout
	if timeout == 0 {
		timeout = 2 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = []string{} // never inherit the user's env
	cmd.Dir = work
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	start := time.Now()
	err := cmd.Run()
	res := hostResult{result: result{cmd: describe(cmd), stdout: stdout.String(), stderr: stderr.String(), duration: time.Since(start)}}
	var exitErr *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exitErr) && exitErr.Exited():
		res.code = exitErr.ExitCode()
	default:
		t.Fatalf("running p82host: %v (ctx: %v)\n%s", err, ctx.Err(), res.result)
	}
	if b, err := os.ReadFile(logPath); err == nil && len(b) > 0 {
		res.calls = strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
		slices.Sort(res.calls)
	}
	return res
}

// checkHost runs s and fails the test, with the full report, on any
// mismatch. Exit code 3 (a goroutine outlived arv.Run) always fails.
func checkHost(t *testing.T, s hostSpec) hostResult {
	t.Helper()
	r := runHost(t, s)
	var problems []string
	if r.code != s.code {
		problems = append(problems, fmt.Sprintf("exit code = %d, want %d", r.code, s.code))
	}
	if r.stdout != s.stdout {
		problems = append(problems, fmt.Sprintf("stdout = %q, want %q", clip(r.stdout), s.stdout))
	}
	if s.code == 0 && r.stderr != "" {
		problems = append(problems, "stderr not empty on success")
	}
	if s.kind != "" && !strings.Contains(r.stderr, "\nkind: "+s.kind+"\n") {
		problems = append(problems, fmt.Sprintf("stderr lacks kind %q", s.kind))
	}
	for _, want := range s.errHas {
		if !strings.Contains(r.stderr, want) {
			problems = append(problems, fmt.Sprintf("stderr lacks %q", want))
		}
	}
	for _, bad := range s.errNot {
		if strings.Contains(r.stderr, bad) || strings.Contains(r.stdout, bad) {
			problems = append(problems, fmt.Sprintf("output contains %q", bad))
		}
	}
	if strings.Contains(r.stderr, "goroutine ") || strings.Contains(r.stderr, "panic:") {
		problems = append(problems, "stderr holds a Go panic or a stack dump")
	}
	if !s.anyCalls {
		want := slices.Clone(s.calls)
		slices.Sort(want)
		if !slices.Equal(r.calls, want) {
			problems = append(problems, fmt.Sprintf("Fn calls = %q, want %q", r.calls, want))
		}
	}
	if s.maxRunFor > 0 && r.duration > s.maxRunFor {
		problems = append(problems, fmt.Sprintf("ran %s, want at most %s", r.duration, s.maxRunFor))
	}
	if len(problems) > 0 {
		t.Fatalf("%s\n%s\n--- calls ---\n%s", strings.Join(problems, "\n"), r.result, strings.Join(r.calls, "\n"))
	}
	if s.verify != nil {
		s.verify(t, r)
	}
	return r
}

// runHosts runs each spec as a parallel subtest.
func runHosts(t *testing.T, specs []hostSpec) {
	t.Helper()
	for _, s := range specs {
		t.Run(s.name, func(t *testing.T) {
			t.Parallel()
			checkHost(t, s)
		})
	}
}
