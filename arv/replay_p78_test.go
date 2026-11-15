package arv

import (
	"bytes"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/delavalom/arvlang/arv/llm"
	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// recordOpts runs src under opts with a log, and returns the output, the
// result's text and the log read back.
func recordOpts(t *testing.T, src string, opts Options) (string, string, *runtime.Log) {
	out, res, l, _ := recordRaw(t, src, opts)
	return out, res, l
}

// recordRaw is recordOpts, with the log's lines' kinds too.
func recordRaw(t *testing.T, src string, opts Options) (string, string, *runtime.Log, []string) {
	t.Helper()
	var out, log bytes.Buffer
	opts.Out, opts.Record = &out, &log
	res, err := Run(src, opts)
	l, rerr := runtime.ReadLog(bytes.NewReader(log.Bytes()))
	if rerr != nil {
		t.Fatalf("reading the log: %v", rerr)
	}
	var kinds []string
	for _, line := range strings.Split(strings.TrimSpace(log.String()), "\n")[1:] {
		var r runtime.LogRecord
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatal(err)
		}
		if r.End == nil {
			kinds = append(kinds, r.Kind)
		}
	}
	return out.String(), resultText(res, err), l, kinds
}

func resultText(res value.Object, err error) string {
	if err != nil {
		return "error: " + err.Error()
	}
	return res.Inspect()
}

// I7 for Phase 7: ask and agent replay with no model host at all. The
// replay sends nothing, prints the same, ends the same, and charges the
// same tokens: the second ask below fails the tokens budget in both runs.
func TestReplayOfModelCalls(t *testing.T) {
	f := llm.NewFake(
		llm.TextReply("Lisbon", 5, 2),
		llm.ToolUseReply(10, 3, llm.ToolUse("u1", "lookup", `{"city": "Lisbon"}`)),
		llm.TextReply("done: 545k", 20, 4),
		llm.Reply{Err: &llm.Error{Provider: "anthropic", Kind: llm.ErrOverloaded, Status: 529, Message: "busy"}},
	)
	host := &llm.Host{Providers: map[string]llm.Provider{"anthropic": f}}
	caps := []runtime.Capability{{Kind: runtime.LLM, Scope: "anthropic:*"}, {Kind: runtime.Tool, Scope: "lookup"}}
	src := `let m = "anthropic:claude-haiku-5-5";
puts(ask(m, "Capital of Portugal?"));
let lookup = fn(city) { "population of " + city + ": " + str(random(1000000)) };
puts(agent(m, "How many live there?", {"lookup": lookup}));
puts(match scope { ask(m, "again") } { ok(v) => v, err(e) => e["kind"] + ": " + e["message"] });
puts(match scope (tokens: 10) { ask(m, "too much") } { ok(v) => v, err(e) => e["kind"] + ": " + e["message"] });
"end"`
	src = strings.ReplaceAll(src, `str(random(1000000))`, `json(random(1000000))`)
	out, res, l, logged := recordRaw(t, src, Options{LLM: host, Caps: caps})
	calls := len(f.Calls())
	if calls != 4 || !strings.Contains(out, "Lisbon\n") || !strings.Contains(out, "overloaded") || !strings.Contains(out, "budget: budget exceeded: tokens") {
		t.Fatalf("recording made %d calls and printed %q", calls, out)
	}
	var rout bytes.Buffer
	rres, rerr := Run(src, Options{Replay: l, Out: &rout})
	if rerr != nil || rout.String() != out || resultText(rres, rerr) != res {
		t.Fatalf("replay: %v; printed %q, want %q", rerr, rout.String(), out)
	}
	if len(f.Calls()) != calls {
		t.Fatalf("the replay called the provider: %d calls, want %d", len(f.Calls()), calls)
	}
	// The log has the model calls, and no key or provider object.
	kinds := map[string]int{}
	for _, k := range logged {
		kinds[k]++
	}
	if kinds["llm"] != 4 || kinds["llm_model"] != 4 {
		t.Fatalf("log kinds = %v, want 4 llm and 4 llm_model records", kinds)
	}
}

// A replay whose program asks the model something else diverges, and
// sends nothing.
func TestReplayOfAChangedPromptDiverges(t *testing.T) {
	f := llm.NewFake(llm.TextReply("a", 1, 1))
	host := &llm.Host{Providers: map[string]llm.Provider{"anthropic": f}}
	caps := []runtime.Capability{{Kind: runtime.LLM, Scope: "anthropic:*"}}
	_, _, l := recordOpts(t, `ask("anthropic:claude-haiku-5-5", "one")`, Options{LLM: host, Caps: caps})
	_, err := Run(`ask("anthropic:claude-haiku-5-5", "two")`, Options{Replay: l, LLM: host})
	var ae *Error
	if !errors.As(err, &ae) || ae.Kind != runtime.KindReplay || !strings.Contains(ae.Msg, `"two"`) {
		t.Fatalf("err = %v, want a divergence naming the new prompt", err)
	}
	if len(f.Calls()) != 1 {
		t.Fatalf("%d calls, want the recording's 1", len(f.Calls()))
	}
}

// I7 for Phase 8.2: every host effect goes through the gate as
// "host:<name>". A replay declares the same effects (their names are
// bound per run) but calls no Fn, and gets the same results, errors and
// charges.
func TestReplayOfHostEffects(t *testing.T) {
	var n atomic.Int64
	dag := func(t *runtime.Task, args ...value.Object) (value.Object, error) {
		a := &value.Array{Elements: []value.Object{&value.String{Value: strings.Repeat("x", 100)}}}
		for range 30 {
			a = &value.Array{Elements: []value.Object{a, a}}
		}
		return value.NewHash(map[string]value.Object{"ok": &value.Boolean{Value: true}, "dag": a}), nil
	}
	failing := func(t *runtime.Task, args ...value.Object) (value.Object, error) {
		return nil, errors.New("cluster unreachable")
	}
	effects := map[string]Effect{"k8s_apply": counted(&n, applied), "k8s_dag": counted(&n, dag), "k8s_fail": counted(&n, failing)}
	src := `puts(k8s_apply("staging", "web.yaml"));
puts(k8s_dag("staging")["ok"] == true);
puts(match scope { k8s_fail("staging") } { ok(v) => v, err(e) => e["kind"] + ": " + e["message"] });
puts(match scope { k8s_apply("prod", "x") } { ok(v) => v, err(e) => e["kind"] });
puts(match scope (alloc_mb: 1) { k8s_dag("staging") } { ok(v) => "fits", err(e) => e["kind"] });
"end"`
	out, res, l, logged := recordRaw(t, src, Options{Caps: []runtime.Capability{staging}, Effects: effects})
	calls := n.Load()
	if calls != 4 || !strings.Contains(out, "effect: k8s_fail: cluster unreachable") || !strings.Contains(out, "capability\n") {
		t.Fatalf("recording made %d calls and printed %q", calls, out)
	}
	host := 0
	for _, k := range logged {
		if strings.HasPrefix(k, "host:") {
			host++
		}
	}
	if host != 4 {
		t.Fatalf("%d host records, want one per call that its grants admitted (4)", host)
	}
	var rout bytes.Buffer
	rres, rerr := Run(src, Options{Replay: l, Out: &rout, Effects: effects})
	if rerr != nil || rout.String() != out || resultText(rres, rerr) != res || n.Load() != calls {
		t.Fatalf("replay: %v after %d calls; printed %q, want %q", rerr, n.Load(), rout.String(), out)
	}
}

// Every builtin a host binds per run (HostBindings) is classified: an
// effect goes through the gate under its own name, and a kind's
// constructor logs nothing.
func TestEveryHostBuiltinIsGatedOrPure(t *testing.T) {
	var n atomic.Int64
	effects := map[string]Effect{"k8s_apply": counted(&n, applied), "k8s_get": counted(&n, applied)}
	_, host, err := hostSetup(Options{Effects: effects})
	if err != nil {
		t.Fatal(err)
	}
	for name := range host {
		src := name + `("staging", "x")`
		_, _, _, kinds := recordRaw(t, src, Options{Caps: []runtime.Capability{staging}, Effects: effects})
		kinds = slices.DeleteFunc(kinds, func(k string) bool { return k == "puts" })
		_, isEffect := effects[name]
		switch {
		case isEffect && (len(kinds) != 1 || kinds[0] != "host:"+name):
			t.Errorf("%s is a host effect logged as %v", name, kinds)
		case !isEffect && len(kinds) != 0:
			t.Errorf("%s is a constructor, logged as %v", name, kinds)
		}
	}
	if len(host) != 3 {
		t.Fatalf("host builtins = %d, want 2 effects and the k8s constructor", len(host))
	}
}
