package arv

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/llm"
	"github.com/delavalom/arvlang/arv/runtime"
)

// overshootSrc runs a and b together under tokens: 10000. b reserves
// while a's call is in flight; a's reply then uses 9000 tokens against
// its reservation of about 4600, so a's settlement takes the level below
// zero (a budget error) while b's call is still in flight. b is answered.
const overshootSrc = `let m = "anthropic:claude-haiku-5-5";
let try = fn(p) { match scope { ask(m, p) } { ok(v) => v, err(e) => e["kind"] + ": " + e["message"] } };
let first = fn() { let r = try("a"); k8s_done("staging"); r };
scope (tokens: 10000) {
  let ta = spawn first() with [llm(m), k8s("staging")];
  let tb = spawn try("b") with [llm(m)];
  puts(await ta); puts(await tb);
}`

// overshootPair is the provider of overshootSrc, keeping the recorded
// order of what runs live: a's reply waits until b's call has started
// when b's call is sent (bLive), and b's until a is done when a's
// k8s_done runs (aLive). A recording runs both.
func overshootPair(aLive, bLive bool) *orderedPair {
	bStarted := make(chan struct{})
	return newOrderedPair(func(ctx context.Context, req llm.Request, aDone <-chan struct{}) (llm.Response, error) {
		wait := func(c <-chan struct{}) error {
			select {
			case <-c:
				return nil
			case <-ctx.Done():
				return context.Cause(ctx)
			}
		}
		if req.Messages[0].Content[0].Text == "a" {
			if bLive {
				if err := wait(bStarted); err != nil {
					return llm.Response{}, err
				}
			}
			return llm.TextReply("a answered", 8000, 1000).Response, nil
		}
		close(bStarted)
		if aLive {
			if err := wait(aDone); err != nil {
				return llm.Response{}, err
			}
		}
		return llm.TextReply("b answered", 5, 5).Response, nil
	})
}

const overshootOut = "budget: budget exceeded: tokens (limit 10000)\nb answered\n"

func recordOvershoot(t *testing.T) ([]byte, int) {
	t.Helper()
	p := overshootPair(true, true)
	out, log, err := recordRun(t, overshootSrc, Options{LLM: p.host, Caps: p.caps, Effects: p.effects})
	if err != nil || out != overshootOut || len(p.fake.Calls()) != 2 {
		t.Fatalf("recording: %v, %d calls; printed %q, want %q", err, len(p.fake.Calls()), out, overshootOut)
	}
	if !bytes.Contains(log, []byte(`"kind":"llm_settle","req":[9000,`)) || !bytes.Contains(log, []byte(`"res":{"error"`)) {
		t.Fatalf("a's settlement should be a logged budget error:\n%s", log)
	}
	replaysAlike(t, overshootSrc, out, log, overshootPair(false, false).effects)
	return log, len(p.fake.Calls())
}

// resumeOvershoot resumes crashed and checks that it prints what the
// uninterrupted run printed and sends only the calls the log lacks.
func resumeOvershoot(t *testing.T, crashed []byte, calls int, name string) {
	t.Helper()
	l, err := runtime.ReadLog(bytes.NewReader(crashed))
	if err != nil {
		t.Fatal(err)
	}
	missing := calls - bytes.Count(crashed, []byte(`"kind":"llm","req"`))
	p := overshootPair(!bytes.Contains(crashed, []byte("k8s_done")), missing > 0) // b's llm record is the last

	var got bytes.Buffer
	_, err = Run(overshootSrc, Options{LLM: p.host, Caps: p.caps, Effects: p.effects, Resume: l, Record: bytes.NewBuffer(bytes.Clone(crashed)), Out: &got})
	if err != nil || got.String() != overshootOut || len(p.fake.Calls()) != missing {
		t.Errorf("%s: %v; %d live calls, want %d; printed %q, want %q (uninterrupted)",
			name, err, len(p.fake.Calls()), missing, got.String(), overshootOut)
	}
}

// Review round 2 of Phase 6x7 (Important): a run killed while b's call
// was in flight, after a sibling's settlement went past its reservation
// and over the limit. b's grant is its last record, so the resume sends
// b's call again. The trailing-grant check (round 1) counted a's excess
// against b's grant, though the recorded run charged it after b had
// reserved, and every attempt to resume this honest log diverged with
// "the budget cannot cover". The excess of a failed settlement no longer
// counts against a grant.
func TestResumeAfterASiblingSettlementOvershoots(t *testing.T) {
	log, calls := recordOvershoot(t)
	crashed := cutAfter(t, log, `"kind":"llm_settle"`)
	if n := bytes.Count(crashed, []byte(`"kind":"llm","req"`)); n != 1 || !strings.Contains(string(crashed), `"kind":"llm_reserve"`) {
		t.Fatalf("the cut log should hold a's reply only (%d):\n%s", n, crashed)
	}
	before := settle(0)
	for i := range 3 {
		resumeOvershoot(t, crashed, calls, fmt.Sprintf("resume %d", i+1))
	}
	if after := settle(before); after > before {
		t.Fatalf("goroutines: %d before, %d after the resumes", before, after)
	}
}

// The same run, killed after any line. A crash after a's llm record and
// before its llm_settle has a's past-limit settlement run live, right
// before b's grant is checked: that cut diverged nearly every time.
func TestResumeAfterASiblingOvershootAfterAnyLine(t *testing.T) {
	log, calls := recordOvershoot(t)
	lines := bytes.SplitAfter(log, []byte("\n"))
	for k := 1; k < len(lines)-1; k++ {
		for i := range 3 {
			resumeOvershoot(t, bytes.Join(lines[:k], nil), calls, fmt.Sprintf("cut after line %d, try %d", k, i+1))
		}
	}
}
