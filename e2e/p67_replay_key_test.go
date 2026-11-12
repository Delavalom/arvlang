package e2e

import (
	"strings"
	"testing"
	"time"
)

// I7 for agents through the binary, with the key set: runCLI builds the
// model host (hostLLM) before it reads --replay, so a replay run with
// ANTHROPIC_API_KEY in its environment has a live provider pointed at the
// fake. The replay must still print byte for byte what the recording
// printed and send nothing: the log answers every call (mutation M01 of
// Phase 6x7 sent each replayed call live as well, and no replay test had
// a key, so nothing saw it).
func TestP67ReplayWithTheKeySetIsOffline(t *testing.T) {
	t.Parallel()
	r := newRecorded(t)
	rec, api := runLLMCase(t, llmCase{src: p67Agent, script: p67Script, anyStdout: true, calls: 3, path: r.path, dir: r.dir,
		args: []string{"--record", r.log, "--allow", allowLLM, "--allow", "tool=weather", "--allow", "net=127.0.0.1",
			"--tokens", "100000", "--usd-cents", "100"},
		verify: wantArrivals(map[string]int{"w-Lisbon": 1, "w-Porto": 1, "notes": 0})})
	if !strings.HasPrefix(rec.stdout, "start\nBike: sunny\nrain\n") {
		t.Fatalf("recording\n%s", rec)
	}
	noKeyIn(t, r.log)
	rep := run(t, spec{args: []string{"run", "--replay", r.log, r.path}, dir: r.dir, timeout: 90 * time.Second,
		env: []string{"ARV_ANTHROPIC_URL=" + api.URL + "/v1/messages", "ANTHROPIC_API_KEY=" + e2eKey}})
	if n := len(api.requests()); n != 3 {
		t.Fatalf("the replay sent %d requests with the key set\n%s", n-3, rep)
	}
	if strings.Contains(rep.stdout+rep.stderr, e2eKey) || strings.Contains(rep.stderr, "panic") {
		t.Fatalf("the replay printed the key or crashed\n%s", rep)
	}
	if rep.code != 0 || rep.stdout != rec.stdout || rep.stderr != `replay: grants: llm("anthropic:*"), tool("weather"), net("127.0.0.1")`+"\n" {
		t.Fatalf("replay: want stdout %q\n%s", rec.stdout, rep)
	}
	wantArrivals(map[string]int{"w-Lisbon": 1, "w-Porto": 1, "notes": 0})(t, rep, api)
}
