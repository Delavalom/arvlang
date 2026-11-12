package arv

import (
	"bytes"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"testing"

	"github.com/delavalom/arvlang/arv/llm"
	"github.com/delavalom/arvlang/arv/runtime"
)

// Resume as a property: a random sequential agent run (agents, asks, every
// kind of tool, random limits and usage, so refusals and settlements past
// a reservation), cut after a random line of its log as a crash would
// leave it, resumes to print exactly what the uninterrupted run printed.
// Its live part sends only the calls whose llm record the cut lost, each
// a request the uninterrupted run sent, and repeats no request or host
// call the log holds. A replay of the resumed log prints the same, and
// no goroutine outlives any run.
func TestPropAgentRunsResumeAnywhere(t *testing.T) {
	w := newAgentWorld(t, agentTools)
	before := settle(0)
	host := func(f *llm.Fake) *llm.Host { return &llm.Host{Providers: map[string]llm.Provider{"anthropic": f}} }
	resumes := 0
	for i := range agentPropCases() / 2 {
		g := &agentGen{r: rand.New(rand.NewSource(int64(1000 + i))), dir: w.dir, plan: agentScript{}, usage: map[string][2]int{}, seq: true}
		src := w.src + g.program()
		clearDir(t, w.dir)
		ref := g.fake()
		h0, a0 := w.hits.Load(), w.applied.Load()
		out, log, err := recordRun(t, src, Options{LLM: host(ref), Caps: w.caps, Effects: w.effects, Ctx: hostCtx(t)})
		noLeak(t, before, src)
		if err != nil {
			t.Fatalf("case %d: the recording failed: %v\n%s", i, err, src)
		}
		sent := requestKeys(t, ref.Calls())
		calls, hits, applied := int64(len(ref.Calls())), w.hits.Load()-h0, w.applied.Load()-a0
		lines := bytes.SplitAfter(log, []byte("\n"))
		for range 3 {
			k := 1 + g.r.Intn(len(lines)-2)
			crashed := bytes.Join(lines[:k], nil)
			count := func(kind string) int64 { return int64(bytes.Count(crashed, []byte(`"kind":"`+kind+`","req"`))) }
			// The directory as the crashed run left it: f.txt only if
			// its write is logged.
			clearDir(t, w.dir)
			if count("write_file") > 0 {
				if err := os.WriteFile(filepath.Join(w.dir, "f.txt"), []byte("saved f.txt"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			l, err := runtime.ReadLog(bytes.NewReader(crashed))
			if err != nil {
				t.Fatal(err)
			}
			live := g.fake()
			h1, a1 := w.hits.Load(), w.applied.Load()
			rec := bytes.NewBuffer(bytes.Clone(crashed))
			var got bytes.Buffer
			_, err = Run(src, Options{LLM: host(live), Caps: w.caps, Effects: w.effects, Resume: l, Record: rec, Out: &got, Ctx: hostCtx(t)})
			noLeak(t, before, src)
			where := fmt.Sprintf("case %d, resume after %d of %d lines", i, k, len(lines)-1)
			if err != nil || got.String() != out {
				t.Fatalf("%s: %v\nprinted %q\nwant    %q\n%s", where, err, got.String(), out, src)
			}
			if n := int64(len(live.Calls())); n != calls-count("llm") {
				t.Fatalf("%s: %d live model calls, want %d\n%s", where, n, calls-count("llm"), src)
			}
			for key := range requestKeys(t, live.Calls()) {
				if !sent[key] {
					t.Fatalf("%s sent a request the uninterrupted run did not:\n%s", where, key)
				}
			}
			if h, a := w.hits.Load()-h1, w.applied.Load()-a1; h != hits-count("http_get") || a != applied-count("host:k8s_apply") {
				t.Fatalf("%s: %d requests and %d host calls, want %d and %d\n%s", where, h, a,
					hits-count("http_get"), applied-count("host:k8s_apply"), src)
			}
			resumed, err := runtime.ReadLog(bytes.NewReader(rec.Bytes()))
			if err != nil {
				t.Fatalf("%s: the resumed log: %v", where, err)
			}
			var replayed bytes.Buffer
			if _, err := Run(src, Options{Replay: resumed, Out: &replayed, Effects: w.effects, Ctx: hostCtx(t)}); err != nil || replayed.String() != out {
				t.Fatalf("%s: replay of the resumed log: %v\n%s", where, err, replayed.String())
			}
			noLeak(t, before, src)
			resumes++
		}
	}
	t.Logf("%d resumes", resumes)
}

// clearDir removes every file in dir.
func clearDir(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if err := os.Remove(filepath.Join(dir, e.Name())); err != nil {
			t.Fatal(err)
		}
	}
}
