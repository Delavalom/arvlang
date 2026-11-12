package arv

import (
	"bytes"
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/llm"
	"github.com/delavalom/arvlang/arv/runtime"
)

// settleSrc asks twice under one scope's model budget; each reply uses
// 7000 tokens, more than its reservation (about 4600), so each granted
// call settles past it: an llm_settle record.
const settleSrc = `let try = fn(p) { match scope { ask("anthropic:claude-haiku-5-5", p) } { ok(v) => v, err(e) => e["kind"] + ": " + e["message"] } };
scope (%s) { puts(try("a")); puts(try("b")); puts(try("c")) }`

func settleWorld() (*llm.Fake, *llm.Host, []runtime.Capability) {
	fake := &llm.Fake{Then: func(ctx context.Context, req llm.Request) (llm.Response, error) {
		return llm.TextReply(req.Messages[0].Content[0].Text+" answered", 6000, 1000).Response, nil
	}}
	host := &llm.Host{Providers: map[string]llm.Provider{"anthropic": fake},
		Prices: map[string]llm.Price{"anthropic:claude-haiku-5-5": {Input: 10_000_000, Output: 10_000_000}}}
	return fake, host, []runtime.Capability{{Kind: runtime.LLM, Scope: "anthropic:*"}}
}

// A run whose model calls settle past their reservations, killed after
// any line and resumed, prints what the uninterrupted run printed, and
// sends only the calls whose llm record the log lacks (review round 1 of
// Phase 6x7: no resume went through llm_settle). With tokens: 16000 both
// overages fit and c is refused at its reservation; with 10000, b's
// settlement fails the budget (a logged error).
func TestResumeAroundSettlementsAfterAnyLine(t *testing.T) {
	for _, opts := range []string{"tokens: 16000", "tokens: 10000", "usd_cents: 10"} {
		t.Run(opts, func(t *testing.T) {
			ref, host, caps := settleWorld()
			src := strings.Replace(settleSrc, "%s", opts, 1)
			out, log, err := recordRun(t, src, Options{LLM: host, Caps: caps})
			if err != nil || !strings.HasPrefix(out, "a answered\n") || !strings.Contains(out, "budget exceeded") {
				t.Fatalf("recording: %v; printed %q", err, out)
			}
			if bytes.Count(log, []byte(`"kind":"llm_settle"`)) == 0 {
				t.Fatalf("the log has no llm_settle:\n%s", log)
			}
			calls := len(ref.Calls())
			lines := bytes.SplitAfter(log, []byte("\n"))
			for k := 1; k < len(lines)-1; k++ {
				crashed := bytes.Join(lines[:k], nil)
				l, err := runtime.ReadLog(bytes.NewReader(crashed))
				if err != nil {
					t.Fatal(err)
				}
				live, host, _ := settleWorld()
				var got bytes.Buffer
				_, err = Run(src, Options{LLM: host, Caps: caps, Resume: l, Record: bytes.NewBuffer(bytes.Clone(crashed)), Out: &got})
				missing := calls - bytes.Count(crashed, []byte(`"kind":"llm","req"`))
				if err != nil || got.String() != out || len(live.Calls()) != missing {
					t.Errorf("cut after line %d: %v; %d calls, want %d; printed %q, want %q", k, err, len(live.Calls()), missing, got.String(), out)
				}
			}
		})
	}
}

// Only a call that a tokens or usd_cents level limits has its
// reservation logged.
func TestOnlyLimitedCallsLogReservations(t *testing.T) {
	for src, want := range map[string]int{
		`puts(ask("anthropic:claude-haiku-5-5", "a")); puts(ask("anthropic:claude-haiku-5-5", "b"))`:                                      0,
		`scope (steps: 100000) { puts(ask("anthropic:claude-haiku-5-5", "a")) }; puts(ask("anthropic:claude-haiku-5-5", "b"))`:            0,
		`scope (tokens: 100000) { puts(ask("anthropic:claude-haiku-5-5", "a")) }; puts(ask("anthropic:claude-haiku-5-5", "b"))`:           1,
		`scope (usd_cents: 100) { scope { puts(ask("anthropic:claude-haiku-5-5", "a")); puts(ask("anthropic:claude-haiku-5-5", "b")) } }`: 2,
	} {
		_, host, caps := settleWorld()
		_, log, err := recordRun(t, src, Options{LLM: host, Caps: caps})
		if err != nil {
			t.Fatalf("%s: %v", src, err)
		}
		if got := bytes.Count(log, []byte(`"kind":"llm_reserve"`)); got != want || bytes.Count(log, []byte(`"kind":"llm","req"`)) != 2 {
			t.Errorf("%s: %d llm_reserve records, want %d:\n%s", src, got, want, log)
		}
	}
}

// A logged reservation or settlement whose result is neither nil nor an
// error does not fit the program: the replay diverges.
func TestReplayOfAForgedModelBudgetResult(t *testing.T) {
	_, host, caps := settleWorld()
	src := strings.Replace(settleSrc, "%s", "tokens: 16000", 1)
	_, log, err := recordRun(t, src, Options{LLM: host, Caps: caps})
	if err != nil {
		t.Fatal(err)
	}
	res := regexp.MustCompile(`"res":.*$`)
	for _, kind := range []string{"llm_reserve", "llm_settle"} {
		forged := forge(t, log, func(i int, b []byte) []byte {
			if bytes.Contains(b, []byte(`"kind":"`+kind+`"`)) {
				return append(res.ReplaceAll(b[:len(b)-1], []byte(`"res":1`)), '}')
			}
			return b
		})
		err := replayForged(t, src, forged, Options{})
		if err == nil || !strings.Contains(err.Error(), "the log's "+kind+" is INTEGER, not nil or an error") {
			t.Errorf("%s: %v", kind, err)
		}
	}
}
