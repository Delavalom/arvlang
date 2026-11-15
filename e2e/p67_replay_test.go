package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Phase 6x7 through the binary: an agent run is recorded, and a replay of
// it makes no provider call and runs no tool, without a key; no log ever
// holds the key.

// p67Agent uses an allowed tool twice and a tool it does not hold, asks
// once more under the same budget, and draws a random number and the time.
const p67Agent = "let m = " + sonnet + ";\n" + weatherTools + `puts("start");
puts(agent(m, "Weather in Lisbon and Porto?", tools));
puts(match scope { ask(m, "And tomorrow?") } { ok(v) => v, err(e) => e["kind"] });
puts(random(1000000)); puts(now());
`

var p67Script = []apiReply{
	okTools(40, 9, use("t1", "weather", `{"city":"Lisbon"}`), use("t2", "weather", `{"city":"Porto"}`), use("t3", "notes", `{"path":"/x"}`)),
	okText("Bike: sunny", 60, 5),
	okText("rain", 5, 5),
}

// recorded is where a recorded run keeps its log and program, and the
// directory it runs in, so its replay runs the same file in the same
// place (an error names the file; the log names the directory).
type recorded struct{ dir, log, path string }

func newRecorded(t *testing.T) recorded {
	dir := t.TempDir()
	return recorded{dir: dir, log: filepath.Join(dir, "run.jsonl"), path: filepath.Join(dir, "prog.arv")}
}

// replayLog replays r's log with no key and no grants, pointing
// ARV_ANTHROPIC_URL at api so any call would be counted.
func replayLog(t *testing.T, r recorded, api *llmAPI) result {
	t.Helper()
	api.drain(t) // what the recording sent is counted before the replay
	res := run(t, spec{args: []string{"run", "--replay", r.log, r.path}, dir: r.dir,
		env: []string{"ARV_ANTHROPIC_URL=" + api.URL + "/v1/messages"}, timeout: 90 * time.Second})
	if strings.Contains(res.stderr, "panic") || strings.Contains(res.stderr, "goroutine ") {
		t.Fatalf("the replay crashed\n%s", res)
	}
	api.drain(t)
	return res
}

// noKeyIn fails if the file at path holds the API key.
func noKeyIn(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), e2eKey) {
		t.Fatalf("the log holds the API key:\n%s", data)
	}
	return string(data)
}

// I7 for agents, through the binary: the replay prints byte for byte what
// the recording printed, needs no key and no --allow, and sends nothing:
// the provider sees no request and no tool reaches the server.
func TestP67ReplayOfAnAgentRunIsOffline(t *testing.T) {
	t.Parallel()
	r := newRecorded(t)
	rec, api := runLLMCase(t, llmCase{src: p67Agent, script: p67Script, anyStdout: true, calls: 3, path: r.path, dir: r.dir,
		args: []string{"--record", r.log, "--allow", allowLLM, "--allow", "tool=weather", "--allow", "net=127.0.0.1",
			"--tokens", "100000", "--usd-cents", "100"},
		verify: wantArrivals(map[string]int{"w-Lisbon": 1, "w-Porto": 1, "notes": 0})})
	if !strings.HasPrefix(rec.stdout, "start\nBike: sunny\nrain\n") {
		t.Fatalf("recording\n%s", rec)
	}
	data := noKeyIn(t, r.log)
	// One llm_model per ask or agent; one llm and one llm_reserve per call.
	for kind, n := range map[string]int{"llm_model": 2, "llm": 3, "llm_reserve": 3, "http_get": 2} {
		if got := strings.Count(data, `"kind":"`+kind+`","req"`); got != n {
			t.Errorf("the log has %d %s records, want %d", got, kind, n)
		}
	}
	rep := replayLog(t, r, api)
	if rep.code != 0 || rep.stdout != rec.stdout || rep.stderr != `replay: grants: llm("anthropic:*"), tool("weather"), net("127.0.0.1")`+"\n" {
		t.Fatalf("replay: want stdout %q\n%s", rec.stdout, rep)
	}
	if n := len(api.requests()); n != 3 {
		t.Fatalf("the replay sent %d requests", n-3)
	}
	wantArrivals(map[string]int{"w-Lisbon": 1, "w-Porto": 1, "notes": 0})(t, rep, api)
}

// Every Phase 7 way of getting the key out, recorded: the log holds no
// key, and a replay without one prints and ends the same and sends
// nothing.
func TestP67LogsNeverHoldTheKey(t *testing.T) {
	t.Parallel()
	agent := "let m = " + sonnet + ";\n" + weatherTools + "puts(agent(m, \"g\", tools))\n"
	cases := []llmCase{
		{name: "env=* reads no key", src: `puts("[" + getenv("ANTHROPIC_API_KEY") + "]")`, args: []string{"--allow", "env=*"}, stdout: "[]\n"},
		{name: "a reply echoing the key", src: tryAsk + "puts(try(\"q\"))\n", args: []string{"--allow", allowLLM},
			script: []apiReply{okText("debug: $KEY", 5, 5)}, calls: 1, anyStdout: true},
		{name: "a tool call carrying the key", src: agent, args: []string{"--allow", allowLLM, "--allow", "tool=*", "--allow", "net=127.0.0.1"},
			script: []apiReply{okTools(40, 9, use("t1", "weather", `{"city":"$KEY"}`))}, calls: 1, err: "the reply contains the API key"},
		{name: "a provider error echoing the key", src: tryAsk + "puts(try(\"q\"))\n", args: []string{"--allow", allowLLM},
			script: []apiReply{apiError(401, "authentication_error", "invalid x-api-key: $KEY")}, calls: 1, anyStdout: true},
		{name: "an uncaught provider error", src: "puts(ask(" + haiku + ", \"q\"))\n", args: []string{"--allow", allowLLM},
			script: []apiReply{apiError(403, "permission_error", "key $KEY may not")}, calls: 1, err: "[redacted]"},
		{name: "a server error echoing the key", src: tryAsk + "puts(try(\"q\"))\n", args: []string{"--allow", allowLLM},
			script: []apiReply{{status: 500, body: "boom $KEY"}}, calls: 1, anyStdout: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			r := newRecorded(t)
			c.path, c.dir = r.path, r.dir
			c.args = append([]string{"--record", r.log}, c.args...)
			rec, api := runLLMCase(t, c)
			noKeyIn(t, r.log)
			rep := replayLog(t, r, api)
			// The replay names the grants, then ends as the recording did.
			grants, ended, _ := strings.Cut(rep.stderr, "\n")
			if rep.code != rec.code || rep.stdout != rec.stdout || ended != rec.stderr ||
				!strings.HasPrefix(grants, "replay: grants: ") || len(api.requests()) != c.calls {
				t.Fatalf("replay: %d requests; recording\n%s\nreplay\n%s", len(api.requests())-c.calls, rec, rep)
			}
			if strings.Contains(rep.stdout+rep.stderr, e2eKey) {
				t.Fatalf("the replay printed the key\n%s", rep)
			}
		})
	}
}
