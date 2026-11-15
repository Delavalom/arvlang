package llm

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// escapedKey is testKey with its first byte as a JSON \u escape, so a
// byte search of the body does not find it but the decoded JSON holds it.
var escapedKey = fmt.Sprintf(`\u%04x`, testKey[0]) + testKey[1:]

// A reply with the key, escaped, in a decoded JSON object key is refused
// (mutation M26): a tool input's argument name, a top-level field or a
// nested object's key. Object keys do not reach Arv today, so this pins
// the defence in depth, both in holdsKey and through Complete.
func TestAnthropicReplyWithTheKeyInAnObjectKeyIsRefused(t *testing.T) {
	const usage = `,"usage":{"input_tokens":5,"output_tokens":2}}`
	for _, body := range []string{
		`{"content":[{"type":"tool_use","id":"t","name":"w","input":{"` + escapedKey + `":"v"}}],"stop_reason":"tool_use"` + usage,
		`{"content":[{"type":"tool_use","id":"t","name":"w","input":{"a":{"` + escapedKey + `":1}}}],"stop_reason":"tool_use"` + usage,
		`{"` + escapedKey + `":1,"content":[{"type":"text","text":"x"}],"stop_reason":"end_turn"` + usage,
	} {
		if strings.Contains(body, testKey) {
			t.Fatalf("%s: the key is not hidden from a byte search", body)
		}
		if !holdsKey([]byte(body), testKey) {
			t.Errorf("%s: holdsKey = false, want true", body)
		}
		if e := completeErr(t, body); !strings.Contains(e.Message, "API key") {
			t.Errorf("%s: message %q, want it to say the reply holds the API key", body, e.Message)
		}
	}
	// An escaped object key that is not the key is fine.
	srv := newAPIServer(t, 200, `{"content":[{"type":"tool_use","id":"t","name":"w","input":{"`+fmt.Sprintf(`\u%04x`, 'c')+`ity":"v"}}],"stop_reason":"tool_use"`+usage)
	if _, err := NewAnthropic(testKey, srv.URL).Complete(context.Background(), Request{Model: "m", MaxTokens: 1}); err != nil {
		t.Fatalf("Complete = %v, want a reply", err)
	}
}
