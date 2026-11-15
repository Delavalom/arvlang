package arv

import (
	"bytes"
	"context"
	"sync"
	"testing"

	"github.com/delavalom/arvlang/arv/llm"
	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// orderedPair is two tasks, a and b, calling the same model under one
// scope's model budget. The recording orders them: b waits in the host
// effect k8s_wait until a has called k8s_done. A replay answers both host
// effects from the log, so b runs at once, while a first spins: b now
// reaches the model budget first. The log, not the replay's order, must
// decide what each call got (I7).
type orderedPair struct {
	effects map[string]Effect
	host    *llm.Host
	fake    *llm.Fake
	caps    []runtime.Capability
}

func newOrderedPair(then func(ctx context.Context, req llm.Request, aDone <-chan struct{}) (llm.Response, error)) *orderedPair {
	done := make(chan struct{})
	var once sync.Once
	str := func(s string) value.Object { return &value.String{Value: s} }
	p := &orderedPair{effects: map[string]Effect{
		"k8s_done": {Kind: "k8s", Scope: nsScope, Fn: func(t *runtime.Task, a ...value.Object) (value.Object, error) {
			once.Do(func() { close(done) })
			return str("done"), nil
		}},
		"k8s_wait": {Kind: "k8s", Scope: nsScope, Fn: func(t *runtime.Task, a ...value.Object) (value.Object, error) {
			select {
			case <-done:
			case <-t.Ctx.Done():
			}
			return str("waited"), nil
		}},
	}, caps: []runtime.Capability{{Kind: runtime.LLM, Scope: "anthropic:*"}, staging}}
	p.fake = &llm.Fake{Then: func(ctx context.Context, req llm.Request) (llm.Response, error) { return then(ctx, req, done) }}
	p.host = &llm.Host{Providers: map[string]llm.Provider{"anthropic": p.fake},
		Prices: map[string]llm.Price{"anthropic:claude-haiku-5-5": {Input: 10_000_000, Output: 10_000_000}}}
	return p
}

// orderedPairSrc runs a and b under the scope options opts; each prints
// its answer, or its error's kind and message.
func orderedPairSrc(opts string) string {
	return spinSrc() + `let m = "anthropic:claude-haiku-5-5";
let try = fn(p) { match scope { ask(m, p) } { ok(v) => v, err(e) => e["kind"] + ": " + e["message"] } };
let first = fn() { spin(); let r = try("a"); k8s_done("staging"); r };
let second = fn() { k8s_wait("staging"); try("b") };
scope (` + opts + `) {
  let ta = spawn first() with [llm(m), k8s("staging")];
  let tb = spawn second() with [llm(m), k8s("staging")];
  puts(await ta); puts(await tb);
  puts(try("c"));
}`
}

// replaysAlike replays log three times; each must print want and end well.
func replaysAlike(t *testing.T, src, want string, log []byte, effects map[string]Effect) {
	t.Helper()
	for i := range 3 {
		l, err := runtime.ReadLog(bytes.NewReader(log))
		if err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		_, err = Run(src, Options{Replay: l, Out: &out, Effects: effects})
		if err != nil || out.String() != want {
			t.Fatalf("replay %d: %v; printed %q, want %q", i, err, out.String(), want)
		}
	}
}

// Two tasks race for a model budget that holds one call's reservation:
// whichever reserves first is answered and the other refused. The replay
// refuses the same one, so it prints what the recording printed. Then c
// asks for more than the 4000 tokens (or 4 cents) a left: a resume cut
// before c runs c live, against the budget its replayed part spent, and
// is refused too.
func TestReplayOfConcurrentModelBudgets(t *testing.T) {
	for _, opts := range []string{"tokens: 7000", "usd_cents: 7"} {
		t.Run(opts, func(t *testing.T) {
			p := newOrderedPair(func(ctx context.Context, req llm.Request, _ <-chan struct{}) (llm.Response, error) {
				return llm.TextReply(req.Messages[0].Content[0].Text+" answered", 2000, 1000).Response, nil
			})
			src := orderedPairSrc(opts)
			out, log, err := recordRun(t, src, Options{LLM: p.host, Caps: p.caps, Effects: p.effects})
			refused := "budget: budget exceeded: " + map[string]string{"tokens: 7000": "tokens (limit 7000)", "usd_cents: 7": "usd_cents (limit 7)"}[opts] + "\n"
			want := "a answered\n" + refused + refused
			if err != nil || out != want || len(p.fake.Calls()) != 1 {
				t.Fatalf("recording: %v, %d calls; printed %q, want %q", err, len(p.fake.Calls()), out, want)
			}
			replaysAlike(t, src, out, log, p.effects)
			if len(p.fake.Calls()) != 1 {
				t.Fatalf("the replays called the provider")
			}
			crashed := cutAfter(t, log, `"kind":"puts","req":["budget`)
			l, err := runtime.ReadLog(bytes.NewReader(crashed))
			if err != nil {
				t.Fatal(err)
			}
			var got bytes.Buffer
			_, err = Run(src, Options{LLM: p.host, Caps: p.caps, Effects: p.effects, Resume: l, Record: bytes.NewBuffer(bytes.Clone(crashed)), Out: &got})
			if err != nil || got.String() != want || len(p.fake.Calls()) != 1 {
				t.Fatalf("resume: %v, %d calls; printed %q, want %q", err, len(p.fake.Calls()), got.String(), want)
			}
		})
	}
}
