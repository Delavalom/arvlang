package llm

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// completeErr sends one request to a server that answers 200 with body
// and returns the *Error the call failed with, or fails the test.
func completeErr(t *testing.T, body string) *Error {
	t.Helper()
	srv := newAPIServer(t, 200, body)
	r, err := NewAnthropic(testKey, srv.URL).Complete(context.Background(), Request{Model: "m", MaxTokens: 1})
	var e *Error
	if !errors.As(err, &e) || e.Kind != ErrDecode || e.Status != 200 || strings.Contains(err.Error(), testKey) {
		t.Fatalf("%s: Complete = %+v, %v; want a decode error without the key", body, r, err)
	}
	return e
}

// A key split across blocks is refused too (review round 2): ask returns
// the text blocks joined, and an agent's tool gets its arguments, which
// the program can join, so the halves would make the key in Arv.
func TestAnthropicReplySplittingTheKeyIsRefused(t *testing.T) {
	half := len(testKey) / 2
	a, b := testKey[:half], testKey[half:]
	const usage = `,"usage":{"input_tokens":5,"output_tokens":2}}`
	for _, body := range []string{
		`{"content":[{"type":"text","text":"` + a + `"},{"type":"text","text":"` + b + `"}],"stop_reason":"end_turn"` + usage,
		`{"content":[{"type":"text","text":"x` + a + `"},{"type":"thinking","thinking":"","signature":"s"},{"type":"text","text":"` + b + `y"}],"stop_reason":"end_turn"` + usage,
		`{"content":[{"type":"text","text":"` + a + `"},{"type":"tool_use","id":"t","name":"w","input":{"city":"` + b + `"}}],"stop_reason":"tool_use"` + usage,
		`{"content":[{"type":"tool_use","id":"t","name":"w","input":{"x":"` + a + `","y":"` + b + `"}}],"stop_reason":"tool_use"` + usage,
		`{"content":[{"type":"tool_use","id":"t","name":"w","input":{"x":"` + a + `"}},{"type":"tool_use","id":"u","name":"w","input":{"x":"` + b + `"}}],"stop_reason":"tool_use"` + usage,
	} {
		if e := completeErr(t, body); !strings.Contains(e.Message, "API key") {
			t.Errorf("%s: message %q, want it to say the reply holds the API key", body, e.Message)
		}
	}
}

// A 200 reply without usage, or with zero input tokens (no prompt the
// API accepts is empty), is refused as unreadable (review round 2), so
// the call keeps its whole reservation instead of settling to zero.
func TestAnthropicReplyWithoutUsageIsRefused(t *testing.T) {
	for _, body := range []string{
		`{"content":[{"type":"text","text":"x"}],"stop_reason":"end_turn"}`,
		`{"content":[{"type":"text","text":"x"}],"stop_reason":"end_turn","usage":null}`,
		`{"content":[{"type":"text","text":"x"}],"stop_reason":"end_turn","usage":{}}`,
		`{"content":[{"type":"text","text":"x"}],"stop_reason":"end_turn","usage":{"input_tokens":0,"output_tokens":3}}`,
	} {
		if e := completeErr(t, body); !strings.Contains(e.Message, "usage") {
			t.Errorf("%s: message %q, want it to name the usage", body, e.Message)
		}
	}
	// Cache tokens alone are input.
	srv := newAPIServer(t, 200, `{"content":[],"stop_reason":"end_turn","usage":{"cache_read_input_tokens":4}}`)
	if r, err := NewAnthropic(testKey, srv.URL).Complete(context.Background(), Request{Model: "m"}); err != nil || r.InputTokens != 4 {
		t.Fatalf("Complete = %+v, %v; want 4 input tokens", r, err)
	}
}
