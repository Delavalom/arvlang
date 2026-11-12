package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Phase 5E through the binary, continued: each new form costs what its
// hand-written equivalent costs (G4, G6), the nesting limit holds for the
// new chains, and examples/uptime-ergonomic.arv does what uptime.arv does.

// smallestLimit is the smallest value of flag (searched in [1, hi]) under
// which src runs without a budget error of that kind. The root charges its
// last partial chunk when it closes, so this is the exact charge.
func smallestLimit(t *testing.T, src, flag, budgetErr string, hi int) int {
	t.Helper()
	s, _ := progSpec(t, src)
	fails := func(n int) bool {
		s := s
		s.args = append([]string{"run", flag, fmt.Sprint(n)}, s.args[1:]...)
		res := run(t, s)
		if res.code != 0 && !strings.Contains(res.stderr, budgetErr) {
			t.Fatalf("%s fails without a budget error under %s %d\n%s", src, flag, n, res)
		}
		return res.code != 0
	}
	if fails(hi) {
		t.Fatalf("%s does not fit %s %d", src, flag, hi)
	}
	lo := 1
	for lo < hi {
		mid := (lo + hi) / 2
		if fails(mid) {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	return lo
}

// G4: each ergonomic form takes exactly the steps of what it means.
func TestPEStepsMatchTheHandWrittenForm(t *testing.T) {
	t.Parallel()
	const fn = "let f = fn(n) { if (n == 0) { 0 } else { f(n - 1) } };\n"
	for _, c := range []struct{ name, ergonomic, written string }{
		{"field", `let r = {"up": {"n": 1}}; r.up.n`, `let r = {"up": {"n": 1}}; r["up"]["n"]`},
		{"&&", "let a = 3; let b = 5; a <= b && b >= a", "let a = 3; let b = 5; if (a <= b) { b >= a } else { false }"},
		{"||", "let a = 3; a > 4 || a < 4", "let a = 3; if (a > 4) { true } else { a < 4 }"},
		{"short circuit", fn + "false && f(100)", fn + "if (false) { f(100) } else { false }"},
		{"interpolation", `let x = 12345; "n=${x}!${[x, true]}"`, `let x = 12345; "n=" + str(x) + "!" + str([x, true])`},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			const steps = "budget exceeded: steps"
			e := smallestLimit(t, c.ergonomic, "--steps", steps, 4096)
			w := smallestLimit(t, c.written, "--steps", steps, 4096)
			if e != w {
				t.Fatalf("%s takes %d steps, %s takes %d", c.ergonomic, e, c.written, w)
			}
			t.Logf("%d steps each", e)
		})
	}
}

// G4 and G6 for bytes: an interpolation of large values needs exactly the
// alloc_mb that its concatenation of str calls needs.
func TestPEAllocMatchesTheHandWrittenForm(t *testing.T) {
	t.Parallel()
	const build = "let d = fn(s, n) { if (n == 0) { s } else { d(s + s, n - 1) } };\n" +
		"let s = d(\"xyz\", 18);\nlet a = [s, s, s];\n"
	for _, c := range []struct{ name, ergonomic, written string }{
		{"strings", build + `puts(len("<${s}|${s}>"))`, build + `puts(len("<" + str(s) + "|" + str(s) + ">"))`},
		{"an array", build + `puts(len("${a}!"))`, build + `puts(len(str(a) + "!"))`},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			const alloc = "budget exceeded: alloc"
			e := smallestLimit(t, c.ergonomic, "--alloc-mb", alloc, 64)
			w := smallestLimit(t, c.written, "--alloc-mb", alloc, 64)
			if e != w || e < 2 {
				t.Fatalf("%s needs %d MiB, %s needs %d", c.ergonomic, e, c.written, w)
			}
			t.Logf("%d MiB each", e)
		})
	}
}

// An interpolation that runs out of alloc_mb is reported at its string,
// and nothing after it runs.
func TestPEInterpolationOutOfBytes(t *testing.T) {
	t.Parallel()
	src := "let d = fn(s, n) { if (n == 0) { s } else { d(s + s, n - 1) } };\n" +
		"let s = d(\"x\", 19);\nlet t = \"big: ${s}${s}${s}\";\nputs(\"after\")"
	s, path := progSpec(t, src, "--alloc-mb", "2")
	s.code = 1
	s.stderrHas = []string{"error: budget exceeded: alloc (limit 2097152 bytes) (at " + path + ":3:9)\n"}
	check(t, s)
}

// Each && || and . nests the tree one level: more than parser.MaxNesting
// is a parse error, not a crash. A long interpolation is a + chain, which
// does not nest.
func TestPENesting(t *testing.T) {
	t.Parallel()
	const n = 10_001
	for name, src := range map[string]string{
		"&& chain":    "true" + strings.Repeat(" && true", n),
		"|| chain":    "false" + strings.Repeat(" || false", n),
		"field chain": "let r = {};\nr" + strings.Repeat(".a", n),
		"in ${...}":   `"${` + strings.Repeat("(", n) + "1" + strings.Repeat(")", n) + `}"`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s, _ := progSpec(t, src)
			s.code, s.stderrHas, s.stderrNot = 1, []string{"error: parse error: nesting deeper than 10000 levels"}, goCrash
			check(t, s)
		})
	}
	t.Run("a long interpolation", func(t *testing.T) {
		t.Parallel()
		s, _ := progSpec(t, `let v = 7; puts(len("`+strings.Repeat("${v},", 50_000)+`"))`)
		s.stdout = "100000\n"
		check(t, s)
	})
}

var latency = regexp.MustCompile(`"latency_ms": ?\d+`)

// examples/uptime-ergonomic.arv, run by the binary with the grants its
// header names, writes the report uptime.arv writes and prints its line.
// The targets cover up, down by status (404, 500) and down by a refused
// connection (an effect error, restarted twice). Only the failing run's
// message differs.
func TestPEUptimeErgonomic(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name       string
		ok, bad    int // targets answering 200 and 404
		plainErr   string
		ergonomErr string
	}{
		{name: "under 5% down", ok: 38, bad: 1},
		{name: "over 5% down", ok: 10, bad: 2,
			plainErr: "more than 5% of the targets are down", ergonomErr: "4 of 14 targets are down, more than 5%"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			var urls []string
			for i := 0; i < c.ok; i++ {
				urls = append(urls, fmt.Sprintf("%s/ok?id=%s&n=%d", srv.URL, testID(t), i))
			}
			for i := 0; i < c.bad; i++ {
				urls = append(urls, fmt.Sprintf("%s/status/404?id=%s&n=%d", srv.URL, testID(t), i))
			}
			if c.plainErr != "" { // two more down: a 500, and a port nothing listens on
				urls = append(urls, srv.URL+"/flaky?fail=99&k=a&id="+testID(t), "http://127.0.0.1:1/refused")
			}
			outputs := map[string]string{}
			reports := map[string]string{}
			for _, script := range []string{"uptime.arv", "uptime-ergonomic.arv"} {
				dir := t.TempDir()
				mustWrite(t, filepath.Join(dir, "targets.txt"), strings.Join(urls, "\n"))
				if err := os.Mkdir(filepath.Join(dir, "out"), 0o755); err != nil {
					t.Fatal(err)
				}
				abs, err := filepath.Abs(filepath.Join("..", "examples", script))
				if err != nil {
					t.Fatal(err)
				}
				s := spec{dir: dir, args: []string{"run", "--allow", "fs_read=targets.txt", "--allow", "fs_write=out",
					"--allow", "net=127.0.0.1", abs}}
				msg := c.plainErr
				if script != "uptime.arv" {
					msg = c.ergonomErr
				}
				res := run(t, s)
				switch {
				case msg == "" && (res.code != 0 || res.stderr != ""):
					t.Fatalf("%s failed\n%s", script, res)
				case msg != "" && (res.code != 1 || !strings.HasPrefix(res.stderr, "error: "+msg+" (at ")):
					t.Fatalf("%s: want the error %q\n%s", script, msg, res)
				}
				report, err := os.ReadFile(filepath.Join(dir, "out", "report.json"))
				if err != nil {
					t.Fatal(err)
				}
				outputs[script] = res.stdout
				reports[script] = latency.ReplaceAllString(string(report), `"latency_ms":0`)
			}
			down := c.bad
			if c.plainErr != "" {
				down += 2
			}
			want := fmt.Sprintf(`{"down":%d,"total":%d}`+"\n", down, len(urls))
			if outputs["uptime.arv"] != want || outputs["uptime-ergonomic.arv"] != want {
				t.Fatalf("printed %q and %q, want %q", outputs["uptime.arv"], outputs["uptime-ergonomic.arv"], want)
			}
			if reports["uptime.arv"] != reports["uptime-ergonomic.arv"] {
				t.Fatalf("reports differ:\n%s\n%s", reports["uptime.arv"], reports["uptime-ergonomic.arv"])
			}
			if !strings.Contains(reports["uptime.arv"], `"status":404`) {
				t.Fatalf("report lacks the 404 target: %s", reports["uptime.arv"])
			}
		})
	}
}
