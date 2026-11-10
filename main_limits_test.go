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
	short := writeScript(t, "let a = 1;\nlet b = a + 1;\nlet c = b + 1;\nc")
	tests := []struct {
		args []string
		want string // stderr fragment
	}{
		{[]string{"--steps", "1000", countdown}, "error: budget exceeded: steps (limit 1000) (at " + countdown + ":"},
		{[]string{"--tasks", "5", spawner}, "error: budget exceeded: tasks (limit 5 live tasks) (at " + spawner + ":1:48)"},
		{[]string{"--alloc-mb", "1", grow}, "error: budget exceeded: alloc (limit 1048576 bytes) (at " + grow + ":"},
		{[]string{"--steps", "5", short}, "error: budget exceeded: steps (limit 5) (at " + short + ":4:1)"},
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
