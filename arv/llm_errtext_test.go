package arv

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/delavalom/arvlang/arv/llm"
	"github.com/delavalom/arvlang/arv/runtime"
)

// A host's provider can wrap an *llm.Error. The program sees the same
// error, kind and text, whether or not the run keeps a log, and a replay
// sees it too (review round 1 of Phase 6x7: the log kept only the
// *llm.Error, so recording dropped the wrapper's text).
func TestRecordingKeepsAWrappedProviderErrorsText(t *testing.T) {
	src := `puts(match scope { ask("anthropic:claude-haiku-5-5", "q") } { ok(v) => v, err(e) => e["kind"] + ": " + e["message"] });`
	for _, perr := range []error{
		fmt.Errorf("gateway eu-1: %w", &llm.Error{Provider: "anthropic", Kind: llm.ErrServer, Status: 500, Message: "boom"}),
		&llm.Error{Provider: "anthropic", Kind: llm.ErrServer, Status: 500, Message: "boom"},
		fmt.Errorf("gateway eu-1: %w", &llm.Error{Provider: "anthropic", Kind: llm.ErrRateLimit, Status: 429, Message: "slow down"}),
	} {
		opts := func() Options {
			host := &llm.Host{Providers: map[string]llm.Provider{"anthropic": llm.NewFake(llm.Reply{Err: perr})}}
			return Options{LLM: host, Caps: []runtime.Capability{{Kind: runtime.LLM, Scope: "anthropic:*"}}}
		}
		var plain bytes.Buffer
		o := opts()
		o.Out = &plain
		if _, err := Run(src, o); err != nil {
			t.Fatal(err)
		}
		out, log, err := recordRun(t, src, opts())
		if err != nil || out != plain.String() {
			t.Errorf("%v: recorded %v, %q; unrecorded %q", perr, err, out, plain.String())
			continue
		}
		if replayed, err := replayRun(t, src, log); err != nil || replayed != out {
			t.Errorf("%v: replayed %v, %q; recorded %q", perr, err, replayed, out)
		}
	}
}
