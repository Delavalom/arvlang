package arv

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/delavalom/arvlang/arv/llm"
	"github.com/delavalom/arvlang/arv/runtime"
)

// I7 with a live provider in reach: the CLI hands a replay the model host
// it would hand any run (runCLI sets opts.LLM before the log flags), so
// a replay must stay offline because the log answers every call, not
// because no provider is there. Replayed with the recording's model host
// and host effects (a replay takes its grants from the log), an agent run
// prints and ends the same and the provider sees no call (mutation M01
// of Phase 6x7: every other replay test has no host, so a replayed llm
// record that also sent its call failed before reaching a provider, and
// went unnoticed).
func TestReplayWithALiveModelHostSendsNothing(t *testing.T) {
	w := newAgentWorld(t, agentRun)
	fake := agentPlan.fake()
	host := &llm.Host{Providers: map[string]llm.Provider{"anthropic": fake}}
	out, res, l := recordOpts(t, w.src, Options{LLM: host, Caps: w.caps, Effects: w.effects})
	calls, hits, applied := len(fake.Calls()), w.hits.Load(), w.applied.Load()
	if calls != 9 || res != `end` {
		t.Fatalf("recording: %d calls, result %s\n%s", calls, res, out)
	}
	if err := os.Remove(filepath.Join(w.dir, "out.txt")); err != nil {
		t.Fatal(err)
	}
	var got bytes.Buffer
	rres, err := Run(w.src, Options{Replay: l, LLM: host, Effects: w.effects, Out: &got})
	if n := len(fake.Calls()) - calls; n != 0 {
		t.Fatalf("the replay sent %d model calls with a live host in reach", n)
	}
	if err != nil || got.String() != out || resultText(rres, err) != res {
		t.Fatalf("replay: %v\nprinted %q\nwant    %q", err, got.String(), out)
	}
	if w.hits.Load() != hits || w.applied.Load() != applied {
		t.Fatalf("the replay ran effects: %d requests, %d host calls", w.hits.Load()-hits, w.applied.Load()-applied)
	}
	if _, err := os.Stat(filepath.Join(w.dir, "out.txt")); !os.IsNotExist(err) {
		t.Fatalf("the replay wrote a file: %v", err)
	}
}

// The same for plain asks, racing under one pool, with a provider that
// would answer every call: the replay sends none of them.
func TestReplayOfRacingAsksWithALiveHostSendsNothing(t *testing.T) {
	fake, host, caps := settleWorld()
	for _, limit := range []string{"tokens: 14000", "tokens: 10000"} {
		src := fmt.Sprintf(raceSrc, limit)
		out, log, err := recordRun(t, src, Options{LLM: host, Caps: caps, Ctx: hostCtx(t)})
		if err != nil {
			t.Fatal(err)
		}
		calls := len(fake.Calls())
		l, err := runtime.ReadLog(bytes.NewReader(log))
		if err != nil {
			t.Fatal(err)
		}
		var got bytes.Buffer
		_, err = Run(src, Options{Replay: l, LLM: host, Out: &got, Ctx: context.Background()})
		if n := len(fake.Calls()) - calls; n != 0 {
			t.Fatalf("%s: the replay sent %d model calls with a live host in reach", limit, n)
		}
		if err != nil || got.String() != out {
			t.Fatalf("%s: replay: %v\nprinted %q\nwant    %q", limit, err, got.String(), out)
		}
	}
}
