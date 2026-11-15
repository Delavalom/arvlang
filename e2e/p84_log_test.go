package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Phase 8.4, S5 and I7 through the binary: a log recorded by one engine
// replays and resumes on the other. Every logged check falls on the same
// step on both engines, and the codec numbers long strings by identity,
// so a drift in either would show as a divergence.

// p84LogSrc has every kind of effect the CLI can answer from a log here
// (net, env, time, randomness, sleep), in the root and in tasks, long
// strings shared between values, and the Phase 5E forms.
const p84LogSrc = `let caps = [net("127.0.0.1")];
let long = "a string literal longer than sixteen bytes";
let get = fn(n) { let b = http_get("{{SERVER}}/ok?id={{ID}}&n=" + n)["body"]; {"n": n, "b": b, "long": long} };
let ts = for n in ["1", "2", "3"] { spawn get(n) with caps };
let rs = for t in ts { await t };
puts(for r in rs { "${r.n}:${r.b} ${r.long == long && len(r.long) >= 16}" });
puts([random(1000000), now() > 0, getenv("HOME")]);
sleep(5);
let h = {"k": long, "same": [long, long]};
puts(h.same[0] == h.k || false, random(1000))`

// replayOut is a replay's stderr without the line naming the grants.
func replayOut(stderr string) string {
	if strings.HasPrefix(stderr, "replay: grants") {
		if i := strings.IndexByte(stderr, '\n'); i >= 0 {
			return stderr[i+1:]
		}
	}
	return stderr
}

// recordThenReplay records src on engine rec in a fresh dir and replays
// it on engine rep; flags go to the recording (limits travel in the log).
func recordThenReplay(t *testing.T, rec, rep, src string, flags ...string) (record, replay result, log string) {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "prog.arv")
	mustWrite(t, script, expand(t, src, nil))
	log = filepath.Join(dir, "run.jsonl")
	args := append(append([]string{"run", "--record", log}, flags...), script)
	record = onEngine(t, rec, spec{args: args, dir: dir, env: []string{"HOME=/home/p84"}})
	replay = onEngine(t, rep, spec{args: []string{"run", "--replay", log, script}, dir: dir})
	return record, replay, log
}

// enginePairs are (recording engine, replaying engine): across engines
// both ways.
var enginePairs = [][2]string{{walkEngine, compiledEngine}, {compiledEngine, walkEngine}}

// A run recorded on one engine replays on the other: the same output,
// offline (no request reaches the server).
func TestP84ReplayOnTheOtherEngine(t *testing.T) {
	t.Parallel()
	for _, p := range enginePairs {
		t.Run(p[0]+"-to-"+p[1], func(t *testing.T) {
			t.Parallel()
			rec, rep, _ := recordThenReplay(t, p[0], p[1], p84LogSrc, "--allow", "net=127.0.0.1", "--allow", "env=HOME")
			if rec.code != 0 || rec.stderr != "" || hits(t, "/ok") != 3 {
				t.Fatalf("recording (%d requests)\n%s", hits(t, "/ok"), rec)
			}
			if rep.code != 0 || rep.stdout != rec.stdout || replayOut(rep.stderr) != "" || hits(t, "/ok") != 3 {
				t.Fatalf("replay on %s: want stdout %q, no request (got %d)\n%s", p[1], rec.stdout, hits(t, "/ok"), rep)
			}
		})
	}
}

// p84StepsSrc is cut by --steps between two effects of one task at a
// time (the tasks are awaited one by one, so the cut is deterministic).
const p84StepsSrc = `let f = fn(i) { for x in [1, 2, 3, 4, 5, 6, 7, 8, 9, 10] { let h = getenv("HOME"); puts("${h}${x * i}"); x } };
let rs = for i in [1, 2, 3] { await spawn f(i) with [env("HOME")] };
puts(rs)`

// S2 and S5: a run cut by --steps replays on the other engine to the
// same output and the same error at the same node: each logged check
// falls on the same step. (A shared budget spent by tasks running at
// once is not used here: which task runs out first is a race, and a
// replay can then diverge on either engine, a Phase 6 known limit in
// 16o-phase6-notes.md.)
func TestP84StepsCutReplaysOnTheOtherEngine(t *testing.T) {
	t.Parallel()
	for _, p := range enginePairs {
		for _, steps := range []string{"200", "437", "650"} {
			t.Run(p[0]+"-to-"+p[1]+"/"+steps, func(t *testing.T) {
				t.Parallel()
				rec, rep, _ := recordThenReplay(t, p[0], p[1], p84StepsSrc, "--steps", steps, "--allow", "env=HOME")
				if rec.code != 1 || !strings.Contains(rec.stderr, "budget exceeded: steps (limit "+steps+")") {
					t.Fatalf("recording: want the steps budget\n%s", rec)
				}
				if rep.code != rec.code || rep.stdout != rec.stdout || replayOut(rep.stderr) != rec.stderr {
					t.Fatalf("replay on %s differs\n=== recording ===\n%s\n=== replay ===\n%s", p[1], rec, rep)
				}
			})
		}
	}
}

// A log cut part way (a crash) resumes on the other engine, which runs
// the rest live; a replay of the resumed log on the first engine prints
// what the resume printed.
func TestP84ResumeOnTheOtherEngine(t *testing.T) {
	t.Parallel()
	for _, p := range enginePairs {
		t.Run(p[0]+"-to-"+p[1], func(t *testing.T) {
			t.Parallel()
			rec, _, log := recordThenReplay(t, p[0], p[1], p84LogSrc, "--allow", "net=127.0.0.1", "--allow", "env=HOME")
			if rec.code != 0 {
				t.Fatalf("recording\n%s", rec)
			}
			data, err := os.ReadFile(log)
			if err != nil {
				t.Fatal(err)
			}
			lines := strings.SplitAfter(string(data), "\n")
			if len(lines) < 6 {
				t.Fatalf("a log of %d lines", len(lines))
			}
			if err := os.WriteFile(log, []byte(strings.Join(lines[:len(lines)/2], "")), 0o600); err != nil {
				t.Fatal(err)
			}
			dir, script := filepath.Dir(log), filepath.Join(filepath.Dir(log), "prog.arv")
			env := []string{"HOME=/home/p84"}
			res := onEngine(t, p[1], spec{args: []string{"run", "--resume", log, "--allow", "net=127.0.0.1", "--allow", "env=HOME", script}, dir: dir, env: env})
			if res.code != 0 || res.stderr != "" || len(strings.Split(res.stdout, "\n")) != len(strings.Split(rec.stdout, "\n")) {
				t.Fatalf("resume on %s\n%s\nrecording:\n%s", p[1], res, rec)
			}
			rep := onEngine(t, p[0], spec{args: []string{"run", "--replay", log, script}, dir: dir})
			if rep.code != 0 || rep.stdout != res.stdout || replayOut(rep.stderr) != "" {
				t.Fatalf("replay of the resumed log on %s: want stdout %q\n%s", p[0], res.stdout, rep)
			}
		})
	}
}
