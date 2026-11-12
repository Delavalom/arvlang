package arv

import (
	"bytes"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

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
// 208 cuts of 60 programs (review round 2). Since review round 3 there is
// no exemption: which of two simultaneous failures came first is logged.
func TestTimedProgramsReplayAndResume(t *testing.T) {
	cases, cuts := 30, 8
	if testing.Short() {
		cases, cuts = 6, 4
	}
	for i := 0; i < cases; i++ {
		checkTimedSeed(t, int64(9000+i), cuts)
	}
}

// Review round 2 saw one resume of seed 40019 diverge, once in 66 runs
// (16r). ARV_LONG=1 sweeps every cut of it ARV_SWEEP_REPS times (default
// 20); a divergence saves its log (see saveDivergence).
func TestTimedSeed40019EveryCut(t *testing.T) {
	if os.Getenv("ARV_LONG") == "" {
		t.Skip("set ARV_LONG=1 to sweep seed 40019 at every cut")
	}
	reps := 20
	if n, err := strconv.Atoi(os.Getenv("ARV_SWEEP_REPS")); err == nil && n > 0 {
		reps = n
	}
	for range reps {
		checkTimedSeed(t, 40019, 0)
	}
}

// checkTimedSeed records the program seed writes, replays it, and resumes
// cuts of its log (every cut if cuts is 0).
func checkTimedSeed(t *testing.T, seed int64, cuts int) {
	t.Helper()
	g := &timedGen{r: rand.New(rand.NewSource(seed))}
	src := g.program()
	recorded, full, rerr := recordRun(t, src, Options{})
	if replayed, err := replayRun(t, src, full); fmt.Sprint(err) != fmt.Sprint(rerr) || replayed != recorded {
		t.Fatalf("seed %d: replay %v (recorded %v)\nprinted:\n%s\nrecorded:\n%s\nlog: %s\nprogram:\n%s", seed, err, rerr, replayed, recorded, saveDivergence(t, seed, 0, full), src)
	}
	lines := bytes.SplitAfter(full, []byte("\n"))
	lines = lines[:len(lines)-2] // the end line, and "" after the last newline
	step := 1
	if cuts > 0 {
		step = max(1, len(lines)/cuts)
	}
	for cut := 1 + g.r.Intn(step); cut <= len(lines); cut += step {
		crashed := bytes.Join(lines[:cut], nil)
		printed, resumed, serr := resumeRun(t, src, crashed, nil)
		var ae *Error
		if errorsAs(serr, &ae) && ae.Kind == runtime.KindReplay {
			t.Fatalf("seed %d, cut %d/%d: resume diverged: %v\ncut log: %s\nprogram:\n%s", seed, cut, len(lines), serr, saveDivergence(t, seed, cut, crashed), src)
		}
		if replayed, err := replayRun(t, src, resumed); fmt.Sprint(err) != fmt.Sprint(serr) || replayed != printed {
			t.Fatalf("seed %d, cut %d: replay of the resumed log: %v, %q; resumed %v, %q\nlog: %s\nprogram:\n%s", seed, cut, err, replayed, serr, printed, saveDivergence(t, seed, cut, resumed), src)
		}
	}
}

// saveDivergence keeps a log that failed, so the failure can be studied
// after the test: in ARV_DIVERGENCE_DIR, or else the system's temporary
// directory (not the test's, which is removed). It returns the path.
func saveDivergence(t *testing.T, seed int64, cut int, log []byte) string {
	dir := os.Getenv("ARV_DIVERGENCE_DIR")
	if dir == "" {
		dir = os.TempDir()
	}
	path := filepath.Join(dir, fmt.Sprintf("arv-divergence-seed%d-cut%d-%d.jsonl", seed, cut, time.Now().UnixNano()))
	if err := os.WriteFile(path, log, 0o600); err != nil {
		return "not saved: " + err.Error()
	}
	return path
}
