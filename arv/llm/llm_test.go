package llm

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
)

func TestParseModel(t *testing.T) {
	p, m, err := ParseModel("anthropic:claude-haiku-5-5")
	if err != nil || p != "anthropic" || m != "claude-haiku-5-5" {
		t.Fatalf("ParseModel = %q, %q, %v", p, m, err)
	}
	for _, bad := range []string{"anthropic:*", "*", "anthropic", "", "Anthropic:x"} {
		if _, _, err := ParseModel(bad); err == nil {
			t.Errorf("ParseModel(%q) succeeded", bad)
		}
	}
}

func TestErrorText(t *testing.T) {
	e := &Error{Provider: "anthropic", Kind: ErrRateLimit, Status: 429, Message: "slow down"}
	if got := e.Error(); got != "anthropic: rate_limit (HTTP 429): slow down" {
		t.Fatalf("Error() = %q", got)
	}
	long := &Error{Provider: "p", Kind: ErrNetwork, Message: strings.Repeat("x", 5000)}
	if n := len(long.Error()); n > maxErrorText+64 {
		t.Fatalf("Error() is %d bytes, want it clipped", n)
	}
	if runtime.ErrorKind(e, runtime.KindRuntime) != runtime.KindEffect {
		t.Fatal("a provider error must be of kind effect")
	}
}

func TestResponseText(t *testing.T) {
	r := Response{Content: []Block{{Type: BlockText, Text: "a"}, ToolUse("1", "t", "{}"), {Type: BlockText, Text: "b"}}}
	if r.Text() != "ab" {
		t.Fatalf("Text() = %q", r.Text())
	}
}

func TestFakeAnswersInOrderAndRecords(t *testing.T) {
	f := NewFake(TextReply("one", 3, 1), Reply{Err: errors.New("boom")})
	req := Request{Model: "m", MaxTokens: 10, Messages: []Message{{Role: RoleUser, Content: []Block{{Type: BlockText, Text: "hi"}}}}}
	r, err := f.Complete(context.Background(), req)
	if err != nil || r.Text() != "one" || r.InputTokens != 3 || r.OutputTokens != 1 {
		t.Fatalf("first = %+v, %v", r, err)
	}
	req.Messages[0].Content[0].Text = "changed" // the record is a copy
	if _, err := f.Complete(context.Background(), req); err == nil || err.Error() != "boom" {
		t.Fatalf("second err = %v", err)
	}
	if _, err := f.Complete(context.Background(), req); !errors.Is(err, ErrNoReply) {
		t.Fatalf("third err = %v, want ErrNoReply", err)
	}
	calls := f.Calls()
	if len(calls) != 3 || calls[0].Messages[0].Content[0].Text != "hi" || calls[1].Messages[0].Content[0].Text != "changed" {
		t.Fatalf("calls = %+v", calls)
	}
}

func TestFakeThenAndCancel(t *testing.T) {
	f := NewFake()
	f.Then = func(ctx context.Context, req Request) (Response, error) {
		return ToolUseReply(1, 1, ToolUse("t1", "weather", `{"city":"Lisbon"}`)).Response, nil
	}
	r, err := f.Complete(context.Background(), Request{})
	if err != nil || r.StopReason != StopToolUse || r.Content[0].Name != "weather" {
		t.Fatalf("Then reply = %+v, %v", r, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := f.Complete(ctx, Request{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled err = %v", err)
	}
	if len(f.Calls()) != 2 {
		t.Fatalf("calls = %d, want 2", len(f.Calls()))
	}
}
