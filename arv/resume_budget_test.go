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

// A resume where b is past its records and a still has a logged
// reservation to act out: b's live reservation must wait for a's, as
// the crashed run had it, so the pool it sees is the one the recorded
// run left. Before (review round 1 of Phase 6x7, Critical), b reserved
// live first, was answered, and a's logged reservation was then forced
// through: 8500 tokens (8.5 cents) spent against a limit of 7000 (7),
// with no budget error, and the output differed from the uninterrupted
// run's. The crash is just after b's k8s_wait record, before its
// llm_reserve: a's spin (replayed) lets b reach the budget first.
func TestResumeWaitsForLoggedReservations(t *testing.T) {
	for _, opts := range []string{"tokens: 7000", "usd_cents: 7"} {
		t.Run(opts, func(t *testing.T) {
			use := map[string][2]int{"a": {3500, 500}, "b": {4000, 500}, "c": {1, 1}}
			p := newOrderedPair(func(ctx context.Context, req llm.Request, _ <-chan struct{}) (llm.Response, error) {
				q := req.Messages[0].Content[0].Text
				return llm.TextReply(q+" answered", use[q][0], use[q][1]).Response, nil
			})
			src := orderedPairSrc(opts)
			out, log, err := recordRun(t, src, Options{LLM: p.host, Caps: p.caps, Effects: p.effects})
			if err != nil || len(p.fake.Calls()) != 1 || !strings.HasPrefix(out, "a answered\nbudget: ") {
				t.Fatalf("recording: %v, %d calls; printed %q", err, len(p.fake.Calls()), out)
			}
			crashed := cutAfter(t, log, `k8s_wait`)
			if bytes.Count(crashed, []byte(`"kind":"llm_reserve"`)) != 1 {
				t.Fatalf("the cut log should hold a's reservation only:\n%s", crashed)
			}
			before := settle(0)
			for i := range 3 {
				l, err := runtime.ReadLog(bytes.NewReader(crashed))
				if err != nil {
					t.Fatal(err)
				}
				var got bytes.Buffer
				_, err = Run(src, Options{LLM: p.host, Caps: p.caps, Effects: p.effects, Resume: l, Record: bytes.NewBuffer(bytes.Clone(crashed)), Out: &got})
				spent := 0
				for _, c := range p.fake.Calls() {
					u := use[c.Messages[0].Content[0].Text]
					spent += u[0] + u[1]
				}
				if err != nil || got.String() != out || spent > 7000 {
					t.Fatalf("resume %d: %v; spent %d tokens (limit 7000) in %d calls; printed %q, want %q",
						i, err, spent, len(p.fake.Calls()), got.String(), out)
				}
			}
			if after := settle(before); after > before {
				t.Fatalf("goroutines: %d before, %d after the resumes", before, after)
			}
		})
	}
}

// A forged log can turn a refused reservation into a granted one. When
// that grant is the task's last record, the call after it is sent live,
// so the grant must be backed by the live pools: the resume diverges
// before any request goes out. Before (review round 1 of Phase 6x7,
// Critical), it sent a call of 4000 tokens under a 1000-token scope and
// ended well.
func TestResumeRefusesAForgedTrailingGrant(t *testing.T) {
	fake := &llm.Fake{Then: func(ctx context.Context, req llm.Request) (llm.Response, error) {
		return llm.TextReply("answered", 3000, 1000).Response, nil
	}}
	host := &llm.Host{Providers: map[string]llm.Provider{"anthropic": fake}}
	caps := []runtime.Capability{{Kind: runtime.LLM, Scope: "anthropic:*"}}
	src := `scope (tokens: 1000) { puts(match scope { ask("anthropic:claude-haiku-5-5", "a") } { ok(v) => v, err(e) => e["kind"] + ": " + e["message"] }) }`
	out, log, err := recordRun(t, src, Options{LLM: host, Caps: caps})
	if err != nil || out != "budget: budget exceeded: tokens (limit 1000)\n" || len(fake.Calls()) != 0 {
		t.Fatalf("recording: %v, %d calls; printed %q", err, len(fake.Calls()), out)
	}
	res := regexp.MustCompile(`"res":.*$`)
	forged := forge(t, cutAfter(t, log, `"kind":"llm_reserve"`), func(i int, b []byte) []byte {
		if bytes.Contains(b, []byte(`"llm_reserve"`)) {
			return append(res.ReplaceAll(b[:len(b)-1], []byte(`"res":null`)), '}')
		}
		return b
	})
	l, err := runtime.ReadLog(bytes.NewReader(forged))
	if err != nil {
		t.Fatal(err)
	}
	var got bytes.Buffer
	_, err = Run(src, Options{LLM: host, Caps: caps, Resume: l, Record: bytes.NewBuffer(bytes.Clone(forged)), Out: &got})
	if err == nil || !strings.Contains(err.Error(), "replay diverged") || !strings.Contains(err.Error(), "cannot cover") || len(fake.Calls()) != 0 {
		t.Fatalf("resume: %v; %d provider calls; printed %q", err, len(fake.Calls()), got.String())
	}
}

// A genuine trailing grant (a crash while the call was in flight) is
// sent again, and the resume ends as the uninterrupted run did.
func TestResumeSendsAnInFlightGrantedCall(t *testing.T) {
	fake := &llm.Fake{Then: func(ctx context.Context, req llm.Request) (llm.Response, error) {
		return llm.TextReply("answered", 3000, 1000).Response, nil
	}}
	host := &llm.Host{Providers: map[string]llm.Provider{"anthropic": fake}}
	caps := []runtime.Capability{{Kind: runtime.LLM, Scope: "anthropic:*"}}
	src := `scope (tokens: 9000) { puts(ask("anthropic:claude-haiku-5-5", "a")) }`
	out, log, err := recordRun(t, src, Options{LLM: host, Caps: caps})
	if err != nil || out != "answered\n" {
		t.Fatalf("recording: %v; printed %q", err, out)
	}
	crashed := cutAfter(t, log, `"kind":"llm_reserve"`)
	l, err := runtime.ReadLog(bytes.NewReader(crashed))
	if err != nil {
		t.Fatal(err)
	}
	var got bytes.Buffer
	_, err = Run(src, Options{LLM: host, Caps: caps, Resume: l, Record: bytes.NewBuffer(bytes.Clone(crashed)), Out: &got})
	if err != nil || got.String() != out || len(fake.Calls()) != 2 {
		t.Fatalf("resume: %v; %d provider calls in all; printed %q", err, len(fake.Calls()), got.String())
	}
}
