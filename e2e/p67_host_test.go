package e2e

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Phase 8.2 host effects through the Phase 6 log, with a real Go host
// (e2e/p82host): recorded, replayed with no Fn called, and resumed after
// SIGKILL with only the call that was in flight made again.

// p67HostSrc calls host effects from the root and from a child handed
// k8s("staging") (a replayed spawn of a host kind's grant), then waits
// at the server's gate {{K}} in one more.
const p67HostSrc = `puts(k8s_apply("staging", "web.yaml")["applied"]);
let t = spawn fn() { k8s_apply("staging", "api.yaml")["applied"] }() with [k8s("staging")];
puts(await t);
puts(team_read("a/b"));
puts(k8s_gate("staging", "{{K}}"));
puts("done")
`

var p67HostCaps = []string{"k8s=staging", "team=a"}

const p67HostOut = "web.yaml\napi.yaml\nread a/b\nopen\ndone\n"

// Record, then replay with no grants: the same output, and no Fn runs.
func TestP67HostRecordAndReplay(t *testing.T) {
	t.Parallel()
	log := filepath.Join(t.TempDir(), "run.jsonl")
	arrive(testID(t), "open")
	src := strings.ReplaceAll(p67HostSrc, "{{K}}", "open")
	checkHost(t, hostSpec{src: src, caps: p67HostCaps, gates: true, flags: []string{"-record", log}, stdout: p67HostOut,
		calls: []string{"k8s_apply(staging) task=root", "k8s_apply(staging) task=root/0", "team_read(a/b) task=root", "k8s_gate(staging) task=root"}})
	checkHost(t, hostSpec{src: src, gates: true, flags: []string{"-replay", log}, stdout: p67HostOut})
	if n := hits(t, "/wait"); n != 1 {
		t.Fatalf("the gate saw %d requests, want the recording's 1", n)
	}
}

// SIGKILL while k8s_gate waits, then -resume: the calls the log holds are
// not made again; the one in flight is, and the output is the
// uninterrupted run's.
func TestP67HostResumeAfterSIGKILL(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	log, script := filepath.Join(dir, "run.jsonl"), filepath.Join(dir, "main.arv")
	mustWrite(t, script, expand(t, p67HostSrc, map[string]string{"K": "shut"}))
	calls := filepath.Join(dir, "calls.log")
	args := []string{"-log", calls, "-server", srv.URL, "-id", testID(t), "-record", log}
	for _, c := range p67HostCaps {
		args = append(args, "-cap", c)
	}
	cmd := exec.Command(p82Host(t), append(args, script)...)
	cmd.Env, cmd.Dir = []string{}, dir
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waitHits(t, "/wait", 1)
	cmd.Process.Kill()
	cmd.Wait()
	if out.String() != "web.yaml\napi.yaml\nread a/b\n" {
		t.Fatalf("before the kill: %q", out.String())
	}
	lines := readLogLines(t, log)
	if kinds(lines, "host:k8s_apply") != 2 || kinds(lines, "host:team_read") != 1 || kinds(lines, "host:k8s_gate") != 0 {
		t.Fatalf("the killed run's log:\n%v", lines)
	}
	if err := os.Remove(calls); err != nil {
		t.Fatal(err)
	}
	arrive(testID(t), "shut")
	src, err := os.ReadFile(script)
	if err != nil {
		t.Fatal(err)
	}
	checkHost(t, hostSpec{src: string(src), caps: p67HostCaps, gates: true, flags: []string{"-resume", log},
		stdout: p67HostOut, calls: []string{"k8s_gate(staging) task=root"}, timeout: 90 * time.Second})
	checkHost(t, hostSpec{src: string(src), gates: true, flags: []string{"-replay", log}, stdout: p67HostOut})
	if n := hits(t, "/wait"); n != 2 {
		t.Fatalf("the gate saw %d requests, want 2 (the killed run's and the resume's)", n)
	}
}

// p82host refuses flags its log mode ignores, as the arv CLI does: a
// replay takes its grants and limits from the log, a resume its limits,
// and the three log flags exclude each other. Nothing runs.
func TestP67HostRefusesFlagsTheLogOverrides(t *testing.T) {
	t.Parallel()
	log := filepath.Join(t.TempDir(), "run.jsonl")
	checkHost(t, hostSpec{src: `puts("hi")`, caps: []string{"team=a"}, flags: []string{"-record", log}, stdout: "hi\n"})
	var specs []hostSpec
	for _, f := range [][]string{{"-steps", "5"}, {"-alloc-mb", "5"}, {"-timeout-ms", "5000"}} {
		specs = append(specs,
			hostSpec{name: "replay " + f[0], src: `puts("hi")`, flags: append([]string{"-replay", log}, f...), code: 2,
				errHas: []string{"takes its grants and limits from the log", f[0]}},
			hostSpec{name: "resume " + f[0], src: `puts("hi")`, caps: []string{"team=a"}, flags: append([]string{"-resume", log}, f...), code: 2,
				errHas: []string{"takes its limits from the log", f[0]}})
	}
	specs = append(specs,
		hostSpec{name: "replay -cap", src: `puts("hi")`, caps: []string{"team=a"}, flags: []string{"-replay", log}, code: 2,
			errHas: []string{"takes its grants and limits from the log", "-cap"}},
		hostSpec{name: "record and replay", src: `puts("hi")`, flags: []string{"-record", log + ".2", "-replay", log}, code: 2,
			errHas: []string{"-record, -replay and -resume exclude each other"}})
	runHosts(t, specs)
}
