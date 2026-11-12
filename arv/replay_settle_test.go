package arv

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/llm"
	"github.com/delavalom/arvlang/arv/runtime"
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

// The run above, killed after any line and resumed: each resume prints
// what the uninterrupted run printed and sends only the calls whose llm
// record the log lacks. This is the resume of a failed (logged-error)
// llm_settle under concurrency (review round 2 of Phase 6x7: only a
// replay covered it). A crash after a's settlement and before b's call
// returns has b's grant trailing, checked against a level a's past-limit
// settlement may already have spent.
func TestResumeOfConcurrentModelSettlementsAfterAnyLine(t *testing.T) {
	for _, opts := range []string{"tokens: 12000", "usd_cents: 12"} {
		t.Run(opts, func(t *testing.T) {
			mk := func() *orderedPair {
				return newOrderedPair(func(ctx context.Context, req llm.Request, aDone <-chan struct{}) (llm.Response, error) {
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
			}
			p := mk()
			src := orderedPairSrc(opts)
			out, log, err := recordRun(t, src, Options{LLM: p.host, Caps: p.caps, Effects: p.effects})
			if err != nil || !bytes.Contains(log, []byte(`"kind":"llm_settle","req":[7000,70000],"res":{"error"`)) {
				t.Fatalf("recording: %v; the log should hold a failed llm_settle:\n%s", err, log)
			}
			calls := len(p.fake.Calls())
			lines := bytes.SplitAfter(log, []byte("\n"))
			for k := 1; k < len(lines)-1; k++ {
				crashed := bytes.Join(lines[:k], nil)
				l, err := runtime.ReadLog(bytes.NewReader(crashed))
				if err != nil {
					t.Fatal(err)
				}
				p := mk()
				if bytes.Contains(crashed, []byte(`"kind":"host:k8s_done"`)) {
					p.effects["k8s_done"].Fn(nil) // replayed: a is done
				}
				var got bytes.Buffer
				_, err = Run(src, Options{LLM: p.host, Caps: p.caps, Effects: p.effects, Resume: l, Record: bytes.NewBuffer(bytes.Clone(crashed)), Out: &got})
				missing := calls - bytes.Count(crashed, []byte(`"kind":"llm","req"`))
				if err != nil || got.String() != out || len(p.fake.Calls()) != missing {
					t.Errorf("cut after line %d: %v; %d live calls, want %d; printed %q, want %q",
						k, err, len(p.fake.Calls()), missing, got.String(), out)
				}
			}
		})
	}
}
