package check

import (
	"fmt"
	"math/rand"
	"os"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv"
)

// gen builds random programs in which every leaf effect runs: each
// wrapper evaluates its hole exactly once, and a spawn hands its task
// exactly the capabilities of the leaves inside it.
type gen struct {
	r     *rand.Rand
	leaf  int
	needs map[string]bool // the grants the leaves need, in --allow form
}

// leafExpr is one effect call with a literal argument, and its
// constructor for a spawn's with list.
func (g *gen) leafExpr() (call, constructor string) {
	g.leaf++
	n := g.r.Intn(4)
	switch g.r.Intn(3) {
	case 0:
		g.needs[fmt.Sprintf("env=ARV_P81_%d", n)] = true
		return fmt.Sprintf(`getenv("ARV_P81_%d")`, n), fmt.Sprintf(`env("ARV_P81_%d")`, n)
	case 1:
		g.needs[fmt.Sprintf("fs_read=r%d.txt", n)] = true
		return fmt.Sprintf(`read_file("r%d.txt")`, n), fmt.Sprintf(`fs_read("r%d.txt")`, n)
	default:
		g.needs[fmt.Sprintf("fs_write=w%d.txt", n)] = true
		return fmt.Sprintf(`write_file("w%d.txt", "x")`, n), fmt.Sprintf(`fs_write("w%d.txt")`, n)
	}
}

// expr returns an expression and the constructors of the leaves in it.
func (g *gen) expr(depth int) (string, []string) {
	if depth == 0 || g.r.Intn(4) == 0 {
		k := 1 + g.r.Intn(2)
		var calls, caps []string
		for i := 0; i < k; i++ {
			c, k := g.leafExpr()
			calls, caps = append(calls, c), append(caps, k)
		}
		return "len([" + strings.Join(calls, ", ") + "])", caps
	}
	h, caps := g.expr(depth - 1)
	with := "[" + strings.Join(caps, ", ") + "]"
	switch g.r.Intn(10) {
	case 0:
		return "scope { " + h + " }", caps
	case 1:
		return "scope (timeout_ms: 600000, steps: 100000000, tasks: 50) { " + h + " }", caps
	case 2:
		return "match scope (supervise: \"collect\") { await spawn (fn() { " + h + " })() with " + with +
			" } { ok(v) => v, err(e) => error(e[\"message\"]) }", caps
	case 3:
		return "if (true) { " + h + " } else { 0 }", caps
	case 4:
		return "(fn(x) { " + h + " })(1)", caps
	case 5:
		return "await spawn (fn() { " + h + " })() with " + with, caps
	case 6:
		return "len(for x in [1] { " + h + " })", caps
	case 7:
		return "{\"k\": " + h + "}[\"k\"]", caps
	case 8:
		return "scope (supervise: \"restart\", retries: 1, backoff_ms: 1) { await spawn (fn() { " + h + " })() with " + with + " }", caps
	default:
		return "match " + h + " { ok(v) => v, err(e) => error(e[\"message\"]) }", caps
	}
}

// Running a program with exactly the grants arv check reports never
// meets a capability denial (soundness), and leaving any one out does
// (every reported grant is used: these programs run every leaf).
func TestReportedGrantsSufficeAndAreNeeded(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	for n := 0; n < 4; n++ {
		if err := os.WriteFile(fmt.Sprintf("r%d.txt", n), []byte("data"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	r := rand.New(rand.NewSource(81))
	for i := 0; i < 300; i++ {
		g := &gen{r: r, needs: map[string]bool{}}
		src, _ := g.expr(1 + r.Intn(4))
		src += ";"
		rep, err := Source(src)
		if err != nil {
			t.Fatalf("%s: %v", src, err)
		}
		if rep.Notes != nil {
			t.Fatalf("%s: notes %v", src, rep.Notes)
		}
		var got []Entry
		for _, n := range rep.Needs {
			got = append(got, Entry{Text: n.String()})
		}
		if len(got) != len(g.needs) {
			t.Fatalf("%s: check needs %v, the leaves %v", src, got, g.needs)
		}
		if err := runWithEntries(src, got, dir); err != nil {
			t.Fatalf("%s\nwith the reported grants: %v", src, err)
		}
		for drop := range got {
			rest := append(append([]Entry(nil), got[:drop]...), got[drop+1:]...)
			err := runWithEntries(src, rest, dir)
			if err == nil || !strings.Contains(err.Error(), "capability denied") {
				t.Fatalf("%s\nwithout %s: %v, want a denial", src, got[drop].Text, err)
			}
		}
	}
}

func runWithEntries(src string, entries []Entry, cwd string) error {
	caps, err := Grants(entries, cwd)
	if err != nil {
		return err
	}
	_, err = arv.Run(src, arv.Options{Caps: caps})
	return err
}
