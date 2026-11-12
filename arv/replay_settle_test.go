package arv

import (
	"context"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/llm"
)

// Both calls fit the budget when they reserve, but each uses more than
// it reserved (7000 tokens against about 4600), and only one overage
// fits: the call that settles second fails the budget. In the recording
// that is b, which the provider answers only after a is done. The replay
// settles b first, and must still fail b's settlement, not a's.
func TestReplayOfConcurrentModelSettlements(t *testing.T) {
	for opts, limit := range map[string]string{"tokens: 12000": "tokens (limit 12000)", "usd_cents: 12": "usd_cents (limit 12)"} {
		t.Run(opts, func(t *testing.T) {
			p := newOrderedPair(func(ctx context.Context, req llm.Request, aDone <-chan struct{}) (llm.Response, error) {
				prompt := req.Messages[0].Content[0].Text
				if prompt == "b" {
					select {
					case <-aDone:
					case <-ctx.Done():
						return llm.Response{}, context.Cause(ctx)
					}
				}
				return llm.TextReply(prompt+" answered", 6000, 1000).Response, nil
			})
			src := orderedPairSrc(opts)
			out, log, err := recordRun(t, src, Options{LLM: p.host, Caps: p.caps, Effects: p.effects})
			// c, after both, is refused: the settlement left the level spent.
			want := "a answered\n" + strings.Repeat("budget: budget exceeded: "+limit+"\n", 2)
			if err != nil || out != want || len(p.fake.Calls()) != 2 {
				t.Fatalf("recording: %v, %d calls; printed %q, want %q", err, len(p.fake.Calls()), out, want)
			}
			replaysAlike(t, src, out, log, p.effects)
			if len(p.fake.Calls()) != 2 {
				t.Fatalf("the replays called the provider")
			}
		})
	}
}
