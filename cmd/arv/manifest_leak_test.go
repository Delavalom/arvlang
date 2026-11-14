package main

import (
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"
	"time"
)

// Leak rule for Phase 8.1: no goroutine survives "arv run --manifest" or
// "arv check", on the paths that end a run early: a manifest refusal, a
// runtime denial of a need arv check could not see (with sibling tasks
// asleep), a spawn whose capability is outside the header, a supervised
// restart, a timeout.
func TestManifestRunsLeaveNoGoroutine(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile(filepath.Join(dir, "in.txt"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ARV_P81_LEAK", "v")
	const nap = "let nap = fn() { sleep(600000) };\n"
	cases := []struct {
		name, src string
		code      int
		stderr    string
	}{
		{"refused before running",
			"// arv:allow env=ARV_P81_LEAK\n" + nap + "spawn nap(); read_file(\"in.txt\");",
			1, "outside its arv:allow manifest"},
		{"note denied at run time while siblings sleep",
			"// arv:allow fs_read=in.txt\n" + nap + "let rd = fn(p) { read_file(p) };\n" +
				"scope { spawn nap(); spawn nap(); await spawn rd(\"other.txt\") with [fs_read(\"in.txt\")] }",
			1, "capability denied"},
		{"spawn outside the header while siblings sleep",
			"// arv:allow env=ARV_P81_LEAK\n" + nap + "let mk = fn(s) { env(s) }; let f = fn() { 1 };\n" +
				"scope { spawn nap(); await spawn f() with [mk(\"HOME\")] }",
			1, "capability denied"},
		{"supervised restarts under the header",
			"// arv:allow fs_read=in.txt\nlet rd = fn() { read_file(\"in.txt\") / 0 };\n" +
				"scope (supervise: \"restart\", retries: 2, backoff_ms: 1) { await spawn rd() with [fs_read(\"in.txt\")] }",
			1, ""},
		{"collect under the header",
			"// arv:allow env=ARV_P81_LEAK\n" + nap + "let g = fn() { getenv(\"ARV_P81_LEAK\") }; let bad = fn() { 1 / 0 };\n" +
				"puts(scope (supervise: \"collect\") { let ts = [spawn g() with [env(\"ARV_P81_LEAK\")], spawn bad()];\n" +
				"  [match await ts[0] { ok(v) => v, err(e) => e }, match await ts[1] { ok(v) => 0, err(e) => e[\"message\"] }] });",
			0, ""},
		{"timeout under the header",
			"// arv:allow env=ARV_P81_LEAK\n" + nap + "scope (timeout_ms: 50) { spawn nap(); await spawn (fn() { getenv(\"ARV_P81_LEAK\"); nap() })() with [env(\"ARV_P81_LEAK\")] }",
			1, "budget exceeded: timeout"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			script := writeScript(t, tc.src)
			before := goruntime.NumGoroutine()
			for _, args := range [][]string{{"check", script}, {"run", "--manifest", script}} {
				code, _, errOut := runArv(args...)
				if args[0] == "check" {
					continue // its exit status is the manifest's; only its goroutines matter here
				}
				if code != tc.code || !strings.Contains(errOut, tc.stderr) {
					t.Fatalf("%v: exit %d, stderr %q; want %d and %q", args, code, errOut, tc.code, tc.stderr)
				}
			}
			n := goruntime.NumGoroutine()
			for deadline := time.Now().Add(10 * time.Second); n > before && time.Now().Before(deadline); {
				time.Sleep(5 * time.Millisecond)
				n = goruntime.NumGoroutine()
			}
			if n > before {
				buf := make([]byte, 1<<16)
				t.Fatalf("goroutines: %d before, %d after\n%s", before, n, buf[:goruntime.Stack(buf, true)])
			}
		})
	}
}
