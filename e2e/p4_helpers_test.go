package e2e

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// p4case is one inline program run with net=127.0.0.1 granted (plus
// allow), checked like a spec, then by verify against the run's result.
type p4case struct {
	name   string
	src    string // {{SERVER}} and {{ID}} are expanded
	allow  []string
	stdout string
	err    string // the stderr line without "error: " and its position; "" for success
	at     string // "LINE:COL" of the error
	verify func(t *testing.T, res result)
}

// runP4 runs every case as a parallel subtest.
func runP4(t *testing.T, cases []p4case) {
	t.Helper()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "prog.arv")
			mustWrite(t, path, expand(t, c.src, nil))
			args := []string{"run", "--allow", "net=127.0.0.1"}
			for _, a := range c.allow {
				args = append(args, "--allow", a)
			}
			s := spec{args: append(args, path), stdout: c.stdout, stderrNot: goCrash, timeout: 60 * time.Second}
			if c.err != "" {
				s.code = 1
				s.stderrHas = []string{"error: " + c.err + " (at " + path + ":" + c.at + ")\n"}
			}
			res := run(t, s)
			expect(t, s, res)
			if c.verify != nil {
				c.verify(t, res)
			}
		})
	}
}

// wantHits checks how many requests for path carried this test's id.
func wantHits(path string, want int) func(*testing.T, result) {
	return func(t *testing.T, res result) {
		t.Helper()
		if got := hits(t, path); got != want {
			t.Fatalf("%s hits = %d, want %d\n%s", path, got, want, res)
		}
	}
}

// col is the 1-based column of the first sub in line.
func col(line, sub string) int { return strings.Index(line, sub) + 1 }

const grant = `with [net("127.0.0.1")]`
