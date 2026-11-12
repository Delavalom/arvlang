package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --replay of a run that called a model needs no key and sends nothing:
// the calls are in the log (llm_model, llm), and the key never is.
func TestCLIReplayOfModelCallsNeedsNoKey(t *testing.T) {
	api := newFakeAPI(t, 200, textReply("ok"))
	script := writeScript(t, `puts(ask("anthropic:claude-haiku-5-5", "Reply with the word ok")); puts("after")`)
	log := filepath.Join(t.TempDir(), "run.jsonl")
	code, out, stderr := cli("run", "--allow", "llm=anthropic:*", "--tokens", "100000", "--record", log, script)
	if code != 0 || out != "ok\nafter\n" || len(api.bodies) != 1 {
		t.Fatalf("record: exit %d, out %q, err %q, %d calls", code, out, stderr, len(api.bodies))
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	noKey(t, string(data))
	if !strings.Contains(string(data), `"tokens":100000`) {
		t.Fatalf("the header lacks the tokens limit: %s", data)
	}
	t.Setenv("ANTHROPIC_API_KEY", "")
	code, rout, stderr := cli("run", "--replay", log, script)
	if code != 0 || rout != out || len(api.bodies) != 1 || !strings.Contains(stderr, "replay: grants: llm(\"anthropic:*\")") {
		t.Fatalf("replay: exit %d, out %q, err %q, %d calls", code, rout, stderr, len(api.bodies))
	}
	// --manifest grants from the file, which a replay must not take.
	if code, _, stderr := cli("run", "--replay", log, "--manifest", script); code != 2 || !strings.Contains(stderr, "drop --manifest") {
		t.Fatalf("--replay --manifest: exit %d, err %q", code, stderr)
	}
	if code, _, stderr := cli("run", "--replay", log, "--tokens", "5", script); code != 2 || !strings.Contains(stderr, "drop the limit flags") {
		t.Fatalf("--replay --tokens: exit %d, err %q", code, stderr)
	}
}
