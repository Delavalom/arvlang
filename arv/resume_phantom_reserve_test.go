package arv

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/delavalom/arvlang/arv/runtime"
)

// A resume's live model step waits until every logged reservation has
// been acted out (AwaitReservations). A forged log can hold a reservation
// that nothing in the run will ever act out: here, one of a task the
// program never spawns. The wait must still end once the run is aborted,
// by the host cancelling it or by the stall watcher diverging it, and
// leave no goroutine behind (mutation M25 of Phase 6x7: on abort,
// waitModels went on waiting for the logged reservations; on an honest
// log the replayed tasks act them out anyway, so no test saw it).
func TestResumeWithAReservationNoTaskActsOutEnds(t *testing.T) {
	_, host, caps := settleWorld()
	const src = `puts(ask("anthropic:claude-haiku-5-5", "a")); puts("after")`
	_, log, err := recordRun(t, `scope (tokens: 100000) { `+src+` }`, Options{LLM: host, Caps: caps, Ctx: hostCtx(t)})
	if err != nil {
		t.Fatal(err)
	}
	// Keep the header and the llm_model record, then add the reservation
	// the root logged next as one of a task that never exists: a crash
	// right after it.
	var model, reserve []byte
	for _, line := range bytes.Split(log, []byte("\n")) {
		switch {
		case bytes.Contains(line, []byte(`"task":"root","seq":1,"kind":"llm_model"`)):
			model = line
		case bytes.Contains(line, []byte(`"task":"root","seq":2,"kind":"llm_reserve"`)):
			reserve = line
		}
	}
	if model == nil || reserve == nil {
		t.Fatalf("the recording lacks the root's llm_model and llm_reserve records:\n%s", log)
	}
	header, _, _ := bytes.Cut(log, []byte("\n"))
	phantom := bytes.Replace(reserve, []byte(`"task":"root","seq":2`), []byte(`"task":"root/7","seq":1`), 1)
	crashed := forge(t, bytes.Join([][]byte{header, model, phantom, nil}, []byte("\n")), func(_ int, b []byte) []byte { return b })

	before := settle(0)
	for _, after := range []time.Duration{-1, 0, time.Millisecond, 50 * time.Millisecond} {
		where := fmt.Sprintf("cancelled after %s", after)
		ctx, cancel := context.WithCancel(context.Background())
		if after < 0 {
			where = "not cancelled" // the stall watcher must end it
		} else {
			defer time.AfterFunc(after, cancel).Stop()
		}
		l, err := runtime.ReadLog(bytes.NewReader(crashed))
		if err != nil {
			t.Fatalf("%s: ReadLog: %v", where, err)
		}
		done := make(chan error, 1)
		go func() {
			_, err := Run(`scope (tokens: 100000) { `+src+` }`, Options{LLM: hangingHost(true), Caps: caps, Resume: l,
				Record: bytes.NewBuffer(bytes.Clone(crashed[:l.Size])), Out: &bytes.Buffer{}, Ctx: ctx})
			done <- err
		}()
		select {
		case err = <-done:
		case <-time.After(90 * time.Second):
			t.Fatalf("%s: the resume did not end: its live call waits for a reservation no task will act out", where)
		}
		cancel()
		noLeak(t, before, where)
		var e *Error
		if err == nil || !errors.As(err, &e) || e.Kind != runtime.KindCancelled && e.Kind != runtime.KindReplay {
			t.Fatalf("%s: want a cancellation or a divergence, got %v", where, err)
		}
		if after < 0 && (e.Kind != runtime.KindReplay || !strings.Contains(err.Error(), "diverged")) {
			t.Fatalf("%s: want the stall watcher's divergence, got %v", where, err)
		}
	}
}
