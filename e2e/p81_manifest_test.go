package e2e

import (
	"path/filepath"
	"testing"
)

// Phase 8.1 hardening: arv run --manifest and the arv:allow header through
// the real binary.

// A need outside the header stops the run before anything is evaluated:
// not even the first puts runs, no file is written, the server sees
// nothing.
func TestP81ManifestRefusesBeforeEvaluating(t *testing.T) {
	dir := t.TempDir()
	check(t, spec{
		name: "refuse", script: "p81_refuse_first.arv", args: []string{"run", "--manifest", "{{SCRIPT}}"}, dir: dir,
		code:      1,
		stderrHas: []string{"needs fs_write=made.txt (write_file at 3:1), net=" + serverHost(t) + " (http_get at 4:1), outside its arv:allow manifest"},
		verify: func(t *testing.T) {
			if n := hits(t, "/ok"); n != 0 {
				t.Errorf("the server saw %d requests", n)
			}
			assertAbsent(t, filepath.Join(dir, "made.txt"))
		},
	})
}

// I1 under --manifest: a need arv check cannot see (a note) is not
// refused up front, but the run holds the header's grants and nothing
// else, so the runtime denies it at the call, before any request leaves.
// I2: a capability built at run time is checked against the header's
// grants at spawn.
func TestP81ManifestNotesAreDeniedAtRunTime(t *testing.T) {
	port := serverPort(t)
	runAll(t, []spec{{
		name: "effect", script: "p81_dynamic.arv", args: []string{"run", "--manifest", "{{SCRIPT}}"},
		vars: map[string]string{"HOST": serverHost(t), "PORT": port}, code: 1, stdout: "200\n",
		stderrHas: []string{"capability denied: net(\"localhost:" + port + "\")"},
		verify: func(t *testing.T) {
			if n := hits(t, "/ok"); n != 1 {
				t.Errorf("the server saw %d requests, want 1 (the covered one)", n)
			}
		},
	}, {
		name: "check prints the note", script: "p81_dynamic.arv", args: []string{"check", "{{SCRIPT}}"},
		vars:   map[string]string{"HOST": serverHost(t), "PORT": port},
		stdout: "# http_get at 2:19: host not statically known\n",
	}})
	dir := t.TempDir()
	script := filepath.Join(dir, "attenuate.arv")
	mustWrite(t, script, "// arv:allow env=HOME\nlet mk = fn(s) { env(s) };\nlet f = fn() { 1 };\n"+
		"puts(await spawn f() with [mk(\"HOME\")]);\nputs(await spawn f() with [mk(\"PATH\")]);\n")
	check(t, spec{
		name: "spawn", args: []string{"run", "--manifest", script}, env: []string{"HOME=/h", "PATH=/p"},
		code: 1, stdout: "1\n",
		stderrHas: []string{`capability denied: env("PATH") (holds: [env("HOME")])`},
	})
}

// Without a header, --manifest grants nothing: a program that needs
// nothing runs, one that needs a grant is refused. A comment after the
// first line of code is not the header, even if it says arv:allow.
func TestP81ManifestWithoutAHeader(t *testing.T) {
	dir := t.TempDir()
	none, needs, late := filepath.Join(dir, "none.arv"), filepath.Join(dir, "needs.arv"), filepath.Join(dir, "late.arv")
	mustWrite(t, none, "puts(\"hi\");\n")
	mustWrite(t, needs, "puts(getenv(\"HOME\"));\n")
	mustWrite(t, late, "puts(\"hi\");\n// arv:allow env=HOME\ngetenv(\"HOME\");\n")
	runAll(t, []spec{
		{name: "nothing needed", args: []string{"run", "--manifest", none}, stdout: "hi\n"},
		{name: "a need", args: []string{"run", "--manifest", needs}, env: []string{"HOME=/h"}, code: 1,
			stderrHas: []string{"needs env=HOME (getenv at 1:6), outside its arv:allow manifest"}},
		{name: "late comment, run", args: []string{"run", "--manifest", late}, env: []string{"HOME=/h"}, code: 1,
			stderrHas: []string{"needs env=HOME (getenv at 3:1), outside its arv:allow manifest"}},
		{name: "late comment, check", args: []string{"check", late}, stdout: "--allow env=HOME\n"},
	})
}

// A header line that is not one valid grant is an error for both
// commands, before anything runs.
func TestP81ManifestMalformedHeader(t *testing.T) {
	dir := t.TempDir()
	two, kind := filepath.Join(dir, "two.arv"), filepath.Join(dir, "kind.arv")
	mustWrite(t, two, "// arv:allow net=a b\nputs(1);\n")
	mustWrite(t, kind, "// arv:allow bogus=x\nputs(1);\n")
	var specs []spec
	for _, cmd := range [][]string{{"check"}, {"run", "--manifest"}} {
		specs = append(specs,
			spec{name: cmd[0] + " two words", args: append(append([]string{}, cmd...), two), code: 1,
				stderrHas: []string{`line 1: arv:allow wants one kind=scope, got "net=a b"`}},
			spec{name: cmd[0] + " unknown kind", args: append(append([]string{}, cmd...), kind), code: 1,
				stderrHas: []string{`line 1: unknown capability kind "bogus"`}})
	}
	runAll(t, specs)
}

// I3: the header and the program's paths are compared canonically, as
// the runtime admits them. A path through a symlink into the granted
// directory is covered (and runs); one that climbs out with .. is refused
// by check and run --manifest, before the program's first line runs.
// A directory grant covers the files under it.
func TestP81ManifestComparesCanonicalPaths(t *testing.T) {
	for _, tc := range []struct {
		name, path, stdout, refused string
	}{
		{"through a symlink", "alias/f.txt", "started\n", ""},
		{"inside", "real/f.txt", "started\n", ""},
		{"climbing out", "real/../top.txt", "", "needs fs_read=real/../top.txt (read_file at 4:30), outside its arv:allow manifest"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			mustWrite(t, filepath.Join(dir, "real", "f.txt"), "granted")
			mustWrite(t, filepath.Join(dir, "top.txt"), "not granted")
			mustWrite(t, filepath.Join(dir, "out", ".keep"), "")
			mustSymlink(t, "real", filepath.Join(dir, "alias"))
			vars := map[string]string{"PATH": tc.path}
			for _, cmd := range [][]string{{"check"}, {"run", "--manifest"}} {
				s := spec{name: cmd[0], script: "p81_fs.arv", args: append(append([]string{}, cmd...), "{{SCRIPT}}"), vars: vars, dir: dir}
				if cmd[0] == "check" {
					s.stdout = "--allow fs_read=" + tc.path + "\n--allow fs_write=out/report.txt\n"
				} else {
					s.stdout = tc.stdout
				}
				if tc.refused != "" {
					s.code, s.stderrHas = 1, []string{tc.refused}
				}
				check(t, s)
			}
			if tc.refused == "" {
				assertFile(t, filepath.Join(dir, "out", "report.txt"), "granted")
			} else {
				assertAbsent(t, filepath.Join(dir, "out", "report.txt"))
			}
		})
	}
}

// A dial to 0.0.0.0 lands on loopback, so it also needs 127.0.0.1:PORT.
// arv check reports it: a header without it is refused before the run;
// with it, the request reaches the server.
func TestP81ManifestDialNeed(t *testing.T) {
	port := serverPort(t)
	loop := "// arv:allow net=127.0.0.1:" + port
	runAll(t, []spec{{
		name: "missing loopback", script: "p81_dial.arv", args: []string{"run", "--manifest", "{{SCRIPT}}"},
		vars: map[string]string{"PORT": port, "MORE": ""}, code: 1,
		stderrHas: []string{"needs net=127.0.0.1:" + port + " (http_get at 3:6), outside its arv:allow manifest"},
		verify: func(t *testing.T) {
			if n := hits(t, "/ok"); n != 0 {
				t.Errorf("the server saw %d requests", n)
			}
		},
	}, {
		name: "check prints both", script: "p81_dial.arv", args: []string{"check", "{{SCRIPT}}"},
		vars:   map[string]string{"PORT": port, "MORE": loop},
		stdout: "--allow net=0.0.0.0:" + port + "\n--allow net=127.0.0.1:" + port + "\n",
	}, {
		name: "with loopback", script: "p81_dial.arv", args: []string{"run", "--manifest", "{{SCRIPT}}"},
		vars: map[string]string{"PORT": port, "MORE": loop}, stdout: "200\n",
		verify: func(t *testing.T) {
			if n := hits(t, "/ok"); n != 1 {
				t.Errorf("the server saw %d requests, want 1", n)
			}
		},
	}})
}
