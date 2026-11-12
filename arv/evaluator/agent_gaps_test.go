package evaluator

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/llm"
	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// An ask or agent the task does not hold is a capability denial even when
// the host configured no model providers: the grant is checked before
// anything about the host is revealed (I1, mutation M52).
func TestUnheldModelWithoutAHostIsDenied(t *testing.T) {
	for _, src := range []string{
		`ask("` + sonnet + `", "hi")`,
		`agent("` + sonnet + `", "hi", {})`,
	} {
		got, out := runLLM(t, src, nil, runtime.Limits{}, grant(t, runtime.Tool, "*"))
		e := wantKind(t, got, runtime.KindCapability)
		if !strings.Contains(e.Message, "capability denied") || strings.Contains(e.Message, "provider") || out != "" {
			t.Fatalf("%s: error %q, output %q; want a denial that says nothing of the host", src, e.Message, out)
		}
	}
	// Held, the same call says the host has no providers.
	got, _ := runLLM(t, `ask("`+sonnet+`", "hi")`, nil, runtime.Limits{}, grant(t, runtime.LLM, sonnet))
	wantKindHas(t, got, runtime.KindEffect, "no model providers")
}

// runTool's own result for a tool the task does not hold is the redacted
// denial, which names no held grant (mutation M40: runTools redacts it
// again, so only runTool's contract and its charge tell them apart).
func TestRunToolDeniedResultIsRedacted(t *testing.T) {
	held := grant(t, runtime.FSRead, "/secret/held/path")
	root := runtime.NewRoot(context.Background(), runtime.NewCapSet(held, grant(t, runtime.Tool, "other")), &strings.Builder{})
	in := New(root)
	tools := map[string]agentTool{"w": {fn: &value.Function{}}}
	var text string
	var isErr bool
	var errv *value.Error
	l := runtime.Limits{}
	in.withScope(frame{newScope(root, l, false), runtime.Root(l)}, func() value.Object {
		text, isErr, errv = in.runTool(tools, llm.Block{Type: llm.BlockToolUse, ID: "u", Name: "w", Input: []byte(`{}`)})
		return nil
	})
	want := (&runtime.DeniedError{Want: runtime.Capability{Kind: runtime.Tool, Scope: "w"}}).Redacted()
	if errv != nil || !isErr || text != want || strings.Contains(text, "/secret") {
		t.Fatalf("runTool = %q, %v, %v; want %q as an error result", text, isErr, errv, want)
	}
}

// A model call cut short because the task was cancelled is a cancelled
// error, whatever error the provider returns for it (mutation M43): here
// the host's context is cancelled with a cause that has no kind, and the
// provider answers with a plain error, as a provider may.
func TestCancelledCallIsCancelledWhateverTheProviderSays(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	f := llm.NewFake()
	f.Then = func(context.Context, llm.Request) (llm.Response, error) {
		cancel(errors.New("host stopped"))
		return llm.Response{}, errors.New("connection reset")
	}
	host := fakeHost(f, nil)
	root := runtime.NewRoot(llm.WithHost(ctx, host), runtime.NewCapSet(grant(t, runtime.LLM, "*")), &strings.Builder{})
	in := New(root)
	m, errv := model{}, (*value.Error)(nil)
	var resp llm.Response
	l := runtime.Limits{}
	in.withScope(frame{newScope(root, l, false), runtime.Root(l)}, func() value.Object {
		if m, errv = in.model("ask", sonnet); errv == nil {
			resp, errv = in.complete("ask", m, llm.Request{Messages: []llm.Message{userText("hi")}})
		}
		return nil
	})
	if errv == nil {
		t.Fatalf("complete = %+v, want an error", resp)
	}
	if errv.ErrKind() != runtime.KindCancelled || !strings.Contains(errv.Message, "host stopped") {
		t.Fatalf("error %q has kind %q, want a cancelled error naming the cause", errv.Message, errv.ErrKind())
	}
}
