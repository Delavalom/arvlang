package arv

import (
	"bytes"
	"context"
	"fmt"
	"math/rand"
	"slices"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
)

// progGen writes small random Arv programs: trees of spawned tasks that
// print, sleep, fetch from a local server, draw random numbers, open
// nested (sometimes collect) scopes, and at most once fail.
type progGen struct {
	r       *rand.Rand
	url     string
	fns     []string
	n       int
	failure bool // whether the program may fail once
	failed  bool
}

func (g *progGen) task(depth int) string {
	g.n++
	name := fmt.Sprintf("t%d", g.n)
	var b strings.Builder
	for i, n := 0, 1+g.r.Intn(5); i < n; i++ {
		switch k := g.r.Intn(8); {
		case k == 0:
			fmt.Fprintf(&b, "puts(%q);\n", fmt.Sprintf("%s.%d", name, i))
		case k == 1:
			fmt.Fprintf(&b, "sleep(%d);\n", g.r.Intn(4))
		case k == 2:
			fmt.Fprintf(&b, "puts(http_get(%q)[\"body\"]);\n", fmt.Sprintf("%s/%s/%d", g.url, name, i))
		case k == 3:
			fmt.Fprintf(&b, "puts(random(1000));\n")
		case k <= 5 && depth < 3:
			child := g.task(depth + 1)
			fmt.Fprintf(&b, "let h%d = spawn %s() with [net(\"127.0.0.1\")];\n", i, child)
			if g.r.Intn(2) == 0 {
				fmt.Fprintf(&b, "puts(await h%d);\n", i)
			}
		case k == 6 && depth < 3:
			a, c := g.task(depth+1), g.task(depth+1)
			opts := ""
			if g.r.Intn(3) == 0 {
				opts = `(supervise: "collect") `
			}
			fmt.Fprintf(&b, "puts(match scope %s{ let x = spawn %s() with [net(\"127.0.0.1\")]; let y = spawn %s() with [net(\"127.0.0.1\")]; [await x, await y] } { ok(v) => v, err(e) => e[\"message\"] });\n", opts, a, c)
		case k == 7 && g.failure && !g.failed && g.r.Intn(2) == 0:
			g.failed = true
			fmt.Fprintf(&b, "error(%q);\n", "boom in "+name)
		}
	}
	g.fns = append(g.fns, fmt.Sprintf("let %s = fn() {\n%s%q\n};\n", name, b.String(), name+" done"))
	return name
}

func (g *progGen) program() string {
	top := g.task(0)
	return strings.Join(g.fns, "") + fmt.Sprintf("puts(await spawn %s() with [net(\"127.0.0.1\")])\n", top)
}

// I7 property: every recorded run replays identically, output and result,
// with no request reaching the server. Each case has its own server,
// closed before the replay (Close waits for the requests a cancelled task
// left in flight): a live request would fail and change the output.
func TestReplayProperty(t *testing.T) {
	cases := 200
	if testing.Short() {
		cases = 40
	}
	for i := 0; i < cases; i++ {
		srv, hits := countedServer(t)
		g := &progGen{r: rand.New(rand.NewSource(int64(i))), url: srv.URL, failure: i%2 == 1}
		src := g.program()
		recorded, log, rerr := recordRun(t, src, Options{Caps: loopback})
		srv.Close()
		before := hits.Load()
		replayed, err := replayRun(t, src, log)
		if fmt.Sprint(err) != fmt.Sprint(rerr) || replayed != recorded || hits.Load() != before {
			t.Fatalf("case %d: replay %v (recorded %v), %d requests\nprinted:\n%s\nrecorded:\n%s\nprogram:\n%s",
				i, err, rerr, hits.Load()-before, replayed, recorded, src)
		}
	}
}

// Differential property: a run cut off after a random number of records
// and resumed ends as an uninterrupted run does, prints the same lines
// (their order across tasks is the scheduler's), and leaves a log that
// replays identically.
func TestResumeProperty(t *testing.T) {
	srv, _ := countedServer(t)
	cases := 60
	if testing.Short() {
		cases = 15
	}
	for i := 0; i < cases; i++ {
		g := &progGen{r: rand.New(rand.NewSource(int64(1000 + i))), url: srv.URL}
		src := g.program()
		whole, full, err := recordRun(t, src, Options{Caps: loopback})
		if err != nil {
			t.Fatalf("case %d: %v\n%s", i, err, src)
		}
		n := bytes.Count(full, []byte("\n")) - 2 // records, header and end excluded
		ctx, cancel := context.WithCancel(context.Background())
		crash := &crashAfter{kind: "", n: 1 + g.r.Intn(max(n, 1)), cancel: cancel}
		Run(src, Options{Ctx: ctx, Caps: loopback, Record: crash, Out: &bytes.Buffer{}})
		cancel()
		l, err := runtime.ReadLog(bytes.NewReader(crash.buf.Bytes()))
		if err != nil {
			t.Fatalf("case %d: crashed log: %v", i, err)
		}
		var out bytes.Buffer
		log := bytes.NewBuffer(crash.buf.Bytes()[:l.Size])
		if _, err := Run(src, Options{Caps: loopback, Resume: l, Record: log, Out: &out}); err != nil {
			t.Fatalf("case %d: resume after %d records: %v\n%s", i, crash.n, err, src)
		}
		if sorted(out.String()) != sorted(whole) {
			t.Fatalf("case %d: resumed run printed\n%s\nuninterrupted\n%s", i, out.String(), whole)
		}
		if replayed, err := replayRun(t, src, log.Bytes()); err != nil || replayed != out.String() {
			t.Fatalf("case %d: replay of the resumed log: %v\n%s\nwant\n%s", i, err, replayed, out.String())
		}
	}
}

// sorted is s's lines, sorted, except random numbers, which differ
// between two live runs.
func sorted(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if l != "" && strings.Trim(l, "0123456789") == "" {
			lines[i] = "N"
		}
	}
	slices.Sort(lines)
	return strings.Join(lines, "\n")
}
