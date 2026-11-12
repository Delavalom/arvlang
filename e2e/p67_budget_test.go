package e2e

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// Budgets on replay and resume match the original, through the binary:
// three agents race for one tokens pool that holds two reservations, so
// which one is refused depends on the run's scheduling. The replay and
// the resume act out the logged outcome, whatever their own order.

// p67Race spawns three asks under tokens: 10000. Each reservation is
// about 4100 tokens (4096 of output and the prompt), so two fit at once.
const p67Race = "let h = " + haiku + `;
let a = fn(q) { match scope { ask(h, q) } { ok(v) => v, err(e) => e["kind"] + ": " + e["message"] } };
scope (tokens: 10000) {
  let ts = [spawn a("one") with [llm(h)], spawn a("two") with [llm(h)], spawn a("three") with [llm(h)]];
  for t in ts { puts(await t) }
}
`

// refused is the tasks whose llm_reserve record holds an error.
func refused(t *testing.T, path string) []string {
	t.Helper()
	var out []string
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, `"kind":"llm_reserve"`) && !strings.Contains(line, `"res":null`) {
			task, _, _ := strings.Cut(strings.TrimPrefix(line, `{"task":"`), `"`)
			out = append(out, task)
		}
	}
	return out
}

// raceAnswers holds every reply until the run at log has logged a
// refused reservation (so all three reserved before any settled), or
// hangs when hang is set.
func raceAnswers(log string, hang bool) func(string) apiReply {
	return func(body string) apiReply {
		deadline := time.Now().Add(gateTimeout)
		for len(refusedQuiet(log)) == 0 && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}
		if hang {
			return apiReply{hang: true}
		}
		for _, q := range []string{"one", "two", "three"} {
			if strings.Contains(body, `"text":"`+q+`"`) {
				return okText("answer "+q, 30, 7)
			}
		}
		return okText("unknown", 1, 1)
	}
}

// refusedQuiet is refused without a test, for the server's goroutines.
func refusedQuiet(path string) []string {
	data, _ := os.ReadFile(path)
	var out []string
	for _, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, `"kind":"llm_reserve"`) && strings.HasSuffix(line, "}") && !strings.Contains(line, `"res":null`) {
			out = append(out, line)
		}
	}
	return out
}

// wantRace checks out: each task's line in spawn order, the refused task
// (root/i) printing the budget error.
func wantRace(t *testing.T, out, task string) {
	t.Helper()
	lines := []string{"answer one", "answer two", "answer three"}
	var i int
	if _, err := fmt.Sscanf(task, "root/%d", &i); err != nil || i < 0 || i > 2 {
		t.Fatalf("refused task %q", task)
	}
	lines[i] = "budget: budget exceeded: tokens (limit 10000)"
	if want := strings.Join(lines, "\n") + "\n"; out != want {
		t.Fatalf("printed %q, want %q (task %s refused)", out, want, task)
	}
}

// Recorded, the race refuses one task; three replays print the same with
// no request, whichever task the replay's scheduling reaches first.
func TestP67RacingAgentsReplayTheirBudget(t *testing.T) {
	t.Parallel()
	r := newRecorded(t)
	rec, api := runLLMCase(t, llmCase{src: p67Race, respond: raceAnswers(r.log, false), anyStdout: true, calls: 2,
		path: r.path, dir: r.dir, args: []string{"--record", r.log, "--allow", allowLLM}})
	tasks := refused(t, r.log)
	if lines := readLogLines(t, r.log); len(tasks) != 1 || kinds(lines, "llm_reserve") != 3 || kinds(lines, "llm") != 2 {
		t.Fatalf("the log refuses %q; %d llm_reserve, %d llm records", tasks, kinds(lines, "llm_reserve"), kinds(lines, "llm"))
	}
	wantRace(t, rec.stdout, tasks[0])
	noKeyIn(t, r.log)
	for i := range 3 {
		rep := replayLog(t, r, api)
		if rep.code != 0 || rep.stdout != rec.stdout || len(api.requests()) != 2 {
			t.Fatalf("replay %d: %d requests\n%s", i, len(api.requests())-2, rep)
		}
	}
}

// Killed with both granted calls in flight (the refusal logged), the
// resume refuses the same task, sends exactly the two calls in flight
// again, byte for byte, and its log replays the same.
func TestP67RacingAgentsResumeTheirBudget(t *testing.T) {
	t.Parallel()
	r := newRecorded(t)
	mustWrite(t, r.path, p67Race)
	crashAPI := newLLMAPI(t, raceAnswers(r.log, true))
	cmd, out := startArv(t, r.dir, apiEnv(crashAPI), "run", "--record", r.log, "--fsync", "--allow", allowLLM, r.path)
	deadline := time.Now().Add(gateTimeout)
	for len(crashAPI.requests()) < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	for len(refusedQuiet(r.log)) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cmd.Process.Kill()
	cmd.Wait()
	tasks := refused(t, r.log)
	if lines := readLogLines(t, r.log); len(tasks) != 1 || kinds(lines, "llm") != 0 || len(crashAPI.requests()) != 2 {
		t.Fatalf("before the kill: %q printed, %q refused, %d llm records, %d requests",
			out.String(), tasks, kinds(lines, "llm"), len(crashAPI.requests()))
	}
	noKeyIn(t, r.log)
	api := newLLMAPI(t, raceAnswers(r.log, false))
	res := run(t, spec{args: []string{"run", "--resume", r.log, "--allow", allowLLM, r.path}, dir: r.dir, env: apiEnv(api), timeout: 90 * time.Second})
	if res.code != 0 || res.stderr != "" {
		t.Fatalf("resume\n%s", res)
	}
	wantRace(t, res.stdout, tasks[0])
	sent, crashed := api.requests(), crashAPI.requests()
	if len(sent) != 2 || !(sent[0] == crashed[0] && sent[1] == crashed[1] || sent[0] == crashed[1] && sent[1] == crashed[0]) {
		t.Fatalf("resume sent %d requests, want the two in flight\n%q\n%q", len(sent), sent, crashed)
	}
	noKeyIn(t, r.log)
	if got := refused(t, r.log); len(got) != 1 || got[0] != tasks[0] {
		t.Fatalf("the resumed log refuses %q, the killed one %q", got, tasks)
	}
	rep := replayLog(t, r, api)
	if rep.code != 0 || rep.stdout != res.stdout || len(api.requests()) != 2 {
		t.Fatalf("replay of the resumed log\n%s", rep)
	}
}
