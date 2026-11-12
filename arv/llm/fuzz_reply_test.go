package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

// roundTrip answers every request in memory: no socket, no goroutine.
type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// fuzzProvider is an Anthropic whose replies come from status and body.
func fuzzProvider(status int, body []byte) *Anthropic {
	a := NewAnthropic(testKey, "http://fuzz.invalid/v1/messages")
	a.client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		r.Body.Close()
		return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(body)), Request: r}, nil
	})
	return a
}

var errorKinds = map[string]bool{ErrAuth: true, ErrBilling: true, ErrNotFound: true, ErrInvalid: true,
	ErrRateLimit: true, ErrOverloaded: true, ErrServer: true, ErrNetwork: true, ErrDecode: true}

// FuzzAnthropicReply feeds the provider arbitrary replies, the model's
// output on its way to a tool invocation: whatever the status and body,
// Complete returns either an *Error of a known kind that holds no key, or
// a response from which no key can reach Arv (its text, any block's
// fields or JSON, any string in a tool's input, or the text and tool
// arguments joined), that reports at least one input token, and that can
// be sent back on the next turn. $KEY in the body becomes the key, and
// $K1 and $K2 its halves, so the fuzzer can place it anywhere.
func FuzzAnthropicReply(f *testing.F) {
	f.Add(200, `{"content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":5,"output_tokens":1}}`)
	f.Add(200, `{"content":[{"type":"tool_use","id":"t","name":"weather","input":{"city":"Lisbon"}}],"stop_reason":"tool_use","usage":{"input_tokens":5,"output_tokens":1}}`)
	f.Add(200, `{"content":[{"type":"tool_use","id":"t","name":"w","input":{"a":"$K1","b":"$K2"}}],"stop_reason":"tool_use","usage":{"input_tokens":5,"output_tokens":1}}`)
	f.Add(200, `{"content":[{"type":"text","text":"$K1"},{"type":"thinking","thinking":""},{"type":"text","text":"$K2"}],"stop_reason":"end_turn","usage":{"input_tokens":5,"output_tokens":1}}`)
	f.Add(200, `{"content":[{"type":"text","text":"sk"}],"usage":{"input_tokens":1,"cache_read_input_tokens":3,"output_tokens":-1}}`)
	f.Add(200, `{"content":[{"TYPE":"tool_use","Input":{"x":["$KEY"]}}],"usage":{"input_tokens":1}}`)
	f.Add(200, `{"content":[null,{"type":"text"}],"usage":null}`)
	f.Add(401, `{"type":"error","error":{"type":"authentication_error","message":"bad $KEY"}}`)
	f.Add(302, ``)
	f.Add(529, `{"error":{"message":"$K1$K2"}}`)
	f.Add(500, `not json`)
	half := len(testKey) / 2
	keyed := strings.NewReplacer("$KEY", testKey, "$K1", testKey[:half], "$K2", testKey[half:])
	f.Fuzz(func(t *testing.T, status int, body string) {
		if status < 100 || status > 999 {
			status = 200 + (status%800+800)%800
		}
		data := []byte(keyed.Replace(body))
		resp, err := fuzzProvider(status, data).Complete(context.Background(), Request{Model: "m", MaxTokens: 16})
		if err != nil {
			var e *Error
			if !errors.As(err, &e) || e.Provider != "anthropic" || !errorKinds[e.Kind] {
				t.Fatalf("status %d: error %T %v is not a provider error of a known kind", status, err, err)
			}
			if e.Status != status {
				t.Fatalf("status %d: error has status %d", status, e.Status)
			}
			if strings.Contains(err.Error(), testKey) || strings.Contains(e.Message, testKey) {
				t.Fatalf("the error holds the key: %v", err)
			}
			return
		}
		if status != 200 {
			t.Fatalf("status %d was accepted", status)
		}
		if resp.InputTokens < 1 || resp.OutputTokens < 0 {
			t.Fatalf("usage %d in, %d out", resp.InputTokens, resp.OutputTokens)
		}
		var all strings.Builder
		reach := []string{resp.Text(), resp.StopReason}
		for _, b := range resp.Content {
			reach = append(reach, b.Text, b.Name, b.ID, string(b.Input), string(b.Raw))
			if b.Type == BlockText {
				all.WriteString(b.Text)
			}
			if b.Type == BlockToolUse && len(b.Input) > 0 {
				var v any
				if json.Unmarshal(b.Input, &v) == nil {
					reach = append(reach, strings.Join(jsonStrings(v), "\x00"))
				}
				appendValues(&all, b.Input)
			}
		}
		reach = append(reach, all.String())
		for _, s := range reach {
			if strings.Contains(s, testKey) {
				t.Fatalf("the key reached a response field %q\nbody %q", s, data)
			}
		}
		// The reply goes back unchanged on the next turn.
		req := Request{Model: "m", Messages: []Message{{Role: RoleAssistant, Content: resp.Content}}}
		if _, err := encodeRequest(req); err != nil {
			t.Fatalf("the reply cannot be sent back: %v\nbody %q", err, data)
		}
	})
}

// jsonStrings is every string and object key in v.
func jsonStrings(v any) []string {
	switch v := v.(type) {
	case string:
		return []string{v}
	case []any:
		var out []string
		for _, e := range v {
			out = append(out, jsonStrings(e)...)
		}
		return out
	case map[string]any:
		var out []string
		for k, e := range v {
			out = append(append(out, k), jsonStrings(e)...)
		}
		return out
	}
	return nil
}
