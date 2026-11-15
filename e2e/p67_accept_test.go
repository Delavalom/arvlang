package e2e

import (
	"os"
	"strings"
	"testing"
	"time"
)

// The acceptance tests of 13-phase6-durable-replay.md and the replay row
// of 14-phase7-agents.md, for agent runs, through the binary: every
// Phase 7 acceptance case recorded and replayed offline, an edited goal
// diverging, a crash after 3 of 5 model calls resumed, and a restarted
// agent replayed in attempt order.

// recordLLMCase records c in a fresh directory and replays it with no
// key and no grants. The replay must end as the recording did, byte for
// byte, send the provider nothing and run no tool; c.verify (tool
// arrivals, request bodies) must hold again after it.
func recordLLMCase(t *testing.T, c llmCase) (recorded, result, result, *llmAPI) {
	t.Helper()
	r := newRecorded(t)
	c.path, c.dir = r.path, r.dir
	c.args = append([]string{"--record", r.log}, c.args...)
	rec, api := runLLMCase(t, c)
	noKeyIn(t, r.log)
	rep := replayLog(t, r, api)
	grants, ended, _ := strings.Cut(rep.stderr, "\n")
	if rep.code != rec.code || rep.stdout != rec.stdout || ended != rec.stderr || !strings.HasPrefix(grants, "replay: grants: ") {
		t.Fatalf("replay differs\nrecording:\n%s\nreplay:\n%s", rec, rep)
	}
	if n := len(api.requests()); c.calls >= 0 && n != c.calls {
		t.Fatalf("the replay sent %d requests", n-c.calls)
	}
	if strings.Contains(rep.stdout+rep.stderr, e2eKey) {
		t.Fatalf("the replay printed the key\n%s", rep)
	}
	if c.verify != nil {
		c.verify(t, rep, api)
	}
	return r, rec, rep, api
}

// 14-phase7-agents.md, "replay (with Phase 6): 0 provider calls, same
// answer", for every other row of that table: each ends the same when
// replayed, a refused budget and a denied tool included.
func TestP67Phase7AcceptanceReplays(t *testing.T) {
	t.Parallel()
	for _, c := range phase7Cases() {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			recordLLMCase(t, c)
		})
	}
}

// An edited goal diverges: the error names the task, the effect and
// both model requests, and the replay sends nothing.
func TestP67EditedGoalDiverges(t *testing.T) {
	t.Parallel()
	src := "let m = " + sonnet + ";\n" + weatherTools + "puts(agent(m, \"Weather in Lisbon?\", tools))\n"
	r, _, _, api := recordLLMCase(t, llmCase{src: src, calls: 2, stdout: "Bike: sunny\n",
		args:   []string{"--allow", allowLLM, "--allow", "tool=weather", "--allow", "net=127.0.0.1"},
		script: []apiReply{okTools(40, 9, use("t1", "weather", `{"city":"Lisbon"}`)), okText("Bike: sunny", 60, 5)},
		verify: wantArrivals(map[string]int{"w-Lisbon": 1})})
	mustWrite(t, r.path, expand(t, strings.Replace(src, "in Lisbon?", "in Porto?", 1), nil))
	rep := replayLog(t, r, api)
	for _, want := range []string{"has changed since the log was recorded", "replay diverged at task root, effect ",
		"Weather in Lisbon?", "Weather in Porto?"} {
		if !strings.Contains(rep.stderr, want) {
			t.Errorf("stderr lacks %q", want)
		}
	}
	if rep.code != 1 || rep.stdout != "" || len(api.requests()) != 2 || gateArrivals(t, "w-Lisbon") != 1 || strings.Contains(rep.stderr, e2eKey) {
		t.Fatalf("%d requests\n%s", len(api.requests()), rep)
	}
}

// p67Five asks five questions in a row under a tokens limit; the fourth
// waits at the provider in the run that is killed.
const p67Five = "let h = " + haiku + ";\nfor q in [\"q1\", \"q2\", \"q3\", \"q4\", \"q5\"] { puts(ask(h, q)) }\nputs(\"end\")\n"

// fiveAnswers answers each question with its own name, or hangs on q4
// when hang is set.
func fiveAnswers(hang bool) func(string) apiReply {
	return func(body string) apiReply {
		for _, q := range []string{"q1", "q2", "q3", "q4", "q5"} {
			if strings.Contains(body, `"text":"`+q+`"`) {
				if hang && q == "q4" {
					return apiReply{hang: true}
				}
				return okText("answer "+q, 30, 7)
			}
		}
		return okText("unknown", 1, 1)
	}
}

// 13-phase6-durable-replay.md, "crash after 3 of 5 sequential effects",
// with model calls: killed with the fourth call in flight (its
// reservation logged, its reply not), the resume sends exactly the
// fourth and fifth requests, each as the uninterrupted run sent it, and
// prints what that run printed.
func TestP67ResumeAfterThreeOfFiveCalls(t *testing.T) {
	t.Parallel()
	want := "answer q1\nanswer q2\nanswer q3\nanswer q4\nanswer q5\nend\n"
	_, refAPI := runLLMCase(t, llmCase{src: p67Five, respond: fiveAnswers(false), stdout: want, calls: 5,
		args: []string{"--tokens", "100000", "--allow", allowLLM}})
	r := newRecorded(t)
	crashAPI := newLLMAPI(t, fiveAnswers(true))
	mustWrite(t, r.path, p67Five)
	cmd, out := startArv(t, r.dir, apiEnv(crashAPI), "run", "--record", r.log, "--fsync", "--tokens", "100000", "--allow", allowLLM, r.path)
	deadline := time.Now().Add(gateTimeout)
	for len(crashAPI.requests()) < 4 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cmd.Process.Kill()
	cmd.Wait()
	lines := readLogLines(t, r.log)
	if out.String() != "answer q1\nanswer q2\nanswer q3\n" || kinds(lines, "llm") != 3 || kinds(lines, "llm_reserve") != 4 {
		t.Fatalf("before the kill: %q, %d llm and %d llm_reserve records", out.String(), kinds(lines, "llm"), kinds(lines, "llm_reserve"))
	}
	noKeyIn(t, r.log)
	api := newLLMAPI(t, fiveAnswers(false))
	res := run(t, spec{args: []string{"run", "--resume", r.log, "--allow", allowLLM, r.path}, dir: r.dir, env: apiEnv(api), timeout: 90 * time.Second})
	sent, refSent := api.requests(), refAPI.requests()
	if res.code != 0 || res.stderr != "" || res.stdout != want || len(sent) != 2 || sent[0] != refSent[3] || sent[1] != refSent[4] {
		t.Fatalf("resume: %d requests, want q4 and q5 as first sent\n%s", len(sent), res)
	}
	noKeyIn(t, r.log)
	if lines := readLogLines(t, r.log); kinds(lines, "llm") != 5 || kinds(lines, "llm_reserve") != 5 {
		t.Fatalf("the resumed log: %d llm and %d llm_reserve records", kinds(lines, "llm"), kinds(lines, "llm_reserve"))
	}
	rep := replayLog(t, r, api)
	if rep.code != 0 || rep.stdout != want || len(api.requests()) != 2 {
		t.Fatalf("replay of the resumed log\n%s", rep)
	}
}

// 13-phase6-durable-replay.md, "replay with restarts", with an agent: a
// provider error fails the first attempt, restart runs it again, the log
// holds each attempt's calls under its own name, and the replay prints
// the same with no request.
func TestP67ReplayOfARestartedAgent(t *testing.T) {
	t.Parallel()
	src := "let m = " + sonnet + ";\n" + weatherTools + `let run = fn() { puts("attempt"); agent(m, "Weather in Lisbon?", tools) };
puts(scope (supervise: "restart", retries: 3, backoff_ms: 1, retry_on: ["effect"]) {
  await spawn run() with [llm(m), tool("weather"), net("127.0.0.1")]
})
`
	r, _, _, _ := recordLLMCase(t, llmCase{src: src, calls: 4, stdout: "attempt\nattempt\nBike: sunny\n",
		args: []string{"--allow", allowLLM, "--allow", "tool=weather", "--allow", "net=127.0.0.1"},
		script: []apiReply{okTools(40, 9, use("t1", "weather", `{"city":"Lisbon"}`)), {status: 500, body: "down"},
			okTools(40, 9, use("t1", "weather", `{"city":"Lisbon"}`)), okText("Bike: sunny", 60, 5)},
		verify: wantArrivals(map[string]int{"w-Lisbon": 2})})
	var tasks []string
	for _, l := range readLogLines(t, r.log) {
		if l.Kind == "llm" {
			tasks = append(tasks, l.Task)
		}
	}
	if strings.Join(tasks, " ") != "root/0 root/0 root/0#2 root/0#2" {
		t.Fatalf("model calls logged as %q", tasks)
	}
}

// A forged log whose model reply asks for a tool the agent does not
// hold: the replay still checks the grant, runs no tool, and diverges
// at the next model request (its tool result is a denial, not the
// recorded forecast), sending nothing.
func TestP67ForgedToolCallIsStillChecked(t *testing.T) {
	t.Parallel()
	r, _, _, api := recordLLMCase(t, llmCase{src: agentProg(`llm(m), tool("weather"), net("127.0.0.1")`), calls: 2, stdout: "Yes, bike.\n",
		args:   []string{"--allow", allowLLM, "--allow", "tool=*", "--allow", "net=127.0.0.1"},
		script: []apiReply{okTools(40, 9, use("t1", "weather", `{"city":"Lisbon"}`)), okText("Yes, bike.", 60, 4)},
		verify: wantArrivals(map[string]int{"w-Lisbon": 1, "notes": 0})})
	data, err := os.ReadFile(r.log)
	if err != nil {
		t.Fatal(err)
	}
	forged := strings.Replace(string(data), `"name":"weather","input"`, `"name":"notes","input"`, 1)
	if forged == string(data) {
		t.Fatalf("no tool call to forge in\n%s", data)
	}
	mustWrite(t, r.log, string(rechain([]byte(forged))))
	rep := replayLog(t, r, api)
	if rep.code != 1 || !strings.Contains(rep.stderr, "replay diverged") || len(api.requests()) != 2 {
		t.Fatalf("%d requests\n%s", len(api.requests()), rep)
	}
	wantArrivals(map[string]int{"w-Lisbon": 1, "notes": 0})(t, rep, api)
}
