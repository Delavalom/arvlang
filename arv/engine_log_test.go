package arv

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/evaluator"
	"github.com/delavalom/arvlang/arv/runtime"
)

// A log is the same whichever engine wrote it or reads it (Phase 8.4, S2,
// review round 1): a log recorded by a tree-walker binary (every binary
// before Phase 8.4) is replayed and resumed by the compiled default, and
// the reverse. Each check a task logs (Task.Tick, every checkEvery steps)
// names the check by its ordinal, so it pins the step count: here a task
// that is cancelled inside a loop does an effect every pass, so a check
// that fell one step earlier or later on the other engine would sit
// between other records and the replay would diverge.

// engineLogLoop runs until it is cancelled, logging a getenv each pass.
const engineLogLoop = `let loop = fn() { for r in reps { for s in reps { unit(); getenv("HOME") } }; "never" };
`

// engineLogPrograms: effects in two tasks and the Phase 5E forms (same log
// on both engines), a task cancelled by its sibling's failure, and a loop
// that a timed scope ends, in a task and in the root.
func engineLogPrograms() []struct{ name, src, out string } {
	spin := spinSrc() + engineLogLoop
	return []struct{ name, src, out string }{
		{"effects", `let home = fn(i) { let v = getenv("HOME"); if (v != "" && i >= 0) { "${i}:${len(v) > 0}" } else { "none" } };
let w = fn(n) { for i in [0, 1, 2, 3, 4, 5, 6, 7, 8, 9] { if (i <= n || i >= 8) { home(i) } else { -i } } };
let r = scope { let ta = spawn w(3) with [env("HOME")]; let tb = spawn w(5) with [env("HOME")]; [await ta, await tb] };
puts(r); puts({"k": r[0][1]}.k); len(r)`,
			"[[0:true, 1:true, 2:true, 3:true, -4, -5, -6, -7, 8:true, 9:true], [0:true, 1:true, 2:true, 3:true, 4:true, 5:true, -6, -7, 8:true, 9:true]]\n1:true\n"},
		{"fail-fast", spin + `let boom = fn() { sleep(20); error("boom") };
puts(match scope { let ta = spawn loop() with [env("HOME")]; let tb = spawn boom(); [await ta, await tb] } { ok(v) => v, err(e) => e["message"] });
"end"`, "boom\n"},
		{"timed", spin + `puts(match scope (timeout_ms: 30) { let ta = spawn loop() with [env("HOME")]; await ta } { ok(v) => v, err(e) => e["message"] });
puts(match scope (timeout_ms: 30) { loop() } { ok(v) => v, err(e) => e["message"] });
"end"`, "budget exceeded: timeout (limit 30 ms)\nbudget exceeded: timeout (limit 30 ms)\n"},
	}
}

// recordOnEngine records src on one engine (walk: the tree-walker), up to
// three times until it prints out and, if check, logs a check.
func recordOnEngine(t *testing.T, src, out string, check, walk bool) ([]byte, error) {
	t.Helper()
	old := evaluator.SetTreeWalk(walk)
	defer evaluator.SetTreeWalk(old)
	for try := 1; ; try++ {
		printed, log, err := recordRun(t, src, Options{Caps: engineLogCaps(t)})
		if printed == out && (!check || bytes.Contains(log, []byte(`"check":`))) {
			return log, err
		}
		if try == 3 {
			t.Fatalf("walk=%v: recording printed %q (%v), want %q; check wanted %v\n%s", walk, printed, err, out, check, log)
		}
	}
}

func engineLogCaps(t *testing.T) []runtime.Capability {
	home, err := runtime.New(runtime.Env, "HOME")
	if err != nil {
		t.Fatal(err)
	}
	return []runtime.Capability{home}
}

// engineLogCuts are where a recorded log is cut for a resume: after its
// first record, half way, and just after its first logged check.
func engineLogCuts(log []byte) []int {
	lines := bytes.SplitAfter(log, []byte("\n"))
	records := len(lines) - 3 // header, end, and the empty tail
	cuts := []int{1, max(records/2, 1)}
	for i, l := range lines[1 : len(lines)-2] {
		if bytes.Contains(l, []byte(`"check":`)) {
			cuts = append(cuts, i+1)
			break
		}
	}
	return cuts
}

// prefix is log's header and its first n records.
func prefix(log []byte, n int) []byte {
	lines := bytes.SplitAfter(log, []byte("\n"))
	return bytes.Join(lines[:1+n], nil)
}

func TestLogsCrossEngines(t *testing.T) {
	for _, p := range engineLogPrograms() {
		check := p.name != "effects"
		for _, rec := range []bool{true, false} {
			play := !rec
			name := fmt.Sprintf("%s, recorded walk=%v, read walk=%v", p.name, rec, play)
			log, rerr := recordOnEngine(t, p.src, p.out, check, rec)
			onEngineDo(play, func() {
				if replayed, err := replayRun(t, p.src, log); fmt.Sprint(err) != fmt.Sprint(rerr) || replayed != p.out {
					t.Fatalf("%s: replay: %v, %q; recorded %v, %q", name, err, replayed, rerr, p.out)
				}
			})
			for _, n := range engineLogCuts(log) {
				var resumed []byte
				onEngineDo(play, func() {
					printed, whole, err := resumeRun(t, p.src, prefix(log, n), engineLogCaps(t))
					if fmt.Sprint(err) != fmt.Sprint(rerr) || printed != p.out {
						t.Fatalf("%s: resume after %d records: %v, %q; recorded %v, %q", name, n, err, printed, rerr, p.out)
					}
					resumed = whole
				})
				onEngineDo(rec, func() {
					if replayed, err := replayRun(t, p.src, resumed); fmt.Sprint(err) != fmt.Sprint(rerr) || replayed != p.out {
						t.Fatalf("%s: replay of the resume after %d records: %v, %q", name, n, err, replayed)
					}
				})
				if !check && !slices.Equal(logRecords(t, resumed), logRecords(t, log)) {
					t.Fatalf("%s: the resume after %d records logged\n%s\nthe recording\n%s", name, n, resumed, log)
				}
			}
		}
		if !check { // a program whose log does not depend on timing logs the same on both engines
			w, _ := recordOnEngine(t, p.src, p.out, false, true)
			c, _ := recordOnEngine(t, p.src, p.out, false, false)
			if !slices.Equal(logRecords(t, w), logRecords(t, c)) {
				t.Fatalf("%s: the tree-walker logged\n%s\ncompiled code\n%s", p.name, w, c)
			}
		}
	}
}

// onEngineDo runs f with the tree-walker on (walk) or off.
func onEngineDo(walk bool, f func()) {
	old := evaluator.SetTreeWalk(walk)
	defer evaluator.SetTreeWalk(old)
	f()
}

// logRecords is log's lines without what a second run changes (times, the
// hash chain that covers them, the header's start), sorted: tasks append
// in the order the scheduler runs them.
func logRecords(t *testing.T, log []byte) []string {
	t.Helper()
	var out []string
	for _, line := range strings.Split(strings.TrimSpace(string(log)), "\n") {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("%v: %s", err, line)
		}
		delete(m, "at")
		delete(m, "began")
		delete(m, "h")
		if h, ok := m["header"].(map[string]any); ok {
			delete(h, "started")
		}
		b, _ := json.Marshal(m)
		out = append(out, string(b))
	}
	slices.Sort(out)
	return out
}
