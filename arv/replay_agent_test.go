package arv

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/delavalom/arvlang/arv/llm"
	"github.com/delavalom/arvlang/arv/runtime"
)

// agentTools is every kind of tool an agent can have: each one's effect
// (a request, a file written and read, the environment, a host effect,
// randomness and time), one the agent does not hold, and one that fails.
const agentTools = `let m = "anthropic:claude-haiku-5-5";
let tools = {
  "fetch": fn(arg) { http_get("{{SRV}}" + arg)["body"] },
  "save": fn(arg) { write_file("{{DIR}}/" + arg, "saved " + arg); "wrote " + arg },
  "load": fn(arg) { read_file("{{DIR}}/" + arg) },
  "home": fn(arg) { getenv(arg) },
  "deploy": fn(arg) { k8s_apply("staging", arg) },
  "dice": fn(arg) { json(random(1000000)) + " at " + json(now()) },
  "secret": fn(arg) { "never runs" },
  "broken": fn(arg) { error("tool broke on " + arg) }
};
`

// agentRun is an agent run that uses every tool, a spawned agent with
// fewer grants, and an agent stopped by max_turns.
const agentRun = agentTools + `let worker = fn(goal) { agent(m, goal, tools) };
scope (tokens: 100000, max_turns: 4) {
  puts(agent(m, "main", tools));
  let t = spawn worker("child") with [llm(m), tool("fetch"), tool("deploy"), net("127.0.0.1"), k8s("staging")];
  puts(await t);
  puts(match scope { agent(m, "loop", tools) } { ok(v) => v, err(e) => e["kind"] + ": " + e["message"] });
  "end"
}`

var agentPlan = agentScript{
	"main":  {{"fetch:/a", "save:out.txt", "home:ARV_P67_HOME", "deploy:web.yaml", "dice:x", "secret:x", "broken:x"}, {"load:out.txt"}},
	"child": {{"fetch:/b", "deploy:api.yaml", "save:denied.txt"}},
	"loop":  {{"dice:x"}, {"dice:x"}, {"dice:x"}, {"dice:x"}, {"dice:x"}},
}

// agentWorld is what agentRun needs: a server, a directory, the grants,
// and the host effect, with counters for each.
type agentWorld struct {
	src     string
	dir     string
	hits    *atomic.Int64
	applied *atomic.Int64
	effects map[string]Effect
	caps    []runtime.Capability
}

func newAgentWorld(t *testing.T, src string) *agentWorld {
	srv, hits := countedServer(t)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	w := &agentWorld{dir: dir, hits: hits, applied: new(atomic.Int64)}
	w.src = strings.NewReplacer("{{SRV}}", srv.URL, "{{DIR}}", dir).Replace(src)
	w.effects = map[string]Effect{"k8s_apply": counted(w.applied, applied)}
	w.caps = []runtime.Capability{{Kind: runtime.LLM, Scope: "anthropic:*"}, {Kind: runtime.Net, Scope: "127.0.0.1"},
		{Kind: runtime.FSRead, Scope: dir}, {Kind: runtime.FSWrite, Scope: dir}, {Kind: runtime.Env, Scope: "ARV_P67_HOME"}, staging}
	for _, name := range []string{"fetch", "save", "load", "home", "deploy", "dice", "broken"} {
		w.caps = append(w.caps, runtime.Capability{Kind: runtime.Tool, Scope: name})
	}
	t.Setenv("ARV_P67_HOME", "/home/recorded")
	return w
}

// I7 for agents: a replay of an agent run prints byte for byte what the
// recording printed and ends the same, with no model host, no grants, a
// different environment and the files gone. It makes no provider call
// and runs no tool effect: no request, no file, no host call.
func TestReplayOfAnAgentRunsNoToolEffect(t *testing.T) {
	w := newAgentWorld(t, agentRun)
	fake := agentPlan.fake()
	out, res, l := recordOpts(t, w.src, Options{LLM: &llm.Host{Providers: map[string]llm.Provider{"anthropic": fake}}, Caps: w.caps, Effects: w.effects})
	for _, want := range []string{"ok: body of /a", "ok: wrote out.txt", "ok: /home/recorded", "ok: applied to staging", " at ",
		`error: capability denied: tool("secret")`, "error: tool broke on x", "ok: saved out.txt",
		"child => ok: body of /b | ok: applied to staging | error: capability denied: tool(\"save\")",
		"budget: budget exceeded: max_turns (limit 4)"} {
		if !strings.Contains(out, want) {
			t.Fatalf("the recording lacks %q:\n%s", want, out)
		}
	}
	calls, hits, applied := len(fake.Calls()), w.hits.Load(), w.applied.Load()
	if calls != 9 || hits != 2 || applied != 2 || res != `end` {
		t.Fatalf("recording: %d calls, %d requests, %d host calls, result %s", calls, hits, applied, res)
	}
	if err := os.Remove(filepath.Join(w.dir, "out.txt")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ARV_P67_HOME", "/home/elsewhere")
	var got bytes.Buffer
	rres, err := Run(w.src, Options{Replay: l, Out: &got, Effects: w.effects})
	if err != nil || got.String() != out || resultText(rres, err) != res {
		t.Fatalf("replay: %v\nprinted %q\nwant    %q", err, got.String(), out)
	}
	if len(fake.Calls()) != calls || w.hits.Load() != hits || w.applied.Load() != applied {
		t.Fatalf("the replay ran effects: %d calls, %d requests, %d host calls", len(fake.Calls())-calls, w.hits.Load()-hits, w.applied.Load()-applied)
	}
	if _, err := os.Stat(filepath.Join(w.dir, "out.txt")); !os.IsNotExist(err) {
		t.Fatalf("the replay wrote a file: %v", err)
	}
}
