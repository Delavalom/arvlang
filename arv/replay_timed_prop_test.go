package arv

import (
	"bytes"
	"fmt"
	"math/rand"
	"regexp"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
)

// timedGen writes random programs whose outcomes depend on time: timed
// scopes (some of which time out), scopes of one task at a time,
// restarts, collect, sleeps and pure work. Each task may fail once, and
// every scope's failure is caught, so the program goes on.
type timedGen struct {
	r   *rand.Rand
	fns []string
	n   int
}

func (g *timedGen) task(depth int) string {
	g.n++
	name := fmt.Sprintf("t%d", g.n)
	var b strings.Builder
	child := func() string {
		if depth >= 2 {
			return "leaf"
		}
		return g.task(depth + 1)
	}
	for i, n := 0, 1+g.r.Intn(4); i < n; i++ {
		switch k := g.r.Intn(9); k {
		case 0:
			fmt.Fprintf(&b, "puts(%q);\n", fmt.Sprintf("%s.%d", name, i))
		case 1:
			fmt.Fprintf(&b, "sleep(%d);\n", g.r.Intn(60))
		case 2:
			fmt.Fprintf(&b, "work();\n")
		case 3:
			fmt.Fprintf(&b, "let h%d = spawn %s(); puts(await h%d);\n", i, child(), i)
		case 4, 5:
			opts := []string{"timeout_ms: 20", "timeout_ms: 80", "timeout_ms: 250", "tasks: 1", `supervise: "collect"`, "tasks: 1, timeout_ms: 60"}[g.r.Intn(6)]
			fmt.Fprintf(&b, "puts(match scope (%s) { let x = spawn %s(); let y = spawn %s(); [await x, await y] } { ok(v) => v, err(e) => e[\"message\"] });\n", opts, child(), child())
		case 6:
			fmt.Fprintf(&b, "puts(match scope (supervise: \"restart\", retries: 2, backoff_ms: 5, retry_on: [\"user\"]) { await spawn flaky() } { ok(v) => v, err(e) => e[\"message\"] });\n")
		case 7:
			fmt.Fprintf(&b, "puts(match scope (timeout_ms: %d) { work(); sleep(%d); \"in time\" } { ok(v) => v, err(e) => e[\"message\"] });\n", 10+g.r.Intn(60), g.r.Intn(60))
		case 8:
			fmt.Fprintf(&b, "if (random(3) == 0) { error(%q) };\n", "boom in "+name)
		}
	}
	g.fns = append(g.fns, fmt.Sprintf("let %s = fn() {\n%s%q\n};\n", name, b.String(), name+" done"))
	return name
}

const timedPrelude = `let a = [1, 2, 3, 4, 5, 6, 7, 8, 9, 10];
let work = fn() { for x in a { for y in a { for z in a { for w in a { 1 } } } }; 0 };
let leaf = fn() { sleep(random(40)); work(); "leaf" };
let flaky = fn() { if (random(2) == 0) { error("flaky") }; "steady" };
`

func (g *timedGen) program() string {
	top := g.task(0)
	return timedPrelude + strings.Join(g.fns, "") +
		fmt.Sprintf("puts(match scope (timeout_ms: 400) { await spawn %s() } { ok(v) => v, err(e) => e[\"message\"] })\n", top)
}

// I7 for timing: every recorded run of a timed program replays
// identically, and every cut of its log (a crash after any line) resumes
// without diverging and leaves a log that replays identically to the
// resumed run. The resumed run's outcome itself may differ from the
// recording's: its live part runs on this run's timing. Before deadlines
// were part of the log, a timing-heavy probe like this one diverged on
// 208 cuts of 60 programs (review round 2).
func TestTimedProgramsReplayAndResume(t *testing.T) {
	cases, cuts := 30, 8
	if testing.Short() {
		cases, cuts = 6, 4
	}
	for i := 0; i < cases; i++ {
		g := &timedGen{r: rand.New(rand.NewSource(int64(9000 + i)))}
		src := g.program()
		recorded, full, rerr := recordRun(t, src, Options{})
		if replayed, err := replayRun(t, src, full); (fmt.Sprint(err) != fmt.Sprint(rerr) || replayed != recorded) && !failureRace(err) {
			t.Fatalf("case %d: replay %v (recorded %v)\nprinted:\n%s\nrecorded:\n%s\nprogram:\n%s", i, err, rerr, replayed, recorded, src)
		}
		lines := bytes.SplitAfter(full, []byte("\n"))
		lines = lines[:len(lines)-2] // the end line, and "" after the last newline
		step := max(1, len(lines)/cuts)
		for cut := 1 + g.r.Intn(step); cut <= len(lines); cut += step {
			crashed := bytes.Join(lines[:cut], nil)
			printed, resumed, serr := resumeRun(t, src, crashed, nil)
			var ae *Error
			if errorsAs(serr, &ae) && ae.Kind == runtime.KindReplay {
				t.Fatalf("case %d, cut %d/%d: resume diverged: %v\nprogram:\n%s", i, cut, len(lines), serr, src)
			}
			if replayed, err := replayRun(t, src, resumed); (fmt.Sprint(err) != fmt.Sprint(serr) || replayed != printed) && !failureRace(err) {
				t.Fatalf("case %d, cut %d: replay of the resumed log: %v, %q; resumed %v, %q\nprogram:\n%s", i, cut, err, replayed, serr, printed, src)
			}
		}
	}
}

// raceOfFailures is a replay that reached another first failure of a
// fail_fast scope: two of its tasks failed at once, and the replay's
// timing picked the other. That is a known limit (16o, "some outcomes
// are detected, not reproduced"), not a deadline's: the replay reports
// it as a divergence and prints nothing wrong.
var raceOfFailures = regexp.MustCompile(`the log has puts\("boom in t\d+"\), the program asked for puts\("boom in t\d+"\)`)

func failureRace(err error) bool {
	return err != nil && raceOfFailures.MatchString(err.Error())
}
