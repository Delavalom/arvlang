# Phase 7, task p7.13: Anthropic wire format

> **For agentic workers:** this file is one task, p7.13. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: task p7.12 is committed in your tree. Start every shell command with `cd <your worktree> &&` and use absolute paths: the shell's working directory does not persist. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify` and never push; keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0` (toolchain 1.26.2): `t.Chdir`, `min` and `max` are available; do not use `testing/synctest` or `sync.WaitGroup.Go`. Before a long test run, check `df -h /System/Volumes/Data`; below 1 GB free, stop and report (only `go clean -cache` may be deleted). This machine is heavily loaded: keep wall-clock bounds generous. No test may reach the network or spend money: never set `ARV_LIVE_ANTHROPIC`, and never put a real API key anywhere.
>
> Every code block below was applied, in task order, to a scratch copy of `feat/phases-3-8` (Phases 3, 4 and 5 merged; Phase 6 is not) and passed `gofmt -l .`, `go vet ./...` and `go test -race ./...` (including `e2e/`). Design: `14-phase7-agents.md`. Deviations from it, and why: `p7-plan-1.md`.

### Task p7.13: Anthropic wire format

The Messages API's JSON, as checked against the `claude-api` skill: `encodeRequest` writes `model`, `max_tokens`, `system`, `messages` and `tools` (each with an `input_schema` object of required string properties and `additionalProperties: false`). A block with `Raw` set (anything the API sent) goes back exactly as received; a `tool_result` carries `tool_use_id`, its text as `content`, and `is_error` when true. `decodeResponse` keeps every block's raw JSON, reads `text` and `tool_use` blocks, and counts cache tokens as input. No network in this task.

**Files:**
- Create: `arv/llm/anthropic_wire.go`
- Create (test): `arv/llm/anthropic_wire_test.go`

- [ ] **Step 1: Write the failing tests**

Create `arv/llm/anthropic_wire_test.go`:

```go
package llm

import (
	"encoding/json"
	"testing"
)

// canonical is JSON text with object keys sorted, for comparing bodies.
func canonical(t *testing.T, data []byte) string {
	t.Helper()
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("not JSON: %s", data)
	}
	out, _ := json.Marshal(v)
	return string(out)
}

func TestEncodeRequest(t *testing.T) {
	thinking := json.RawMessage(`{"type":"thinking","thinking":"","signature":"sig"}`)
	req := Request{Model: "claude-haiku-5-5", MaxTokens: 100, Messages: []Message{
		{Role: RoleUser, Content: []Block{{Type: BlockText, Text: "Bike?"}}},
		{Role: RoleAssistant, Content: []Block{{Type: "thinking", Raw: thinking}, ToolUse("t1", "weather", `{"city":"Lisbon"}`)}},
		{Role: RoleUser, Content: []Block{{Type: BlockToolResult, ID: "t1", Text: "denied", IsError: true}, {Type: BlockToolResult, ID: "t2", Text: ""}}},
	}, Tools: []ToolDef{{Name: "weather", Description: "Weather now", Params: []string{"city"}}, {Name: "now"}}}
	body, err := encodeRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"max_tokens":100,"messages":[` +
		`{"content":[{"text":"Bike?","type":"text"}],"role":"user"},` +
		`{"content":[{"signature":"sig","thinking":"","type":"thinking"},{"id":"t1","input":{"city":"Lisbon"},"name":"weather","type":"tool_use"}],"role":"assistant"},` +
		`{"content":[{"content":"denied","is_error":true,"tool_use_id":"t1","type":"tool_result"},{"content":"","tool_use_id":"t2","type":"tool_result"}],"role":"user"}],` +
		`"model":"claude-haiku-5-5","tools":[` +
		`{"description":"Weather now","input_schema":{"additionalProperties":false,"properties":{"city":{"type":"string"}},"required":["city"],"type":"object"},"name":"weather"},` +
		`{"input_schema":{"additionalProperties":false,"properties":{},"required":[],"type":"object"},"name":"now"}]}`
	if got := canonical(t, body); got != want {
		t.Fatalf("body:\n%s\nwant\n%s", got, want)
	}
	// A system prompt is sent when set, and no field Arv does not use.
	body, _ = encodeRequest(Request{Model: "m", MaxTokens: 1, System: "Be brief.", Messages: []Message{}})
	if got := canonical(t, body); got != `{"max_tokens":1,"messages":null,"model":"m","system":"Be brief."}` {
		t.Fatalf("body = %s", got)
	}
	if _, err := encodeRequest(Request{Messages: []Message{{Role: RoleUser, Content: []Block{{Type: "image"}}}}}); err == nil {
		t.Fatal("a block Arv cannot build was encoded")
	}
}

func TestDecodeResponse(t *testing.T) {
	body := `{"id":"msg_1","content":[{"type":"thinking","thinking":"","signature":"s"},{"type":"text","text":"Checking."},` +
		`{"type":"tool_use","id":"t1","name":"weather","input":{"city":"Lisbon"}}],"stop_reason":"tool_use",` +
		`"usage":{"input_tokens":10,"output_tokens":7,"cache_read_input_tokens":3,"cache_creation_input_tokens":2}}`
	r, err := decodeResponse([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if r.StopReason != StopToolUse || r.InputTokens != 15 || r.OutputTokens != 7 || len(r.Content) != 3 || r.Text() != "Checking." {
		t.Fatalf("response = %+v", r)
	}
	use := r.Content[2]
	if use.Type != BlockToolUse || use.ID != "t1" || use.Name != "weather" || string(use.Input) != `{"city":"Lisbon"}` {
		t.Fatalf("tool_use = %+v", use)
	}
	// Every block goes back exactly as it came.
	if string(r.Content[0].Raw) != `{"type":"thinking","thinking":"","signature":"s"}` ||
		string(r.Content[2].Raw) != `{"type":"tool_use","id":"t1","name":"weather","input":{"city":"Lisbon"}}` {
		t.Fatalf("Raw = %s, %s", r.Content[0].Raw, r.Content[2].Raw)
	}
	again, _ := encodeRequest(Request{Messages: []Message{{Role: RoleAssistant, Content: r.Content}}})
	if want := `{"model":"","max_tokens":0,"messages":[{"role":"assistant","content":[` +
		`{"type":"thinking","thinking":"","signature":"s"},{"type":"text","text":"Checking."},` +
		`{"type":"tool_use","id":"t1","name":"weather","input":{"city":"Lisbon"}}]}]}`; string(again) != want {
		t.Fatalf("re-encoded:\n%s\nwant\n%s", again, want)
	}
	for _, bad := range []string{`{"content": 5}`, `{"stop_reason":"end_turn"}`, `nope`, `{"content":[5]}`} {
		if _, err := decodeResponse([]byte(bad)); err == nil {
			t.Errorf("decodeResponse(%s) succeeded", bad)
		}
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test -run '^(TestEncodeRequest|TestDecodeResponse)$' ./arv/llm/`

Expected: FAIL: the package does not compile. The output includes:

```text
# github.com/delavalom/arvlang/arv/llm [github.com/delavalom/arvlang/arv/llm.test]
arv/llm/anthropic_wire_test.go:26:15: undefined: encodeRequest
arv/llm/anthropic_wire_test.go:41:12: undefined: encodeRequest
arv/llm/anthropic_wire_test.go:45:15: undefined: encodeRequest
```

- [ ] **Step 3: Implement**

Create `arv/llm/anthropic_wire.go`:

```go
package llm

import (
	"encoding/json"
	"errors"
	"fmt"
)

var errNoContent = errors.New("reply has no content array")

// The Messages API's JSON, as much of it as Arv sends and reads.
type (
	apiRequest struct {
		Model     string       `json:"model"`
		MaxTokens int          `json:"max_tokens"`
		System    string       `json:"system,omitempty"`
		Messages  []apiMessage `json:"messages"`
		Tools     []apiTool    `json:"tools,omitempty"`
	}
	apiMessage struct {
		Role    string            `json:"role"`
		Content []json.RawMessage `json:"content"`
	}
	apiTool struct {
		Name        string    `json:"name"`
		Description string    `json:"description,omitempty"`
		InputSchema apiSchema `json:"input_schema"`
	}
	apiSchema struct {
		Type                 string                       `json:"type"`
		Properties           map[string]map[string]string `json:"properties"`
		Required             []string                     `json:"required"`
		AdditionalProperties bool                         `json:"additionalProperties"`
	}
	apiBlock struct {
		Type      string          `json:"type"`
		Text      *string         `json:"text,omitempty"`
		ID        string          `json:"id,omitempty"`
		Name      string          `json:"name,omitempty"`
		Input     json.RawMessage `json:"input,omitempty"`
		ToolUseID string          `json:"tool_use_id,omitempty"`
		Content   *string         `json:"content,omitempty"`
		IsError   bool            `json:"is_error,omitempty"`
	}
	apiResponse struct {
		Content    []json.RawMessage `json:"content"`
		StopReason string            `json:"stop_reason"`
		Usage      struct {
			InputTokens              int `json:"input_tokens"`
			OutputTokens             int `json:"output_tokens"`
			CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
			CacheReadInputTokens     int `json:"cache_read_input_tokens"`
		} `json:"usage"`
	}
)

// encodeRequest is req as a Messages API body. A block that came from a
// reply goes back as it came (Raw); one Arv built is encoded from its
// fields. Every parameter is a required string, and no other is allowed.
func encodeRequest(req Request) ([]byte, error) {
	out := apiRequest{Model: req.Model, MaxTokens: req.MaxTokens, System: req.System}
	for _, m := range req.Messages {
		am := apiMessage{Role: m.Role, Content: make([]json.RawMessage, 0, len(m.Content))}
		for _, b := range m.Content {
			raw, err := encodeBlock(b)
			if err != nil {
				return nil, err
			}
			am.Content = append(am.Content, raw)
		}
		out.Messages = append(out.Messages, am)
	}
	for _, t := range req.Tools {
		s := apiSchema{Type: "object", Properties: map[string]map[string]string{}, Required: append([]string{}, t.Params...)}
		for _, p := range t.Params {
			s.Properties[p] = map[string]string{"type": "string"}
		}
		out.Tools = append(out.Tools, apiTool{Name: t.Name, Description: t.Description, InputSchema: s})
	}
	return json.Marshal(out)
}

func encodeBlock(b Block) (json.RawMessage, error) {
	if len(b.Raw) > 0 {
		return b.Raw, nil
	}
	switch b.Type {
	case BlockText:
		return json.Marshal(apiBlock{Type: BlockText, Text: &b.Text})
	case BlockToolUse:
		input := b.Input
		if len(input) == 0 {
			input = json.RawMessage("{}")
		}
		return json.Marshal(apiBlock{Type: BlockToolUse, ID: b.ID, Name: b.Name, Input: input})
	case BlockToolResult:
		return json.Marshal(apiBlock{Type: BlockToolResult, ToolUseID: b.ID, Content: &b.Text, IsError: b.IsError})
	}
	return nil, fmt.Errorf("cannot send a %q block", b.Type)
}

// decodeResponse reads a 200 reply. Every block keeps its JSON in Raw, to
// go back unchanged; text and tool_use blocks also fill their fields.
// Cache tokens count as input.
func decodeResponse(data []byte) (Response, error) {
	var r apiResponse
	if err := json.Unmarshal(data, &r); err != nil {
		return Response{}, err
	}
	if r.Content == nil {
		return Response{}, errNoContent
	}
	out := Response{
		StopReason:   r.StopReason,
		InputTokens:  r.Usage.InputTokens + r.Usage.CacheCreationInputTokens + r.Usage.CacheReadInputTokens,
		OutputTokens: r.Usage.OutputTokens,
	}
	for _, raw := range r.Content {
		var b apiBlock
		if err := json.Unmarshal(raw, &b); err != nil {
			return Response{}, err
		}
		blk := Block{Type: b.Type, Raw: raw}
		switch b.Type {
		case BlockText:
			if b.Text != nil {
				blk.Text = *b.Text
			}
		case BlockToolUse:
			blk.ID, blk.Name, blk.Input = b.ID, b.Name, b.Input
		}
		out.Content = append(out.Content, blk)
	}
	return out, nil
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test -race -run '^(TestEncodeRequest|TestDecodeResponse)$' ./arv/llm/`, then `gofmt -l .`, `go vet ./...` and `go test -race ./...`.
Expected: PASS everywhere.

- [ ] **Step 5: Commit**

```bash
git add arv/llm/anthropic_wire_test.go arv/llm/anthropic_wire.go
git commit -m "feat(llm): Anthropic Messages API wire format

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
