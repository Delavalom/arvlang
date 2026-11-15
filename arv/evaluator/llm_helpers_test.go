package evaluator

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/lexer"
	"github.com/delavalom/arvlang/arv/llm"
	"github.com/delavalom/arvlang/arv/parser"
	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// sonnet is the model the Phase 7 acceptance tests name.
const sonnet = "anthropic:claude-sonnet-5-5"

// fakeHost serves every model of "anthropic" from f, at the default prices
// unless prices overrides them.
func fakeHost(f *llm.Fake, prices map[string]llm.Price) *llm.Host {
	return &llm.Host{Providers: map[string]llm.Provider{"anthropic": f}, Prices: prices}
}

// runLLM runs input as the root task holding caps, under limits l, with
// host as the run's model providers. It returns the result and the output.
func runLLM(t *testing.T, input string, host *llm.Host, l runtime.Limits, caps ...runtime.Capability) (value.Object, string) {
	t.Helper()
	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}
	var out bytes.Buffer
	ctx := context.Background()
	if host != nil {
		ctx = llm.WithHost(ctx, host)
	}
	root := runtime.NewRoot(ctx, runtime.NewCapSet(caps...), &out)
	return New(root).RunLimited(program, value.NewEnvironment(), l), out.String()
}

// wantKindHas fails unless got is an error of kind whose message
// contains msg.
func wantKindHas(t *testing.T, got value.Object, kind, msg string) {
	t.Helper()
	if e := wantKind(t, got, kind); !strings.Contains(e.Message, msg) {
		t.Fatalf("error %q does not contain %q", e.Message, msg)
	}
}

// userTextOf is the text of the first block of the first message of req.
func userTextOf(req llm.Request) string {
	if len(req.Messages) == 0 || len(req.Messages[0].Content) == 0 {
		return ""
	}
	return req.Messages[0].Content[0].Text
}
