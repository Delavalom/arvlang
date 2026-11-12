package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
)

const cliKey = "sk-ant-cli-SECRET-9876543210"

// fakeAPI is a local stand-in for the Messages API: it answers the n-th
// request with replies[n] (the last one again after that) and keeps every
// request's body and API key.
type fakeAPI struct {
	*httptest.Server
	mu     sync.Mutex
	bodies []string
	keys   []string
}

func newFakeAPI(t *testing.T, status int, replies ...string) *fakeAPI {
	t.Helper()
	f := &fakeAPI{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		n := len(f.bodies)
		f.bodies = append(f.bodies, string(data))
		f.keys = append(f.keys, r.Header.Get("x-api-key"))
		f.mu.Unlock()
		w.WriteHeader(status)
		io.WriteString(w, strings.ReplaceAll(replies[min(n, len(replies)-1)], "$KEY", r.Header.Get("x-api-key")))
	}))
	t.Cleanup(f.Close)
	t.Setenv("ANTHROPIC_API_KEY", cliKey)
	t.Setenv("ARV_ANTHROPIC_URL", f.URL+"/v1/messages")
	return f
}

func textReply(text string) string {
	b, _ := json.Marshal(text)
	return `{"content":[{"type":"text","text":` + string(b) + `}],"stop_reason":"end_turn","usage":{"input_tokens":9,"output_tokens":2}}`
}

// noKey fails if the key shows up in any of texts.
func noKey(t *testing.T, texts ...string) {
	t.Helper()
	for _, s := range texts {
		if strings.Contains(s, cliKey) {
			t.Fatalf("the API key leaked into %q", s)
		}
	}
}

func TestCLIAsk(t *testing.T) {
	api := newFakeAPI(t, 200, textReply("ok"))
	script := writeScript(t, `puts(ask("anthropic:claude-haiku-5-5", "Reply with the word ok"))`)
	code, out, stderr := cli("run", "--allow", "llm=anthropic:*", script)
	if code != 0 || out != "ok\n" {
		t.Fatalf("exit %d, out %q, err %q", code, out, stderr)
	}
	if len(api.keys) != 1 || api.keys[0] != cliKey {
		t.Fatal("the request did not carry the key")
	}
	noKey(t, out, stderr, api.bodies[0])
}

// The program cannot read the key, even holding every env grant: the CLI
// removed it from the environment before the run.
func TestCLIKeyIsNotInTheEnvironment(t *testing.T) {
	newFakeAPI(t, 200, textReply("ok"))
	script := writeScript(t, `puts("[" + getenv("ANTHROPIC_API_KEY") + "]")`)
	code, out, stderr := cli("run", "--allow", "env=*", script)
	if code != 0 || out != "[]\n" {
		t.Fatalf("exit %d, out %q, err %q", code, out, stderr)
	}
	if os.Getenv("ANTHROPIC_API_KEY") != "" {
		t.Fatal("the key is still in the environment")
	}
}

// A provider error is an Arv error with a kind, and holds no key even
// when the server echoes it back.
func TestCLIProviderErrorHidesTheKey(t *testing.T) {
	newFakeAPI(t, 401, `{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key: $KEY"}}`)
	script := writeScript(t, `match ask("anthropic:claude-haiku-5-5", "hi") { ok(v) => v, err(e) => puts(e["kind"] + ": " + e["message"]) }`)
	code, out, stderr := cli("run", "--allow", "llm=anthropic:*", script)
	if code != 0 || !strings.HasPrefix(out, "effect: ask: anthropic: auth (HTTP 401): authentication_error: invalid x-api-key: [redacted]") {
		t.Fatalf("exit %d, out %q, err %q", code, out, stderr)
	}
	noKey(t, out, stderr)
}

// An agent end to end through the real provider code: a tool the task
// does not hold is refused as a tool result, the model sees no grant the
// task holds, and the key appears nowhere but in the request header.
func TestCLIAgentDeniedTool(t *testing.T) {
	dir := t.TempDir()
	api := newFakeAPI(t, 200,
		`{"content":[{"type":"tool_use","id":"t1","name":"notes","input":{"path":"/etc/passwd"}}],"stop_reason":"tool_use","usage":{"input_tokens":30,"output_tokens":9}}`,
		textReply("I may not read notes."))
	script := writeScript(t, `let tools = {"notes": fn(path) { read_file(path) }, "weather": fn(city) { "sunny" }};
puts(agent("anthropic:claude-sonnet-5-5", "Read my notes", tools))`)
	code, out, stderr := cli("run", "--allow", "llm=anthropic:*", "--allow", "tool=weather", "--allow", "fs_read="+dir, script)
	if code != 0 || out != "I may not read notes.\n" {
		t.Fatalf("exit %d, out %q, err %q", code, out, stderr)
	}
	if len(api.bodies) != 2 || !strings.Contains(api.bodies[1], `"content":"capability denied: tool(\"notes\")","is_error":true`) {
		t.Fatalf("second request = %s", api.bodies[len(api.bodies)-1])
	}
	for _, b := range api.bodies {
		if strings.Contains(b, dir) || strings.Contains(b, "holds") {
			t.Fatalf("a request names a held grant: %s", b)
		}
	}
	noKey(t, append([]string{out, stderr}, api.bodies...)...)
}

// A 200 reply that echoes the key fails the call; the key reaches no
// output (review round 1).
func TestCLIReplyEchoingTheKeyFails(t *testing.T) {
	newFakeAPI(t, 200, textReply("debug: $KEY"))
	script := writeScript(t, `puts(ask("anthropic:claude-haiku-5-5", "hi"))`)
	code, out, stderr := cli("run", "--allow", "llm=anthropic:*", script)
	noKey(t, out, stderr)
	if code == 0 || !strings.Contains(stderr, "the reply contains the API key") {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, out, stderr)
	}
}
