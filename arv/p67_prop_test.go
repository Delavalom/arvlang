package arv

import (
	"bytes"
	"context"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/llm"
	"github.com/delavalom/arvlang/arv/runtime"
)

// I7 for agent runs, as a property: random agent programs against a fake
// model driven by a random script (which tools each turn calls, how many
// tokens each reply uses) record and replay byte for byte, with no
// provider call and no tool effect, and no goroutine outlives a run.

// agentGen builds one random agent program and the script its model
// follows.
type agentGen struct {
	r      *rand.Rand
	dir    string
	plan   agentScript
	usage  map[string][2]int // goal -> input and output tokens of each reply
	goals  int
	spawns int
	seq    bool // no spawned workers and no tool whose result is random
}

// genTools are the tools a plan may call: every one of agentTools, and
// one the agent's map does not have.
var genTools = []string{"fetch:/p", "save:f.txt", "load:f.txt", "load:none.txt", "home:ARV_P67_HOME",
	"deploy:web.yaml", "dice:x", "secret:x", "broken:x", "ghost:x"}

// grants are what a spawned worker may be given (a random subset).
var genGrants = []string{`llm(m)`, `tool("fetch")`, `tool("save")`, `tool("load")`, `tool("home")`, `tool("deploy")`,
	`tool("dice")`, `tool("broken")`, `net("127.0.0.1")`, `fs_read("{{DIR}}")`, `fs_write("{{DIR}}")`,
	`env("ARV_P67_HOME")`, `k8s("staging")`}

// goal adds a goal with a random plan of up to 3 turns (none when
// toolless, for an ask) and returns its name.
func (g *agentGen) goal(toolless bool) string {
	g.goals++
	name := fmt.Sprintf("g%d", g.goals)
	var turns [][]string
	for range g.r.Intn(4) {
		if toolless {
			break
		}
		var calls []string
		for range 1 + g.r.Intn(3) {
			call := genTools[g.r.Intn(len(genTools))]
			if g.seq && call == "dice:x" {
				call = "fetch:/q"
			}
			calls = append(calls, call)
		}
		turns = append(turns, calls)
	}
	g.plan[name] = turns
	// Usage: small, near a reservation (about 4100), or well past it.
	in := []int{100, 3000, 6000}[g.r.Intn(3)]
	g.usage[name] = [2]int{in, []int{10, 500}[g.r.Intn(2)]}
	return name
}

// worker spawns a worker for a new goal, with random grants.
func (g *agentGen) worker(b *strings.Builder) string {
	g.spawns++
	var grants []string
	for _, c := range genGrants {
		if c == `llm(m)` || g.r.Intn(2) == 0 {
			grants = append(grants, strings.ReplaceAll(c, "{{DIR}}", g.dir))
		}
	}
	name := fmt.Sprintf("t%d", g.spawns)
	fmt.Fprintf(b, "  let %s = spawn worker(%q) with [%s];\n", name, g.goal(false), strings.Join(grants, ", "))
	return name
}

// program is a random sequence of agents, asks and groups of spawned
// workers racing for the same budgets, inside a scope with random limits.
func (g *agentGen) program() string {
	var b strings.Builder
	// The caller puts the world's agentTools (m and tools) first.
	b.WriteString(`let try = fn(f) { match scope { f() } { ok(v) => v, err(e) => e["kind"] + ": " + e["message"] } };
let worker = fn(goal) { try(fn() { agent(m, goal, tools) }) };
`)
	var opts []string
	if g.r.Intn(3) > 0 {
		opts = append(opts, fmt.Sprintf("tokens: %d", []int{9000, 13000, 30000, 1000000}[g.r.Intn(4)]))
	}
	if g.r.Intn(3) == 0 {
		opts = append(opts, fmt.Sprintf("usd_cents: %d", 1+g.r.Intn(20)))
	}
	if g.r.Intn(2) == 0 {
		opts = append(opts, fmt.Sprintf("max_turns: %d", 1+g.r.Intn(4)))
	}
	fmt.Fprintf(&b, "scope (%s) {\n", strings.Join(opts, ", "))
	if len(opts) == 0 {
		b.Reset()
		return g.program()
	}
	for range 1 + g.r.Intn(4) {
		n := 3
		if g.seq {
			n = 2
		}
		switch g.r.Intn(n) {
		case 0:
			fmt.Fprintf(&b, "  puts(try(fn() { agent(m, %q, tools) }));\n", g.goal(false))
		case 1:
			fmt.Fprintf(&b, "  puts(try(fn() { ask(m, %q) }));\n", g.goal(true))
		default:
			var ts []string
			for range 1 + g.r.Intn(3) {
				ts = append(ts, g.worker(&b))
			}
			for _, t := range ts {
				fmt.Fprintf(&b, "  puts(await %s);\n", t)
			}
		}
	}
	b.WriteString("  \"end\"\n}\n")
	return b.String()
}

// fake answers by g's plan, with g's usage.
func (g *agentGen) fake() *llm.Fake {
	return &llm.Fake{Then: func(ctx context.Context, req llm.Request) (llm.Response, error) {
		resp, err := g.plan.reply(ctx, req)
		u := g.usage[req.Messages[0].Content[0].Text]
		resp.InputTokens, resp.OutputTokens = u[0], u[1]
		return resp, err
	}}
}

func agentPropCases() int {
	if testing.Short() {
		return 50
	}
	return 200
}

func TestPropAgentRunsReplayIdentically(t *testing.T) {
	w := newAgentWorld(t, agentTools)
	before := settle(0)
	seen := map[string]int{}
	for i := range agentPropCases() {
		g := &agentGen{r: rand.New(rand.NewSource(int64(i))), dir: w.dir, plan: agentScript{}, usage: map[string][2]int{}}
		src := w.src + g.program()
		fake := g.fake()
		var out, log bytes.Buffer
		t.Setenv("ARV_P67_HOME", "/home/recorded")
		res, err := Run(src, Options{LLM: &llm.Host{Providers: map[string]llm.Provider{"anthropic": fake}}, Caps: w.caps,
			Effects: w.effects, Out: &out, Record: &log, Ctx: hostCtx(t)})
		noLeak(t, before, src)
		want := resultText(res, err)
		if err != nil {
			t.Fatalf("case %d: the recording failed: %v\n%s", i, err, src)
		}
		for _, k := range []string{"llm_reserve", "llm_settle", "llm", "http_get", "host:k8s_apply", "write_file", "random"} {
			seen[k] += bytes.Count(log.Bytes(), []byte(`"kind":"`+k+`","req"`))
		}
		seen["refused"] += bytes.Count(log.Bytes(), []byte("budget exceeded"))
		calls, hits, applied := len(fake.Calls()), w.hits.Load(), w.applied.Load()
		if _, err := os.Stat(w.dir + "/f.txt"); err == nil {
			seen["file written"]++
		}
		clearDir(t, w.dir)
		t.Setenv("ARV_P67_HOME", "/home/elsewhere")
		for k := range 2 {
			l, err := runtime.ReadLog(bytes.NewReader(log.Bytes()))
			if err != nil {
				t.Fatalf("case %d: reading the log: %v", i, err)
			}
			var got bytes.Buffer
			rres, rerr := Run(src, Options{Replay: l, Out: &got, Effects: w.effects, Ctx: hostCtx(t)})
			noLeak(t, before, src)
			if got.String() != out.String() || resultText(rres, rerr) != want {
				t.Fatalf("case %d, replay %d: %v\nprinted %q\nwant    %q\n%s", i, k, rerr, got.String(), out.String(), src)
			}
		}
		if len(fake.Calls()) != calls || w.hits.Load() != hits || w.applied.Load() != applied {
			t.Fatalf("case %d: the replay ran effects: %d calls, %d requests, %d host calls\n%s", i,
				len(fake.Calls())-calls, w.hits.Load()-hits, w.applied.Load()-applied, src)
		}
		if entries, _ := os.ReadDir(w.dir); len(entries) != 0 {
			t.Fatalf("case %d: the replay wrote %s", i, entries[0].Name())
		}
	}
	// The generator must reach every record kind and refusal, or the
	// property says little.
	seen["requests"], seen["host calls"] = int(w.hits.Load()), int(w.applied.Load())
	for _, k := range []string{"llm_reserve", "llm_settle", "llm", "http_get", "host:k8s_apply", "write_file", "random", "refused",
		"file written", "requests", "host calls"} {
		if seen[k] == 0 {
			t.Errorf("no case logged %s", k)
		}
	}
	t.Logf("records over %d cases: %v", agentPropCases(), seen)
}
