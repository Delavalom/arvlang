package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
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

// Usage is checked: a negative count is an error, and cache tokens added
// to input saturate instead of wrapping negative.
func TestDecodeResponseUsage(t *testing.T) {
	body := func(in, cache string) []byte {
		return []byte(`{"content":[],"stop_reason":"end_turn","usage":{"input_tokens":` + in + `,"output_tokens":1,"cache_read_input_tokens":` + cache + `}}`)
	}
	if _, err := decodeResponse(body("-5", "0")); err == nil {
		t.Fatal("negative input_tokens decoded")
	}
	if _, err := decodeResponse(body("1", "-5")); err == nil {
		t.Fatal("negative cache_read_input_tokens decoded")
	}
	r, err := decodeResponse(body("9223372036854775000", "9223372036854775000"))
	if err != nil || r.InputTokens != math.MaxInt {
		t.Fatalf("InputTokens = %d, %v; want math.MaxInt", r.InputTokens, err)
	}
}

func TestCostNothing(t *testing.T) {
	for err, want := range map[error]bool{
		&Error{Kind: ErrRateLimit, Status: 429}:                       true,
		&Error{Kind: ErrOverloaded, Status: 529}:                      true,
		&Error{Kind: ErrServer, Status: 500}:                          false,
		&Error{Kind: ErrServer, Status: 504}:                          false,
		&Error{Kind: ErrInvalid, Status: 307}:                         true,
		&Error{Kind: ErrInvalid}:                                      true,
		&Error{Kind: ErrNetwork}:                                      false,
		&Error{Kind: ErrNetwork, Status: 200}:                         false,
		&Error{Kind: ErrDecode, Status: 200}:                          false,
		ErrNoReply:                                                    false,
		context.Canceled:                                              false,
		fmt.Errorf("wrapped: %w", &Error{Kind: ErrAuth, Status: 401}): true,
	} {
		if got := CostNothing(err); got != want {
			t.Errorf("CostNothing(%v) = %v, want %v", err, got, want)
		}
	}
}
