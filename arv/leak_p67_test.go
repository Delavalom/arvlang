package arv

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/delavalom/arvlang/arv/llm"
	"github.com/delavalom/arvlang/arv/runtime"
)

// The leak rule for Phase 6x7's concurrency: a resume's live model steps
// park until the logged reservations are acted out (AwaitReservations),
// and concurrent agents race for one budget. No goroutine may outlive
// arv.Run, whether the resume ends well, diverges, or is cancelled by its
// host while a call is in flight or a task is parked.

// raceSrc spawns three asks under one tokens pool (tokens: 14000 grants
// all three reservations, 10000 only two). Each reply uses 7000
// tokens against a reservation of about 4600, so a settlement may fail;
// which asks are granted depends on the run's scheduling.
const raceSrc = `let try = fn(p) { match scope { ask("anthropic:claude-haiku-5-5", p) } { ok(v) => v, err(e) => e["kind"] + ": " + e["message"] } };
let g = [llm("anthropic:claude-haiku-5-5")];
scope (%s) {
  let ts = [spawn try("a") with g, spawn try("b") with g, spawn try("c") with g];
  for t in ts { puts(await t) }
}`

// hangingHost answers like settleWorld's, but hangs every call until the
// run's context is done when hang is set.
func hangingHost(hang bool) *llm.Host {
	_, host, _ := settleWorld()
	if hang {
		host.Providers["anthropic"] = &llm.Fake{Then: func(ctx context.Context, req llm.Request) (llm.Response, error) {
			<-ctx.Done()
			return llm.Response{}, ctx.Err()
		}}
	}
	return host
}

// Resumed after every line of a racing run: each resume ends well, its
// log replays alike, and nothing outlives either run.
func TestP67NoGoroutineOutlivesARacingResume(t *testing.T) {
	_, host, caps := settleWorld()
	before := settle(0)
	for rec := range 4 {
		raceSrc := fmt.Sprintf(raceSrc, []string{"tokens: 14000", "tokens: 10000"}[rec%2])
		_, log, err := recordRun(t, raceSrc, Options{LLM: host, Caps: caps, Ctx: hostCtx(t)})
		noLeak(t, before, "record")
		if err != nil || !bytes.Contains(log, []byte(`"kind":"llm_reserve"`)) {
			t.Fatalf("recording: %v\n%s", err, log)
		}
		lines := bytes.SplitAfter(log, []byte("\n"))
		for k := 1; k < len(lines)-1; k++ {
			crashed := bytes.Join(lines[:k], nil)
			l, err := runtime.ReadLog(bytes.NewReader(crashed))
			if err != nil {
				t.Fatal(err)
			}
			w := bytes.NewBuffer(bytes.Clone(crashed))
			var out bytes.Buffer
			_, err = Run(raceSrc, Options{LLM: hangingHost(false), Caps: caps, Resume: l, Record: w, Out: &out, Ctx: hostCtx(t)})
			where := fmt.Sprintf("recording %d, resume after %d lines", rec, k)
			noLeak(t, before, where)
			if err != nil || strings.Count(out.String(), "\n") != 3 {
				t.Fatalf("%s: %v\n%s", where, err, out.String())
			}
			resumed, err := runtime.ReadLog(bytes.NewReader(w.Bytes()))
			if err != nil {
				t.Fatal(err)
			}
			var got bytes.Buffer
			if _, err := Run(raceSrc, Options{Replay: resumed, Out: &got, Ctx: hostCtx(t)}); err != nil || got.String() != out.String() {
				t.Fatalf("%s: the replay of the resumed log: %v\n%q\n%q", where, err, got.String(), out.String())
			}
			noLeak(t, before, where+", replayed")
		}
	}
}

// The host cancels a resume whose live calls never answer, at once or
// after a moment, after every line of a racing run's log: the resume
// ends with the host's cancellation (or a budget refusal that raced it,
// or, when its log already covers the whole run, ends well) and leaves no
// goroutine behind.
func TestP67NoGoroutineOutlivesACancelledRacingResume(t *testing.T) {
	_, host, caps := settleWorld()
	before := settle(0)
	for _, limit := range []string{"tokens: 14000", "tokens: 10000"} {
		raceSrc := fmt.Sprintf(raceSrc, limit)
		_, log, err := recordRun(t, raceSrc, Options{LLM: host, Caps: caps, Ctx: hostCtx(t)})
		if err != nil {
			t.Fatal(err)
		}
		cancelAfterEachLine(t, raceSrc, caps, bytes.SplitAfter(log, []byte("\n")), before)
	}
}

func cancelAfterEachLine(t *testing.T, raceSrc string, caps []runtime.Capability, lines [][]byte, before int) {
	for k := 1; k < len(lines)-1; k++ {
		for _, after := range []time.Duration{0, time.Millisecond, 20 * time.Millisecond} {
			l, err := runtime.ReadLog(bytes.NewReader(bytes.Join(lines[:k], nil)))
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			timer := time.AfterFunc(after, cancel)
			done := make(chan error, 1)
			go func() {
				_, err := Run(raceSrc, Options{LLM: hangingHost(true), Caps: caps, Resume: l,
					Record: bytes.NewBuffer(bytes.Join(lines[:k], nil)), Out: &bytes.Buffer{}, Ctx: ctx})
				done <- err
			}()
			select {
			case err = <-done:
			case <-time.After(60 * time.Second):
				t.Fatalf("resume after %d lines, cancelled after %s: Run did not return", k, after)
			}
			timer.Stop()
			cancel()
			where := fmt.Sprintf("resume after %d lines, cancelled after %s", k, after)
			noLeak(t, before, where)
			// The host's cancellation, or a refused reservation that raced
			// it: match does not catch an error once its task is cancelled
			// (evalMatch), so the refusal can be the run's first failure.
			// Never a divergence: the log is honest.
			var e *Error
			if err != nil && (!errors.As(err, &e) || e.Kind != runtime.KindCancelled && e.Kind != runtime.KindBudget) {
				t.Fatalf("%s: %v", where, err)
			}
		}
	}
}
