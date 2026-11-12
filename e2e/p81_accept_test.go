package e2e

import (
	"net"
	"path/filepath"
	"strings"
	"testing"
)

// Phase 8.1 hardening: arv check through the real binary, on every syntax
// form, on what it cannot know, and on misuse. p81_check_test.go holds the
// examples/ acceptance test; p81_manifest_test.go holds run --manifest.

// serverPort is the test server's port.
func serverPort(t *testing.T) string {
	_, port, err := net.SplitHostPort(serverHost(t))
	if err != nil {
		t.Fatal(err)
	}
	return port
}

// Every syntax form is searched, including code that never runs, and
// none of it runs: the server sees nothing, no file appears, and the
// program's own output never shows.
func TestP81CheckSearchesEverySyntaxForm(t *testing.T) {
	host, dir := serverHost(t), t.TempDir()
	check(t, spec{
		name: "forms", script: "p81_forms.arv", args: []string{"check", "{{SCRIPT}}"}, dir: dir,
		vars: map[string]string{"HOST": host},
		stdout: "--allow net=" + host + "\n" +
			"--allow fs_read=in/for.txt\n--allow fs_read=in/scope.txt\n" +
			"--allow fs_write=out\n--allow fs_write=out/ok.txt\n" +
			"--allow env=ARV_P81_DEAD\n--allow env=ARV_P81_ERR_ARM\n",
		stdoutNot: []string{"ran"},
		verify: func(t *testing.T) {
			if n := hits(t, "/ok"); n != 0 {
				t.Errorf("the server saw %d requests", n)
			}
			assertAbsent(t, filepath.Join(dir, "out"))
		},
	})
}

// What arv check cannot know is a note, never a silent omission: an
// argument known only at run time, a builtin used as a value, a call the
// runtime refuses before admitting anything. Folded constants still count.
func TestP81CheckNotes(t *testing.T) {
	check(t, spec{
		name: "notes", script: "p81_notes.arv", args: []string{"check", "{{SCRIPT}}"},
		stdout: "--allow net=api.github.com\n--allow env=AB\n" +
			"# http_get at 2:19: host not statically known\n" +
			"# read_file at 3:10: used as a value: what it needs is not statically known\n" +
			"# write_file at 5:1: never allowed: 3 arguments (it takes 2)\n",
	})
}

// Misuse: usage errors exit 2, a missing or unparsable file exits 1 with
// the same message run gives, and nothing is printed on stdout.
func TestP81CheckMisuse(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.arv")
	mustWrite(t, bad, "let x = ;")
	parse := "parse error: no prefix parse function for ; found (at " + bad + ":1:9)"
	runAll(t, []spec{
		{name: "no file", args: []string{"check"}, code: 2, stderrHas: []string{"arv check FILE"}},
		{name: "two files", args: []string{"check", bad, bad}, code: 2, stderrHas: []string{"arv check FILE"}},
		{name: "a flag", args: []string{"check", "--allow", bad}, code: 2, stderrHas: []string{"arv check FILE"}},
		{name: "missing", args: []string{"check", filepath.Join(dir, "nope.arv")}, code: 1, stderrHas: []string{"no such file"}},
		{name: "parse error", args: []string{"check", bad}, code: 1, stderrHas: []string{parse}},
		{name: "run says the same", args: []string{"run", bad}, code: 1, stderrHas: []string{parse}},
		{name: "run --manifest says the same", args: []string{"run", "--manifest", bad}, code: 1, stderrHas: []string{parse}},
	})
}

// Acceptance, as a round trip: the printed lines, passed to arv run as
// they are, run the program; leaving any one of them out is denied before
// the denied effect happens (I1: the run holds exactly what it is given).
func TestP81CheckOutputRunsTheProgram(t *testing.T) {
	host := serverHost(t)
	vars := map[string]string{"HOST": host}
	env := []string{"ARV_P81_VAR=!"}
	res := check(t, spec{
		name: "check", script: "p81_roundtrip.arv", args: []string{"check", "{{SCRIPT}}"}, vars: vars,
		stdout: "--allow net=" + host + "\n--allow fs_read=in.txt\n--allow fs_write=out.txt\n--allow env=ARV_P81_VAR\n",
	})
	lines := strings.Split(strings.TrimSuffix(res.stdout, "\n"), "\n")
	argsWith := func(keep func(i int) bool) []string {
		args := []string{"run"}
		for i, l := range lines {
			if keep(i) {
				args = append(args, strings.Fields(l)...)
			}
		}
		return append(args, "{{SCRIPT}}")
	}
	t.Run("all", func(t *testing.T) {
		dir := t.TempDir()
		mustWrite(t, filepath.Join(dir, "in.txt"), "data")
		check(t, spec{
			name: "run", script: "p81_roundtrip.arv", args: argsWith(func(int) bool { return true }),
			vars: vars, env: env, dir: dir, stdout: "200\n",
			verify: func(t *testing.T) { assertFile(t, filepath.Join(dir, "out.txt"), "data!") },
		})
	})
	for drop, l := range lines {
		t.Run("without "+strings.TrimPrefix(l, "--allow "), func(t *testing.T) {
			dir := t.TempDir()
			mustWrite(t, filepath.Join(dir, "in.txt"), "data")
			check(t, spec{
				name: "run", script: "p81_roundtrip.arv", args: argsWith(func(i int) bool { return i != drop }),
				vars: vars, env: env, dir: dir, code: 1,
				stderrHas: []string{"capability denied"},
				verify:    func(t *testing.T) { assertAbsent(t, filepath.Join(dir, "out.txt")) },
			})
		})
	}
}

// Secrets stay in the host: arv check names the variable a program reads
// and never its value, even when the environment holds it.
func TestP81CheckNeverReadsTheEnvironment(t *testing.T) {
	const secret = "s3cr3t-p81-value"
	check(t, spec{
		name: "secret", script: "p81_secret.arv", args: []string{"check", "{{SCRIPT}}"},
		env:    []string{"ARV_P81_SECRET=" + secret},
		stdout: "--allow env=ARV_P81_SECRET\n", stdoutNot: []string{secret}, stderrNot: []string{secret},
	})
}

// A program that would sleep, spin forever or wait on the network is
// checked at once: arv check never evaluates it, so its time is the
// parse and walk alone, whatever the program would do when run.
func TestP81CheckIsQuickOnProgramsThatNeverEnd(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "forever.arv")
	mustWrite(t, script, "let spin = fn() { spin() };\nsleep(600000);\n"+
		`http_get("`+srv.URL+`/wait?k=never&n=1&id=`+testID(t)+`");`+"\nspin();\n")
	res := check(t, spec{
		name: "forever", args: []string{"check", script},
		stdout: "--allow net=" + serverHost(t) + "\n",
	})
	if hits(t, "/wait") != 0 {
		t.Errorf("the server saw a request")
	}
	if res.duration > gateTimeout {
		t.Errorf("check took %s", res.duration)
	}
}
