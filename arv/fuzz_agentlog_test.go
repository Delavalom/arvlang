package arv

import (
	"bytes"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	goruntime "runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/delavalom/arvlang/arv/llm"
	"github.com/delavalom/arvlang/arv/runtime"
)

// Fuzzing the log reader on agent-run records: llm_model, llm (a whole
// model request and its reply or error), llm_reserve, llm_settle, the
// tools' effects and a host effect. The input is a recorded agent run's
// lines without their hashes; forge recomputes the chain, so only the
// records' content can give an edit away and the fuzzer reaches the
// decoders behind the hash check.

// agentFuzz is one recorded agent run to fuzz: agentRun under a plan
// whose "main" replies use more than they reserve (so llm_settle is
// logged), and its world.
type agentFuzz struct {
	src     string
	dir     string
	log     []byte
	gen     *agentGen
	caps    []runtime.Capability
	effects map[string]Effect
	hits    *atomic.Int64
	applied *atomic.Int64
}

func newAgentFuzz(f *testing.F) *agentFuzz {
	srv, hits := countedServer(f)
	dir, err := filepath.EvalSymlinks(f.TempDir())
	if err != nil {
		f.Fatal(err)
	}
	a := &agentFuzz{dir: dir, hits: hits, applied: new(atomic.Int64)}
	a.src = strings.NewReplacer("{{SRV}}", srv.URL, "{{DIR}}", dir).Replace(agentRun)
	a.effects = map[string]Effect{"k8s_apply": counted(a.applied, applied)}
	a.caps = []runtime.Capability{{Kind: runtime.LLM, Scope: "anthropic:*"}, {Kind: runtime.Net, Scope: "127.0.0.1"},
		{Kind: runtime.FSRead, Scope: dir}, {Kind: runtime.FSWrite, Scope: dir}, {Kind: runtime.Env, Scope: "ARV_P67_HOME"}, staging}
	for _, name := range []string{"fetch", "save", "load", "home", "deploy", "dice", "broken"} {
		a.caps = append(a.caps, runtime.Capability{Kind: runtime.Tool, Scope: name})
	}
	a.gen = &agentGen{r: rand.New(rand.NewSource(1)), plan: agentPlan,
		usage: map[string][2]int{"main": {6000, 500}, "child": {100, 10}, "loop": {3000, 10}}}
	out, log, err := recordRun(f, a.src, Options{LLM: a.host(), Caps: a.caps, Effects: a.effects})
	if err != nil || !strings.Contains(out, "max_turns (limit 4)") {
		f.Fatalf("recording: %v\n%s", err, out)
	}
	for _, kind := range []string{"llm_model", "llm", "llm_reserve", "llm_settle", "http_get", "write_file", "host:k8s_apply", "random"} {
		if !bytes.Contains(log, []byte(`"kind":"`+kind+`","req"`)) {
			f.Fatalf("the recorded run logged no %s record:\n%s", kind, log)
		}
	}
	a.log = log
	return a
}

func (a *agentFuzz) host() *llm.Host {
	return &llm.Host{Providers: map[string]llm.Provider{"anthropic": a.gen.fake()}}
}

// seeds adds the log without hashes, and edits of its agent records.
func (a *agentFuzz) seeds(f *testing.F) {
	strip := func(log []byte) []byte { return regexp.MustCompile(`,"h":"[0-9a-f]{64}"`).ReplaceAll(log, nil) }
	f.Add(strip(a.log))
	for _, edit := range [][2]string{
		{`"kind":"llm_reserve","req":[`, `"kind":"llm_reserve","req":[1,`},
		{`"kind":"llm_reserve","req":[`, `"kind":"llm_reserve","res":{"error":["budget exceeded: tokens (limit 1)","budget","",""]},"req":[`},
		{`"kind":"llm_settle"`, `"kind":"llm_reserve"`},
		{`"InputTokens":6000`, `"InputTokens":-6000`},
		{`"InputTokens":6000`, `"InputTokens":99999999999999999999`},
		{`"StopReason":"tool_use"`, `"StopReason":"max_tokens"`},
		{`"Name":"fetch"`, `"Name":"secret"`},
		{`["input",`, `["input",-`},
		{`"kind":"llm_model"`, `"kind":"llm"`},
	} {
		f.Add(strip(bytes.Replace(a.log, []byte(edit[0]), []byte(edit[1]), 1)))
	}
	lines := bytes.SplitAfter(strip(a.log), []byte("\n"))
	f.Add(bytes.Join(lines[:len(lines)/2], nil))
}

// files is what dir holds.
func (a *agentFuzz) files(t *testing.T) []string {
	entries, err := os.ReadDir(a.dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// Fuzz property: whatever a forged agent-run log holds, a replay of it
// never panics, ends within 60 s, makes no provider call (no host is
// given), runs no tool effect (no request, no file written, no host
// call), and leaves no goroutine behind.
func FuzzForgedAgentReplay(f *testing.F) {
	a := newAgentFuzz(f)
	a.seeds(f)
	if err := os.RemoveAll(a.dir); err != nil {
		f.Fatal(err)
	}
	if err := os.Mkdir(a.dir, 0o755); err != nil {
		f.Fatal(err)
	}
	hits, applied := a.hits.Load(), a.applied.Load()
	f.Fuzz(func(t *testing.T, bodies []byte) {
		before := goruntime.NumGoroutine()
		forged := forge(t, bodies, func(_ int, b []byte) []byte { return b })
		replayForged(t, a.src, forged, Options{Effects: a.effects})
		noLeak(t, before, "a forged agent replay")
		if a.hits.Load() != hits || a.applied.Load() != applied || len(a.files(t)) != 0 {
			t.Fatalf("a forged replay went live: %d requests, %d host calls, files %v",
				a.hits.Load()-hits, a.applied.Load()-applied, a.files(t))
		}
	})
}

// Fuzz property: a resume of a forged agent-run log, with a live model
// and the run's grants, never panics, ends within 60 s and leaves no
// goroutine behind. When it ends well, the log it wrote reads back as a
// finished run, and a replay of that log prints what the resume printed
// and goes nowhere live.
func FuzzForgedAgentResume(f *testing.F) {
	a := newAgentFuzz(f)
	a.seeds(f)
	f.Fuzz(func(t *testing.T, bodies []byte) {
		before := goruntime.NumGoroutine()
		forged := forge(t, bodies, func(_ int, b []byte) []byte { return b })
		l, err := runtime.ReadLog(bytes.NewReader(forged))
		if err != nil {
			return
		}
		rec := bytes.NewBuffer(bytes.Clone(forged[:l.Size]))
		var out bytes.Buffer
		done := make(chan error, 1)
		go func() {
			_, err := Run(a.src, Options{LLM: a.host(), Caps: a.caps, Effects: a.effects, Resume: l, Record: rec, Out: &out})
			done <- err
		}()
		select {
		case err = <-done:
		case <-time.After(60 * time.Second):
			t.Fatal("a forged resume hung")
		}
		noLeak(t, before, "a forged agent resume")
		if err != nil {
			if strings.Contains(err.Error(), "panicked") {
				t.Fatalf("resume panicked: %v", err)
			}
			return
		}
		resumed, err := runtime.ReadLog(bytes.NewReader(rec.Bytes()))
		if err != nil || resumed.End == nil {
			t.Fatalf("the resume ended well but its log does not read back as finished: %v\n%s", err, rec.Bytes())
		}
		hits, applied := a.hits.Load(), a.applied.Load()
		var got bytes.Buffer
		if _, err := Run(a.src, Options{Replay: resumed, Out: &got, Effects: a.effects}); err != nil || got.String() != out.String() {
			t.Fatalf("replay of the resumed log: %v\nprinted %q\nwant    %q", err, got.String(), out.String())
		}
		noLeak(t, before, "replay of a resumed forged log")
		if a.hits.Load() != hits || a.applied.Load() != applied {
			t.Fatalf("the replay of a resumed log went live")
		}
	})
}
