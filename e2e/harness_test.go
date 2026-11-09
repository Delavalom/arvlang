// Package e2e holds black-box tests for the arv CLI: build the real binary,
// run real .arv scripts, and assert stdout, stderr and the exit code.
//
// The suite is hermetic. HTTP goes to a local httptest server, files live in
// per-test temp dirs, and every child process gets an explicit environment
// (nothing is inherited from the user's shell).
package e2e

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// Bodies served by the test server.
const (
	okBody       = "hello from ok"
	notFoundBody = "nothing here"
	targetBody   = "redirect target reached"
)

var (
	arvBin string           // the binary under test
	srv    *httptest.Server // local HTTP server on 127.0.0.1

	hitsMu sync.Mutex
	hitLog = map[string]int{}         // "path?id" -> requests received
	marks  = map[string][]time.Time{} // id -> arrival times of /mark requests
)

func TestMain(m *testing.M) { os.Exit(runMain(m)) }

func runMain(m *testing.M) int {
	dir, err := os.MkdirTemp("", "arv-e2e-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "e2e: temp dir:", err)
		return 1
	}
	defer os.RemoveAll(dir)

	arvBin = filepath.Join(dir, "arv")
	build := exec.Command("go", "build", "-o", arvBin, "github.com/delavalom/arvlang")
	if out, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "e2e: FAILED to build the arv binary (%s): %v\n%s\n",
			strings.Join(build.Args, " "), err, out)
		return 1
	}

	srv = newServer()
	defer srv.Close()
	return m.Run()
}

// newServer starts the local HTTP server. Every handler records the request
// so tests can prove a request did, or did not, reach the server. Requests
// carry ?id=<test name> so parallel tests never share a counter.
//
//	/ok            200 okBody
//	/status/404    404 notFoundBody
//	/redirect-out  302 to http://localhost:PORT/redirect-target (same server,
//	               different hostname, so the net capability is re-checked)
//	/redirect-target 200 targetBody
//	/mark          200, records the arrival time (to time a script from outside)
func newServer() *httptest.Server {
	mux := http.NewServeMux()
	var port string
	mux.HandleFunc("/ok", func(w http.ResponseWriter, r *http.Request) {
		record(r)
		io.WriteString(w, okBody)
	})
	mux.HandleFunc("/status/404", func(w http.ResponseWriter, r *http.Request) {
		record(r)
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, notFoundBody)
	})
	mux.HandleFunc("/redirect-out", func(w http.ResponseWriter, r *http.Request) {
		record(r)
		to := "http://localhost:" + port + "/redirect-target?" + r.URL.RawQuery
		http.Redirect(w, r, to, http.StatusFound)
	})
	mux.HandleFunc("/redirect-target", func(w http.ResponseWriter, r *http.Request) {
		record(r)
		io.WriteString(w, targetBody)
	})
	mux.HandleFunc("/mark", func(w http.ResponseWriter, r *http.Request) {
		record(r)
		hitsMu.Lock()
		id := r.URL.Query().Get("id")
		marks[id] = append(marks[id], time.Now())
		hitsMu.Unlock()
	})
	s := httptest.NewServer(mux) // listens on 127.0.0.1:0
	port = strconv.Itoa(s.Listener.Addr().(*net.TCPAddr).Port)
	return s
}

func record(r *http.Request) {
	hitsMu.Lock()
	defer hitsMu.Unlock()
	hitLog[r.URL.Path+"?"+r.URL.Query().Get("id")]++
}

// hits reports how many requests for path carried this test's id.
func hits(t *testing.T, path string) int {
	id := testID(t)
	hitsMu.Lock()
	defer hitsMu.Unlock()
	return hitLog[path+"?"+id]
}

// markTimes returns when the server received this test's /mark requests.
func markTimes(t *testing.T) []time.Time {
	id := testID(t)
	hitsMu.Lock()
	defer hitsMu.Unlock()
	return append([]time.Time(nil), marks[id]...)
}

var (
	idsMu sync.Mutex
	ids   = map[*testing.T]string{}
)

// testID is a token unique to this test run (also across -count=N reruns of
// the same test) that is safe inside a URL query and an Arv string.
func testID(t *testing.T) string {
	idsMu.Lock()
	defer idsMu.Unlock()
	if id, ok := ids[t]; ok {
		return id
	}
	id := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			return r
		}
		return '_'
	}, t.Name()) + "_" + strconv.Itoa(len(ids))
	ids[t] = id
	return id
}

// spec describes one CLI run and what it must produce.
type spec struct {
	name   string
	script string            // file under testdata/; "" runs without a script
	vars   map[string]string // {{KEY}} -> value, applied to script, allow and args
	allow  []string          // each becomes "--allow X"
	args   []string          // full CLI args instead of run/--allow/script; may use {{SCRIPT}}
	env    []string          // the child's entire environment, KEY=VALUE
	dir    string            // child working dir; defaults to a fresh temp dir

	code      int      // expected exit code
	stdout    string   // expected stdout, matched exactly
	stderrHas []string // substrings stderr must contain (code 0 requires empty stderr)
	stderrNot []string // substrings stderr must not contain
	stdoutNot []string // substrings stdout must not contain (e.g. leaked secrets)

	verify func(t *testing.T) // extra checks after the run, e.g. file contents
}

// result is what one run of the binary produced.
type result struct {
	cmd      string
	stdout   string
	stderr   string
	code     int
	duration time.Duration
}

func (r result) String() string {
	return fmt.Sprintf("command: %s\nexit code: %d\nduration: %s\n--- stdout ---\n%s--- stderr ---\n%s",
		r.cmd, r.code, r.duration, r.stdout, r.stderr)
}

// check runs s and fails the test (with the full run report) on any mismatch.
func check(t *testing.T, s spec) result {
	t.Helper()
	res := run(t, s)
	expect(t, s, res)
	return res
}

// expect checks an existing result against s; see check.
func expect(t *testing.T, s spec, res result) {
	t.Helper()
	var problems []string
	if res.code != s.code {
		problems = append(problems, fmt.Sprintf("exit code = %d, want %d", res.code, s.code))
	}
	if res.stdout != s.stdout {
		problems = append(problems, fmt.Sprintf("stdout = %q, want %q", res.stdout, s.stdout))
	}
	if s.code == 0 && res.stderr != "" {
		problems = append(problems, "stderr not empty on success")
	}
	if s.code != 0 && !strings.HasPrefix(res.stderr, "error: ") && !strings.HasPrefix(res.stderr, "usage:") {
		problems = append(problems, `stderr starts with neither "error: " nor "usage:"`)
	}
	for _, want := range s.stderrHas {
		if !strings.Contains(res.stderr, expand(t, want, s.vars)) {
			problems = append(problems, fmt.Sprintf("stderr lacks %q", expand(t, want, s.vars)))
		}
	}
	for _, bad := range s.stderrNot {
		if strings.Contains(res.stderr, bad) {
			problems = append(problems, fmt.Sprintf("stderr contains %q", bad))
		}
	}
	for _, bad := range s.stdoutNot {
		if strings.Contains(res.stdout, bad) {
			problems = append(problems, fmt.Sprintf("stdout contains %q", bad))
		}
	}
	if len(problems) > 0 {
		t.Fatalf("%s\n%s", strings.Join(problems, "\n"), res)
	}
	if s.verify != nil {
		s.verify(t)
	}
}

// run materialises the script, runs the binary and captures its output.
// It fails the test only if the process cannot be run at all.
func run(t *testing.T, s spec) result {
	t.Helper()
	work := t.TempDir()
	scriptPath := ""
	if s.script != "" {
		src, err := os.ReadFile(filepath.Join("testdata", s.script))
		if err != nil {
			t.Fatalf("reading testdata/%s: %v", s.script, err)
		}
		scriptPath = filepath.Join(work, s.script)
		if err := os.WriteFile(scriptPath, []byte(expand(t, string(src), s.vars)), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	var args []string
	if s.args != nil {
		vars := map[string]string{"SCRIPT": scriptPath}
		for k, v := range s.vars {
			vars[k] = v
		}
		for _, a := range s.args {
			args = append(args, expand(t, a, vars))
		}
	} else {
		args = []string{"run"}
		for _, a := range s.allow {
			args = append(args, "--allow", expand(t, a, s.vars))
		}
		args = append(args, scriptPath)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, arvBin, args...)
	cmd.Env = append([]string{}, s.env...) // non-nil: never inherit the user's env
	cmd.Dir = work
	if s.dir != "" {
		cmd.Dir = s.dir
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	start := time.Now()
	err := cmd.Run()
	res := result{
		cmd:      describe(cmd),
		stdout:   stdout.String(),
		stderr:   stderr.String(),
		duration: time.Since(start),
	}
	var exitErr *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exitErr) && exitErr.Exited():
		res.code = exitErr.ExitCode()
	default:
		t.Fatalf("running arv: %v (ctx: %v)\n%s", err, ctx.Err(), res)
	}
	return res
}

// expand replaces {{KEY}} placeholders. {{SERVER}} and {{ID}} are always set.
// A leftover placeholder is a bug in the test, so it fails loudly.
func expand(t *testing.T, s string, vars map[string]string) string {
	t.Helper()
	pairs := []string{"{{SERVER}}", srv.URL, "{{ID}}", testID(t)}
	for k, v := range vars {
		pairs = append(pairs, "{{"+k+"}}", v)
	}
	out := strings.NewReplacer(pairs...).Replace(s)
	if i := strings.Index(out, "{{"); i >= 0 {
		t.Fatalf("unexpanded placeholder in %q", out)
	}
	return out
}

// describe renders the command as a copy-pasteable shell line.
func describe(cmd *exec.Cmd) string {
	parts := []string{"cd", shellQuote(cmd.Dir), "&&", "env", "-i"}
	for _, e := range cmd.Env {
		parts = append(parts, shellQuote(e))
	}
	for _, a := range cmd.Args {
		parts = append(parts, shellQuote(a))
	}
	return strings.Join(parts, " ")
}

func shellQuote(s string) string {
	if s != "" && strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_./=:*") == "" {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// runAll runs each spec as a parallel subtest.
func runAll(t *testing.T, specs []spec) {
	t.Helper()
	for _, s := range specs {
		t.Run(s.name, func(t *testing.T) {
			t.Parallel()
			check(t, s)
		})
	}
}

// mustWrite creates path (and its parents) with content.
func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustSymlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}

// assertAbsent fails if path exists (checked without following symlinks).
func assertAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("%s should not exist (lstat err: %v)", path, err)
	}
}

func assertFile(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	if string(got) != want {
		t.Fatalf("%s = %q, want %q", path, got, want)
	}
}
