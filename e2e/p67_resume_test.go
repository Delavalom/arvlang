package e2e

import (
	"strings"
	"testing"
	"time"
)

// SIGKILL in the middle of an agent run, then --resume: the resumed run
// prints what an uninterrupted run prints, sends the provider only the
// call that was in flight (byte for byte the request the uninterrupted
// run sent) and the calls after it, and repeats no tool effect the log
// holds. A replay of the resumed log then sends nothing.

// weatherTurns answers an agent's first turn with one weather call for
// Lisbon and its second with the final text.
func weatherTurns(body string) apiReply {
	if strings.Contains(body, `"tool_result"`) {
		return okText("Bike: sunny", 60, 5)
	}
	return okTools(40, 9, use("t1", "weather", `{"city":"Lisbon"}`))
}

// p67Kill is an agent whose tool waits at gate {{K}} of the server.
const p67Kill = "let m = " + sonnet + `;
let tools = {"weather": fn(city) { http_get("{{SERVER}}/wait?id={{ID}}&k={{K}}&n=1")["body"] + " in " + city }};
puts("start");
puts(agent(m, "Weather in Lisbon?", tools));
puts("end")
`

// p67KillGrants are the grants; a recording also sets a tokens limit,
// which a resume takes from the log (it refuses limit flags).
var p67KillGrants = []string{"--allow", allowLLM, "--allow", "tool=weather", "--allow", "net=127.0.0.1"}

func apiEnv(api *llmAPI) []string {
	return []string{"ARV_ANTHROPIC_URL=" + api.URL + "/v1/messages", "ANTHROPIC_API_KEY=" + e2eKey}
}

// uninterrupted runs p67Kill to its end, its gate open.
func uninterrupted(t *testing.T) (result, *llmAPI) {
	t.Helper()
	arrive(testID(t), "open")
	return runLLMCase(t, llmCase{src: strings.ReplaceAll(p67Kill, "{{K}}", "open"), respond: weatherTurns,
		args: append([]string{"--tokens", "100000"}, p67KillGrants...), stdout: "start\nBike: sunny\nend\n", calls: 2})
}

// killAgent records p67Kill at gate "shut" against api, and kills the run
// once ready says so. It returns what the run printed.
func killAgent(t *testing.T, r recorded, api *llmAPI, ready func() bool) string {
	t.Helper()
	mustWrite(t, r.path, expand(t, p67Kill, map[string]string{"K": "shut"}))
	args := append(append([]string{"run", "--record", r.log, "--fsync", "--tokens", "100000"}, p67KillGrants...), r.path)
	cmd, out := startArv(t, r.dir, apiEnv(api), args...)
	deadline := time.Now().Add(gateTimeout)
	for !ready() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cmd.Process.Kill()
	cmd.Wait()
	api.drain(t)
	if !ready() {
		t.Fatalf("the run was killed before it got there:\n%s", out)
	}
	noKeyIn(t, r.log)
	return out.String()
}

// resumeAgent resumes r against a provider that answers with weatherTurns.
func resumeAgent(t *testing.T, r recorded) (result, *llmAPI) {
	t.Helper()
	api := newLLMAPI(t, weatherTurns)
	res := run(t, spec{args: append(append([]string{"run", "--resume", r.log}, p67KillGrants...), r.path), dir: r.dir,
		env: apiEnv(api), timeout: 90 * time.Second})
	if res.code != 0 || res.stderr != "" || strings.Contains(res.stdout, e2eKey) {
		t.Fatalf("resume\n%s", res)
	}
	noKeyIn(t, r.log)
	return res, api
}

// Killed while the model's second turn is in flight: the resume sends
// that turn again, byte for byte, and nothing else; the tool, whose
// request the log holds, does not run again.
func TestP67ResumeAfterSIGKILLInAModelCall(t *testing.T) {
	t.Parallel()
	ref, refAPI := uninterrupted(t)
	arrive(testID(t), "shut")
	r := newRecorded(t)
	crashAPI := newLLMAPI(t, func(body string) apiReply {
		if strings.Contains(body, `"tool_result"`) {
			return apiReply{hang: true}
		}
		return weatherTurns(body)
	})
	if out := killAgent(t, r, crashAPI, func() bool { return len(crashAPI.requests()) == 2 }); out != "start\n" {
		t.Fatalf("before the kill: %q", out)
	}
	lines := readLogLines(t, r.log)
	if kinds(lines, "llm") != 1 || kinds(lines, "http_get") != 1 || lines[len(lines)-1].End != nil {
		t.Fatalf("the killed run's log: %d llm, %d http_get records", kinds(lines, "llm"), kinds(lines, "http_get"))
	}
	res, api := resumeAgent(t, r)
	sent := api.requests()
	if res.stdout != ref.stdout || len(sent) != 1 || sent[0] != crashAPI.requests()[1] || sent[0] != refAPI.requests()[1] {
		t.Fatalf("resume: %d requests, want the one in flight\n%s", len(sent), res)
	}
	if n := hits(t, "/wait"); n != 2 {
		t.Fatalf("the tool ran %d times, want 2 (the uninterrupted run and the killed one)", n)
	}
	rep := replayLog(t, r, api)
	if rep.code != 0 || rep.stdout != ref.stdout || len(api.requests()) != 1 || hits(t, "/wait") != 2 {
		t.Fatalf("replay of the resumed log\n%s", rep)
	}
}

// Killed while the tool's request is in flight: the resume sends no
// model call the log holds, makes the tool's request again, and then
// sends the second turn as the uninterrupted run sent it.
func TestP67ResumeAfterSIGKILLInATool(t *testing.T) {
	t.Parallel()
	ref, refAPI := uninterrupted(t)
	r := newRecorded(t)
	crashAPI := newLLMAPI(t, weatherTurns)
	if out := killAgent(t, r, crashAPI, func() bool { return hits(t, "/wait") == 2 }); out != "start\n" {
		t.Fatalf("before the kill: %q", out)
	}
	lines := readLogLines(t, r.log)
	if kinds(lines, "llm") != 1 || kinds(lines, "http_get") != 0 || len(crashAPI.requests()) != 1 {
		t.Fatalf("the killed run's log: %d llm, %d http_get records", kinds(lines, "llm"), kinds(lines, "http_get"))
	}
	arrive(testID(t), "shut")
	res, api := resumeAgent(t, r)
	sent := api.requests()
	if res.stdout != ref.stdout || len(sent) != 1 || sent[0] != refAPI.requests()[1] || hits(t, "/wait") != 3 {
		t.Fatalf("resume: %d requests, %d tool requests\n%s", len(sent), hits(t, "/wait"), res)
	}
	rep := replayLog(t, r, api)
	if rep.code != 0 || rep.stdout != ref.stdout || len(api.requests()) != 1 || hits(t, "/wait") != 3 {
		t.Fatalf("replay of the resumed log\n%s", rep)
	}
}
