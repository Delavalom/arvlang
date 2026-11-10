package e2e

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestLanguageBasics(t *testing.T) {
	t.Parallel()
	check(t, spec{
		script: "basics.arv",
		code:   0,
		stdout: "9\n7\n3\ntrue\n42\n42\nhttp://example.test\nbig\n",
	})
}

// I1: getenv needs env=NAME for exactly that variable.
func TestEnv(t *testing.T) {
	t.Parallel()
	const secret = "s3cret-value"
	name := map[string]string{"NAME": "ARV_E2E_TOKEN"}
	env := []string{"ARV_E2E_TOKEN=" + secret, "ARV_E2E_OTHER=other-value"}
	runAll(t, []spec{
		{
			name: "no grant is denied", script: "getenv.arv", vars: name, env: env,
			code:      1,
			stderrHas: []string{"capability denied", `env("ARV_E2E_TOKEN")`},
			stderrNot: []string{secret},
		},
		{
			name: "granted prints value", script: "getenv.arv", vars: name, env: env,
			allow:  []string{"env=ARV_E2E_TOKEN"},
			stdout: secret + "\n",
		},
		{
			name: "grant for another variable is denied", script: "getenv.arv", vars: name, env: env,
			allow:     []string{"env=ARV_E2E_OTHER"},
			code:      1,
			stderrHas: []string{"capability denied", `env("ARV_E2E_TOKEN")`, `holds: [env("ARV_E2E_OTHER")]`},
			stderrNot: []string{secret},
		},
		{
			// The harness passes an explicit env, so HOME is unset in the child.
			name: "child env is hermetic", script: "getenv.arv", vars: map[string]string{"NAME": "HOME"},
			allow:  []string{"env=HOME"},
			stdout: "\n",
		},
	})
}

// I3: fs_read covers a canonical path prefix; symlinks and ".." can't escape.
func TestFSRead(t *testing.T) {
	t.Parallel()
	root := t.TempDir() // on macOS this is under /var, a symlink to /private/var
	canonRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	granted := filepath.Join(root, "data")
	mustWrite(t, filepath.Join(granted, "hello.txt"), "hello from data")
	mustWrite(t, filepath.Join(granted, "sub", "deep.txt"), "deep inside")
	mustWrite(t, filepath.Join(root, "data2", "secret.txt"), "TOP-SECRET sibling") // shares the "data" prefix
	mustWrite(t, filepath.Join(root, "outside", "secret.txt"), "TOP-SECRET outside")
	mustSymlink(t, filepath.Join(root, "outside", "secret.txt"), filepath.Join(granted, "link-file"))
	mustSymlink(t, filepath.Join(root, "outside"), filepath.Join(granted, "link-dir"))
	mustWrite(t, filepath.Join(root, "real", "f.txt"), "via alias")
	mustSymlink(t, filepath.Join(root, "real"), filepath.Join(root, "alias"))

	read := func(name, allow, path, want string) spec {
		s := spec{name: name, script: "read_file.arv", vars: map[string]string{"PATH": path}}
		if allow != "" {
			s.allow = []string{allow}
		}
		if want != "" {
			s.stdout = want + "\n"
		} else {
			s.code = 1
			s.stderrHas = []string{"capability denied: fs_read("}
			s.stderrNot = []string{"TOP-SECRET"} // denial must not leak contents
		}
		return s
	}
	g := "fs_read=" + granted
	specs := []spec{
		read("inside granted dir", g, filepath.Join(granted, "hello.txt"), "hello from data"),
		read("nested inside granted dir", g, filepath.Join(granted, "sub", "deep.txt"), "deep inside"),
		read("no grant is denied", "", filepath.Join(granted, "hello.txt"), ""),
		read("fs_write does not imply fs_read", "fs_write="+granted, filepath.Join(granted, "hello.txt"), ""),
		read("sibling dir sharing a prefix is denied", g, filepath.Join(root, "data2", "secret.txt"), ""),
		read("dotdot escape is denied", g, granted+"/../outside/secret.txt", ""),
		read("symlinked file pointing outside is denied", g, filepath.Join(granted, "link-file"), ""),
		read("symlinked dir pointing outside is denied", g, filepath.Join(granted, "link-dir", "secret.txt"), ""),
		read("grant via symlink, read via real path", "fs_read="+filepath.Join(root, "alias"), filepath.Join(canonRoot, "real", "f.txt"), "via alias"),
		read("grant via symlink, read via symlink", "fs_read="+filepath.Join(root, "alias"), filepath.Join(root, "alias", "f.txt"), "via alias"),
		read("grant via real path, read via symlink", "fs_read="+filepath.Join(root, "real"), filepath.Join(root, "alias", "f.txt"), "via alias"),
		// The macOS /var -> /private/var case (identical paths on Linux).
		read("grant uncanonical temp path, read canonical", g, filepath.Join(canonRoot, "data", "hello.txt"), "hello from data"),
		read("grant canonical temp path, read uncanonical", "fs_read="+filepath.Join(canonRoot, "data"), filepath.Join(granted, "hello.txt"), "hello from data"),
	}
	// Relative grants and relative paths resolve against the working directory.
	rel := read("relative grant and path", "fs_read=data", "data/hello.txt", "hello from data")
	rel.dir = root
	specs = append(specs, rel)
	runAll(t, specs)
}

// I3: fs_write is separate from fs_read and can't be steered outside its dir.
func TestFSWrite(t *testing.T) {
	t.Parallel()
	const text = "written by arv"
	inside := func(granted, outside string) string { return filepath.Join(granted, "out.txt") }
	cases := []struct {
		name   string
		allow  string                               // "" means no grant
		target func(granted, outside string) string // path to write; may create fixtures
		ok     bool
	}{
		{"granted write succeeds", "fs_write={{GRANTED}}", inside, true},
		{"only fs_read is denied", "fs_read={{GRANTED}}", inside, false},
		{"no grant is denied", "", inside, false},
		{"sibling dir sharing a prefix is denied", "fs_write={{GRANTED}}",
			func(granted, outside string) string { return granted + "2/out.txt" }, false},
		{"dotdot escape is denied", "fs_write={{GRANTED}}",
			func(granted, outside string) string { return granted + "/../outside/out.txt" }, false},
		{"symlink to an existing outside file is denied", "fs_write={{GRANTED}}",
			func(granted, outside string) string {
				link := filepath.Join(granted, "link")
				mustSymlink(t, filepath.Join(outside, "keep.txt"), link)
				return link
			}, false},
	}
	var specs []spec
	for _, c := range cases {
		// Each case gets its own dirs so parallel cases can't see each other's files.
		root := t.TempDir()
		granted, outside := filepath.Join(root, "data"), filepath.Join(root, "outside")
		mustWrite(t, filepath.Join(granted, ".keep"), "")
		mustWrite(t, filepath.Join(root, "data2", ".keep"), "")
		mustWrite(t, filepath.Join(outside, "keep.txt"), "untouched")
		path := c.target(granted, outside)
		_, statErr := os.Lstat(path)
		existed := statErr == nil

		s := spec{
			name: c.name, script: "write_file.arv",
			vars: map[string]string{"PATH": path, "TEXT": text, "GRANTED": granted},
		}
		if c.allow != "" {
			s.allow = []string{c.allow}
		}
		if c.ok {
			s.stdout = "ok\n"
			s.verify = func(t *testing.T) { assertFile(t, path, text) }
		} else {
			s.code = 1
			s.stderrHas = []string{"capability denied: fs_write("}
			s.verify = func(t *testing.T) {
				if !existed {
					assertAbsent(t, path)
				}
				assertAbsent(t, filepath.Join(outside, "out.txt"))
				assertAbsent(t, root+"/data2/out.txt")
				assertFile(t, filepath.Join(outside, "keep.txt"), "untouched")
			}
		}
		specs = append(specs, s)
	}
	runAll(t, specs)
}

// I3: a dangling symlink inside the granted dir must not let a write create
// a file outside it, whether the link is absolute, relative, or a directory.
func TestFSWriteDanglingSymlink(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		// setup makes fixtures and returns the path to write and the file
		// that must not appear outside the grant.
		setup func(t *testing.T, granted, outside string) (path, escaped string)
	}{
		{"absolute dangling leaf", func(t *testing.T, granted, outside string) (string, string) {
			escaped := filepath.Join(outside, "created.txt")
			mustSymlink(t, escaped, filepath.Join(granted, "dangling"))
			return filepath.Join(granted, "dangling"), escaped
		}},
		{"relative dangling leaf", func(t *testing.T, granted, outside string) (string, string) {
			mustSymlink(t, "../outside/created.txt", filepath.Join(granted, "dangling"))
			return filepath.Join(granted, "dangling"), filepath.Join(outside, "created.txt")
		}},
		{"chain ending in a dangling leaf", func(t *testing.T, granted, outside string) (string, string) {
			escaped := filepath.Join(outside, "created.txt")
			mustSymlink(t, "hop", filepath.Join(granted, "dangling"))
			mustSymlink(t, escaped, filepath.Join(granted, "hop"))
			return filepath.Join(granted, "dangling"), escaped
		}},
		{"new file under a symlinked dir", func(t *testing.T, granted, outside string) (string, string) {
			mustSymlink(t, outside, filepath.Join(granted, "link-dir"))
			return filepath.Join(granted, "link-dir", "created.txt"), filepath.Join(outside, "created.txt")
		}},
	}
	var specs []spec
	for _, c := range cases {
		root := t.TempDir()
		granted, outside := filepath.Join(root, "data"), filepath.Join(root, "outside")
		mustWrite(t, filepath.Join(granted, "keep.txt"), "x")
		if err := os.MkdirAll(outside, 0o755); err != nil {
			t.Fatal(err)
		}
		path, escaped := c.setup(t, granted, outside)
		specs = append(specs, spec{
			name: c.name, script: "write_file.arv",
			vars:  map[string]string{"PATH": path, "TEXT": "escaped"},
			allow: []string{"fs_write=" + granted},
			code:  1, stderrHas: []string{"capability denied: fs_write("},
			verify: func(t *testing.T) { assertAbsent(t, escaped) },
		})
	}
	runAll(t, specs)
}

// I3: read_file through a dangling symlink inside the grant is denied (its
// target is outside), and never reads a file created there afterwards.
func TestFSReadDanglingSymlink(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	granted, outside := filepath.Join(root, "data"), filepath.Join(root, "outside")
	mustWrite(t, filepath.Join(granted, "keep.txt"), "x")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	mustSymlink(t, filepath.Join(outside, "later.txt"), filepath.Join(granted, "dangling"))
	mustSymlink(t, filepath.Join(granted, "nowhere.txt"), filepath.Join(granted, "dangling-inside"))
	runAll(t, []spec{
		{
			name: "dangling link to outside is denied", script: "read_file.arv",
			vars:  map[string]string{"PATH": filepath.Join(granted, "dangling")},
			allow: []string{"fs_read=" + granted},
			code:  1, stderrHas: []string{"capability denied: fs_read(", "later.txt"},
		},
		{
			name: "dangling link to a missing file inside is not found", script: "read_file.arv",
			vars:  map[string]string{"PATH": filepath.Join(granted, "dangling-inside")},
			allow: []string{"fs_read=" + granted},
			code:  1, stderrHas: []string{"read_file:", "no such file or directory"},
			stderrNot: []string{"capability denied"},
		},
	})
}

// I3: net is checked on the first request and again on every redirect hop.
func TestNet(t *testing.T) {
	t.Parallel()
	get := func(name, route string, allow []string, stdout string) spec {
		s := spec{name: name, script: "http_get.arv", vars: map[string]string{"ROUTE": route}, allow: allow, stdout: stdout}
		if stdout == "" {
			s.code = 1
			s.stderrHas = []string{"capability denied: net("}
		}
		return s
	}
	hitsWant := func(s spec, want map[string]int) spec {
		s.verify = func(t *testing.T) {
			for path, n := range want {
				if got := hits(t, path); got != n {
					t.Errorf("server saw %d requests for %s, want %d", got, path, n)
				}
			}
		}
		return s
	}
	loopback := []string{"net=127.0.0.1"}

	denied := func(s spec, host string) spec {
		s.stderrHas = append(s.stderrHas, `net("`+host+`")`)
		return s
	}
	runAll(t, []spec{
		hitsWant(get("granted host gets 200 and body", "/ok", loopback, "200\n"+okBody+"\n"), map[string]int{"/ok": 1}),
		get("non-2xx status is a value, not an error", "/status/404", loopback, "404\n"+notFoundBody+"\n"),
		hitsWant(denied(get("no grant is denied", "/ok", nil, ""), "127.0.0.1"), map[string]int{"/ok": 0}),
		hitsWant(denied(get("grant for another host is denied", "/ok", []string{"net=example.com"}, ""), "127.0.0.1"), map[string]int{"/ok": 0}),
		get("wildcard grant allows any host", "/ok", []string{"net=*"}, "200\n"+okBody+"\n"),
		hitsWant(denied(get("redirect to an ungranted host is denied", "/redirect-out", loopback, ""), "localhost"),
			map[string]int{"/redirect-out": 1, "/redirect-target": 0}),
		hitsWant(get("redirect to a granted host succeeds", "/redirect-out", []string{"net=127.0.0.1", "net=localhost"}, "200\n"+targetBody+"\n"),
			map[string]int{"/redirect-out": 1, "/redirect-target": 1}),
	})
}

func TestCLI(t *testing.T) {
	t.Parallel()
	runAll(t, []spec{
		{
			name: "unknown flag is a usage error", script: "basics.arv",
			args: []string{"run", "--bogus", "{{SCRIPT}}"},
			code: 2, stderrHas: []string{"usage:"},
		},
		{
			name: "unknown command is a usage error",
			args: []string{"build"},
			code: 2, stderrHas: []string{"usage:"},
		},
		{
			name: "unknown capability kind is a usage error", script: "basics.arv",
			args: []string{"run", "--allow", "disk=/", "{{SCRIPT}}"},
			code: 2, stderrHas: []string{`error: unknown capability kind "disk"`},
		},
		{
			name: "allow without kind=scope is a usage error", script: "basics.arv",
			args: []string{"run", "--allow", "net", "{{SCRIPT}}"},
			code: 2, stderrHas: []string{"want kind=scope"},
		},
		{
			name: "missing file",
			args: []string{"run", "does-not-exist.arv"},
			code: 1, stderrHas: []string{"error: open does-not-exist.arv"},
		},
		{
			name: "syntax error reports the parser message", script: "syntax_error.arv",
			code: 1, stderrHas: []string{"error: parse error: no prefix parse function for ; found"},
		},
		{
			name: "runaway recursion is an Arv error, not a Go crash", script: "recursion.arv",
			code:      1,
			stderrHas: []string{"error: stack overflow: call depth exceeded 10000"},
			stderrNot: []string{"goroutine ", "panic", "fatal error"},
		},
	})
}

// sleep is timed by the server: the script hits /mark before and after
// sleeping, so process start-up time can't hide a sleep that returns early.
func TestSleep(t *testing.T) {
	t.Parallel()
	const ms = 100
	s := spec{
		script: "sleep.arv", vars: map[string]string{"MS": strconv.Itoa(ms)},
		allow: []string{"net=127.0.0.1"}, stdout: "slept\n",
	}
	res := run(t, s)
	expect(t, s, res)
	m := markTimes(t)
	if len(m) != 2 {
		t.Fatalf("server saw %d /mark requests, want 2", len(m))
	}
	if gap := m[1].Sub(m[0]); gap < ms*time.Millisecond {
		t.Fatalf("sleep(%d) took %s between marks", ms, gap)
	}
}
