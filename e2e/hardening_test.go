package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Cases from the capability-layer review: each one pins a hole that was
// verified with a probe before it was fixed.

// limit is the interpreter's body cap (evaluator.maxBody), 10 MiB.
const limit = 10 << 20

// An empty scope is a usage error, never "everything" or the working dir
// (e.g. --allow "fs_read=$UNSET" used to grant the cwd).
func TestEmptyGrantScope(t *testing.T) {
	t.Parallel()
	var specs []spec
	for _, a := range []string{"fs_read=", "fs_write=", "net=", "env="} {
		specs = append(specs, spec{
			name: a, script: "basics.arv", allow: []string{a},
			code: 2, stderrHas: []string{"empty scope"},
		})
	}
	runAll(t, specs)
}

// Net grants that could never match a request are rejected up front.
func TestNetGrantValidation(t *testing.T) {
	t.Parallel()
	bad := func(allow, why string) spec {
		return spec{name: allow, script: "basics.arv", allow: []string{allow}, code: 2, stderrHas: []string{why}}
	}
	runAll(t, []spec{
		bad("net=127.0.0.1:1234", "a port is not allowed"),
		bad("net=https://example.com", "want a bare host name"),
		bad("net=user@example.com", "want a bare host name"),
		bad("net=*.0.0.1", "wildcards cannot cover IP addresses"),
		bad("net=a.*.com", "a wildcard must be"),
		{
			name: "bracketed IPv6 is accepted", script: "basics.arv", allow: []string{"net=[::1]"},
			stdout: "9\n7\n3\ntrue\n42\n42\nhttp://example.test\nbig\n",
		},
	})
}

// One ".." rule: ".." after a symlink goes to the parent of the link's
// target, as the kernel does, for requests and grants alike. g/l is a
// symlink to else/dir, so g/l/../x is else/x (what cat reads), not g/x.
func TestDotDotAfterSymlink(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "g", "x"), "WRONG g/x")
	mustWrite(t, filepath.Join(root, "else", "x"), "right else/x")
	mustWrite(t, filepath.Join(root, "else", "dir", ".keep"), "")
	mustSymlink(t, filepath.Join(root, "else", "dir"), filepath.Join(root, "g", "l"))

	read := func(name, allow, path string, ok bool) spec {
		s := spec{
			name: name, script: "read_file.arv", dir: root,
			vars: map[string]string{"PATH": path}, allow: []string{allow},
			stdoutNot: []string{"WRONG"},
		}
		if ok {
			s.stdout = "right else/x\n"
		} else {
			s.code = 1
			// The denial names the path as written; where it resolves
			// (else/x) is outside the grant and must not be revealed.
			s.stderrHas = []string{"capability denied: fs_read(", "g/l/../x"}
			s.stderrNot = []string{"WRONG", "else/x"}
		}
		return s
	}
	runAll(t, []spec{
		read("relative path under the link's dir is denied", "fs_read=g", "g/l/../x", false),
		read("relative path under the target's dir is allowed", "fs_read=else", "g/l/../x", true),
		read("absolute path under the link's dir is denied", "fs_read="+root+"/g", root+"/g/l/../x", false),
		read("absolute path under the target's dir is allowed", "fs_read="+root+"/else", root+"/g/l/../x", true),
		read("relative grant follows the same rule", "fs_read=g/l/..", "else/x", true),
		{
			name: "relative grant does not cover the lexical parent", script: "read_file.arv", dir: root,
			vars: map[string]string{"PATH": "g/x"}, allow: []string{"fs_read=g/l/.."},
			code: 1, stderrHas: []string{"capability denied: fs_read("}, stderrNot: []string{"WRONG"},
		},
	})
}

// Bodies over the limit are an error, not silently truncated.
func TestOversizeBodies(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	big := filepath.Join(root, "big.bin")
	f, err := os.Create(big)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(limit + 1); err != nil { // sparse: no disk used
		t.Fatal(err)
	}
	f.Close()
	runAll(t, []spec{
		{
			name: "file", script: "read_file.arv",
			vars: map[string]string{"PATH": big}, allow: []string{"fs_read=" + root},
			code: 1, stderrHas: []string{"error: read_file: body exceeds 10 MiB limit"},
		},
		{
			name: "http body", script: "http_get.arv",
			vars:  map[string]string{"ROUTE": "/bytes/" + strconv.Itoa(limit+1)},
			allow: []string{"net=127.0.0.1"},
			code:  1, stderrHas: []string{"error: http_get: body exceeds 10 MiB limit"},
		},
		{
			name: "http body just under the limit", script: "http_get.arv",
			vars:   map[string]string{"ROUTE": "/bytes/16"},
			allow:  []string{"net=127.0.0.1"},
			stdout: "200\n" + strings.Repeat("a", 16) + "\n",
		},
	})
}

// read_file of a FIFO with no writer used to block forever in open.
func TestReadFIFOFailsFast(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("mkfifo"); err != nil {
		t.Skip("mkfifo not available")
	}
	root := t.TempDir()
	fifo := filepath.Join(root, "pipe")
	if out, err := exec.Command("mkfifo", fifo).CombinedOutput(); err != nil {
		t.Fatalf("mkfifo: %v\n%s", err, out)
	}
	check(t, spec{
		script: "read_file.arv", timeout: 10 * time.Second,
		vars: map[string]string{"PATH": fifo}, allow: []string{"fs_read=" + root},
		code: 1, stderrHas: []string{"read_file:", "not a regular file (named pipe)"},
	})
}

// Hosts are checked in the form the request will actually reach.
func TestNetHostForms(t *testing.T) {
	t.Parallel()
	hostport := strings.TrimPrefix(srv.URL, "http://")
	port := hostport[strings.LastIndex(hostport, ":")+1:]
	get := func(name, url string, allow []string, ok bool, okHits int, denied string) spec {
		s := spec{
			name: name, script: "http_get_url.arv",
			vars: map[string]string{"URL": url}, allow: allow,
			verify: func(t *testing.T) {
				if got := hits(t, "/ok"); got != okHits {
					t.Errorf("server saw %d requests for /ok, want %d", got, okHits)
				}
			},
		}
		if ok {
			s.stdout = "200\n" + okBody + "\n"
		} else {
			s.code = 1
			s.stderrHas = []string{`capability denied: net("` + denied + `")`}
		}
		return s
	}
	loopback := []string{"net=127.0.0.1"}
	runAll(t, []spec{
		get("userinfo cannot pose as the host", "http://127.0.0.1@other.invalid/ok", loopback, false, 0, "other.invalid"),
		get("userinfo with a port cannot pose as the host", "http://"+hostport+"@other.invalid:"+port+"/ok", loopback, false, 0, "other.invalid"),
		get("uppercase request host", "http://LOCALHOST:"+port+"/ok", []string{"net=localhost"}, true, 1, ""),
		get("trailing-dot request host", "http://localhost.:"+port+"/ok", []string{"net=localhost"}, true, 1, ""),
		get("uppercase trailing-dot grant", "http://localhost:"+port+"/ok", []string{"net=LocalHost."}, true, 1, ""),
		get("denial names the normalised host", "http://LOCALHOST.:"+port+"/ok", loopback, false, 0, "localhost"),
	})
}
