package e2e

import (
	"net/url"
	"path/filepath"
	"strings"
	"testing"
)

// Phase 8.1, arv check and run --manifest through the real binary.

// serverHost is the test server's host:port.
func serverHost(t *testing.T) string {
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	return u.Host
}

// Acceptance (15-phase8-tooling.md 8.1, as amended in p81-plan-1.md): for
// every file in examples/, arv check prints the set its header's arv run
// line uses, except where that line grants less than the program can ask
// for (agent.arv offers its model a tool it does not grant) or more than
// the program text names (uptime writes out/report.json under the line's fs_write=out) or
// names what only a run knows (uptime's net host comes from targets.txt,
// and a note says so).
func TestP81ExamplesCheck(t *testing.T) {
	want := map[string]string{
		// agent.arv's header grants tool=weather only, on purpose: the
		// model is offered notes too and gets "capability denied" if it
		// asks. arv check reports tool=notes because the model may ask for
		// any tool it is offered, and the notes tool reads a path it is
		// passed, so read_file gets a note.
		"agent.arv": "--allow fs_read=examples/weather.txt\n--allow llm=anthropic:claude-sonnet-5-5\n" +
			"--allow tool=notes\n--allow tool=weather\n" +
			"# read_file at 15:25: path not statically known\n",
		"fanout.arv": "--allow net=api.github.com\n" +
			"# http_get at 14:23: host not statically known\n",
		"uptime.arv": "--allow fs_read=targets.txt\n--allow fs_write=out/report.json\n" +
			"# http_get at 15:16: host not statically known\n" +
			"# net at 23:34: scope not statically known\n" +
			"# net at 34:34: scope not statically known\n",
	}
	paths, err := filepath.Glob("../examples/*.arv")
	if err != nil || len(paths) == 0 {
		t.Fatalf("examples: %v %v", paths, err)
	}
	for _, p := range paths {
		name := filepath.Base(p)
		out, ok := want[name]
		if !ok {
			t.Errorf("examples/%s has no expected arv check output here: add it", name)
			continue
		}
		abs, err := filepath.Abs(p)
		if err != nil {
			t.Fatal(err)
		}
		check(t, spec{name: name, args: []string{"check", abs}, stdout: out})
	}
}

func TestP81CheckNeverRunsTheProgram(t *testing.T) {
	dir := t.TempDir()
	check(t, spec{
		name: "noeffects", script: "p81_noeffects.arv", args: []string{"check", "{{SCRIPT}}"}, dir: dir,
		stdout: "--allow net=" + serverHost(t) + "\n--allow fs_write=made.txt\n--allow env=HOME\n",
		verify: func(t *testing.T) {
			if n := hits(t, "/ok"); n != 0 {
				t.Errorf("the server saw %d requests", n)
			}
			assertAbsent(t, filepath.Join(dir, "made.txt"))
		},
	})
}

func TestP81RunManifest(t *testing.T) {
	host := serverHost(t)
	ok, refused := t.TempDir(), t.TempDir()
	noHits := func(dir string) func(t *testing.T) {
		return func(t *testing.T) {
			if n := hits(t, "/ok"); n != 0 {
				t.Errorf("the server saw %d requests", n)
			}
			assertAbsent(t, filepath.Join(dir, "out.txt"))
		}
	}
	runAll(t, []spec{{
		name: "covered", script: "p81_manifest.arv", args: []string{"run", "--manifest", "{{SCRIPT}}"},
		vars: map[string]string{"GRANT": host}, dir: ok, stdout: "200\n",
		verify: func(t *testing.T) {
			if n := hits(t, "/ok"); n != 1 {
				t.Errorf("the server saw %d requests, want 1", n)
			}
			assertFile(t, filepath.Join(ok, "out.txt"), okBody)
		},
	}, {
		name: "outside", script: "p81_manifest.arv", args: []string{"run", "--manifest", "{{SCRIPT}}"},
		vars: map[string]string{"GRANT": "api.github.com"}, dir: refused, code: 1,
		stderrHas: []string{"needs net=" + host + " (http_get at 3:9), outside its arv:allow manifest"},
		verify:    noHits(refused),
	}, {
		name: "check says so too", script: "p81_manifest.arv", args: []string{"check", "{{SCRIPT}}"},
		vars: map[string]string{"GRANT": "api.github.com"}, code: 1,
		stdout:    "--allow net=" + host + "\n--allow fs_write=out.txt\n",
		stderrHas: []string{"outside its arv:allow manifest"},
	}, {
		name: "with --allow", script: "p81_manifest.arv",
		args: []string{"run", "--allow", "env=HOME", "--manifest", "{{SCRIPT}}"},
		vars: map[string]string{"GRANT": host}, code: 2,
		stderrHas: []string{"cannot be combined with --allow"},
	}})
}

// The usage text names both commands.
func TestP81Usage(t *testing.T) {
	res := run(t, spec{name: "usage", args: []string{"help"}})
	if res.code != 2 || !strings.Contains(res.stderr, "arv check FILE") || !strings.Contains(res.stderr, "--manifest") {
		t.Fatalf("%s", res)
	}
}
