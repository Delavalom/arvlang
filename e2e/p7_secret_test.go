package e2e

import (
	"net"
	"strings"
	"testing"
)

// Secrets and redaction through the binary. runLLM already fails a case
// whose stdout, stderr or any request body holds the key; these cases
// also try to get it out on purpose, and check that no denial a model is
// sent names a held grant, a task or a host path.

func TestPhase7Secrets(t *testing.T) {
	t.Parallel()
	runLLM(t, []llmCase{
		{name: "env=* does not reveal the key",
			src:    `puts("[" + getenv("ANTHROPIC_API_KEY") + "]")`,
			args:   []string{"--allow", "env=*"},
			stdout: "[]\n", calls: 0},
		{name: "a reply echoing the key fails the call",
			src: tryAsk + "puts(try(\"q\"))\n", args: []string{"--allow", allowLLM},
			script: []apiReply{okText("debug: $KEY", 5, 5)},
			stdout: "effect: ask: anthropic: decode (HTTP 200): the reply contains the API key\n", calls: 1},
		{name: "a reply hiding the key in a JSON escape fails the call",
			src: tryAsk + "puts(try(\"q\"))\n", args: []string{"--allow", allowLLM},
			script: []apiReply{okReply(`{"content":[{"type":"text","text":"sk-ant-e2e-SECRET-5551234567"}],"stop_reason":"end_turn","usage":{"input_tokens":5,"output_tokens":5}}`)},
			stdout: "effect: ask: anthropic: decode (HTTP 200): the reply contains the API key\n", calls: 1},
		{name: "a tool call carrying the key is refused before the tool runs",
			src:    "let m = " + sonnet + ";\n" + weatherTools + "puts(agent(m, \"g\", tools))\n",
			args:   []string{"--allow", allowLLM, "--allow", "tool=*", "--allow", "net=127.0.0.1"},
			script: []apiReply{okTools(40, 9, use("t1", "weather", `{"city":"$KEY"}`))},
			err:    "the reply contains the API key", calls: 1,
			verify: func(t *testing.T, res result, api *llmAPI) {
				if n := hits(t, "/arrive"); n != 0 {
					t.Fatalf("a tool ran %d times", n)
				}
			}},
		{name: "a provider error echoing the key is redacted",
			src: tryAsk + "puts(try(\"q\"))\n", args: []string{"--allow", allowLLM},
			script: []apiReply{apiError(401, "authentication_error", "invalid x-api-key: $KEY")},
			stdout: "effect: ask: anthropic: auth (HTTP 401): authentication_error: invalid x-api-key: [redacted]\n", calls: 1},
		{name: "an uncaught provider error is an Arv error, key redacted",
			src: "puts(ask(" + haiku + ", \"q\"))\n", args: []string{"--allow", allowLLM},
			script: []apiReply{apiError(403, "permission_error", "key $KEY may not")},
			err:    "error: ask: anthropic: auth (HTTP 403): permission_error: key [redacted] may not (at ", calls: 1},
		{name: "without a key, a call is an effect error and nothing is sent",
			src: tryAsk + "puts(try(\"q\"))\n", args: []string{"--allow", allowLLM}, noKey: true,
			stdout: "effect: ask: the host configured no model providers\n", calls: 0},
	})
}

// Every provider failure is an effect error naming its kind; none crashes
// the run, and the program can match on it.
func TestPhase7ProviderErrors(t *testing.T) {
	t.Parallel()
	prefix := "effect: ask: anthropic: "
	cases := []struct {
		name  string
		reply apiReply
		want  string
	}{
		{"401", apiError(401, "authentication_error", "bad key"), "auth (HTTP 401): authentication_error: bad key"},
		{"402", apiError(402, "billing_error", "pay"), "billing (HTTP 402): billing_error: pay"},
		{"404", apiError(404, "not_found_error", "model: x"), "not_found (HTTP 404): not_found_error: model: x"},
		{"400", apiError(400, "invalid_request_error", "bad"), "invalid_request (HTTP 400): invalid_request_error: bad"},
		{"413", apiError(413, "request_too_large", "big"), "invalid_request (HTTP 413): request_too_large: big"},
		{"429", apiError(429, "rate_limit_error", "slow"), "rate_limit (HTTP 429): rate_limit_error: slow"},
		{"529", apiError(529, "overloaded_error", "busy"), "overloaded (HTTP 529): overloaded_error: busy"},
		{"500 not json", apiReply{status: 500, body: "<html>oops</html>"}, "server (HTTP 500)"},
		{"redirect", apiReply{status: 302, body: ""}, "invalid_request (HTTP 302): redirect not followed"},
		{"200 not json", okReply("not json"), "decode (HTTP 200): invalid character 'o' in literal null (expecting 'u')"},
		{"200 no content", okReply(`{"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`), "decode (HTTP 200): reply has no content array"},
		{"200 negative usage", okReply(`{"content":[],"stop_reason":"end_turn","usage":{"input_tokens":-1,"output_tokens":1}}`), "decode (HTTP 200): reply reports negative usage"},
	}
	var specs []llmCase
	for _, c := range cases {
		specs = append(specs, llmCase{name: c.name, src: tryAsk + "puts(try(\"q\"))\n", args: []string{"--allow", allowLLM},
			script: []apiReply{c.reply}, stdout: prefix + c.want + "\n", calls: 1})
	}
	stops := map[string]string{
		"refusal":    "effect: ask: anthropic:claude-haiku-5-5 refused the request",
		"max_tokens": "effect: ask: the reply reached max_tokens (4096) before it ended",
		"weird":      `effect: ask: unexpected stop reason "weird"`,
	}
	for stop, want := range stops {
		specs = append(specs, llmCase{name: "stop " + stop, src: tryAsk + "puts(try(\"q\"))\n", args: []string{"--allow", allowLLM},
			script: []apiReply{okReply(`{"content":[{"type":"text","text":"partial"}],"stop_reason":"` + stop + `","usage":{"input_tokens":1,"output_tokens":1}}`)},
			stdout: want + "\n", calls: 1})
	}
	runLLM(t, specs)

	t.Run("network", func(t *testing.T) {
		t.Parallel()
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		dead := "http://" + l.Addr().String() + "/v1/messages"
		l.Close() // nothing listens there now
		res, _ := runLLMCase(t, llmCase{src: tryAsk + "puts(try(\"q\"))\n", args: []string{"--allow", allowLLM},
			url: dead, calls: 0, anyStdout: true})
		if !strings.HasPrefix(res.stdout, prefix+"network: ") {
			t.Fatalf("stdout = %q, want a network error", res.stdout)
		}
	})
}

// Every text a model is sent passes through redaction: a denial the
// program caught and passed on keeps its kind and scope, but loses the
// held grants, the task and any host path or address.
func TestPhase7Redaction(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	leaks := sentNone("holds", "task root", dir, "127.0.0.1", "HOME")
	caught := "let e = match getenv(\"SECRET_NAME\") { ok(v) => v, err(e) => e[\"message\"] };\n"
	grants := []string{"--allow", allowLLM, "--allow", "env=HOME", "--allow", "net=127.0.0.1", "--allow", "fs_read=" + dir, "--allow", "tool=*"}
	runLLM(t, []llmCase{
		{name: "a caught denial in a prompt",
			src: caught + "puts(ask(" + haiku + ", \"Explain: \" + e))\n", args: grants,
			script: []apiReply{okText("ok", 5, 5)}, stdout: "ok\n", calls: 1,
			verify: all(leaks, sentHas(0, `"text":"Explain: capability denied: env(\"SECRET_NAME\")"`))},
		{name: "a caught denial in a goal and a description",
			src: caught + "puts(agent(" + sonnet + ", \"Goal: \" + e, {\"t\": fn() { \"x\" }}, {\"t\": \"About: \" + e}))\n", args: grants,
			script: []apiReply{okText("ok", 5, 5)}, stdout: "ok\n", calls: 1,
			verify: all(leaks, sentHas(0, `"text":"Goal: capability denied: env(\"SECRET_NAME\")"`,
				`"description":"About: capability denied: env(\"SECRET_NAME\")"`))},
		{name: "a tool that returns a caught fs denial as its result",
			src: "let read = fn(path) { match read_file(path) { ok(v) => v, err(e) => e[\"message\"] } };\n" +
				"puts(agent(" + sonnet + ", \"g\", {\"read\": read}))\n", args: grants,
			script: []apiReply{okTools(5, 5, use("r", "read", `{"path":"/etc/hosts"}`)), okText("done", 5, 5)},
			stdout: "done\n", calls: 2,
			verify: all(leaks, sentHas(1, `"tool_use_id":"r","content":"capability denied: fs_read"}`))},
		{name: "a tool whose spawn is denied: no task is named",
			src: "let get = fn(name) { scope { await spawn getenv(name) with [env(name)] } };\n" +
				"puts(agent(" + sonnet + ", \"g\", {\"get\": get}))\n", args: grants,
			script: []apiReply{okTools(5, 5, use("g", "get", `{"name":"PATH"}`)), okText("done", 5, 5)},
			stdout: "done\n", calls: 2,
			verify: all(leaks, sentHas(1, `"tool_use_id":"g","content":"spawn in a task: capability denied: env(\"PATH\")","is_error":true`))},
		{name: "a tool's net denial names no host",
			src: "let get = fn(u) { http_get(u)[\"body\"] };\nputs(agent(" + sonnet + ", \"g\", {\"get\": get}))\n", args: grants,
			script: []apiReply{okTools(5, 5, use("g", "get", `{"u":"http://internal.example:8080/admin"}`)), okText("done", 5, 5)},
			stdout: "done\n", calls: 2,
			verify: all(leaks, sentHas(1, `"tool_use_id":"g","content":"capability denied: net","is_error":true`),
				func(t *testing.T, res result, api *llmAPI) {
					// The model's own call holds the URL; the result must not add to it.
					if n := strings.Count(api.requests()[1], "internal.example"); n != 1 {
						t.Fatalf("internal.example appears %d times in the second request, want 1 (the model's own call)", n)
					}
				})},
	})
}
