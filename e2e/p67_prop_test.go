package e2e

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"testing"
)

// I7 for agent runs through the binary, as a property: random agent
// programs (agents, asks, spawned workers racing for one budget, random
// limits) against the local Messages API driven by a random script (which
// tools each turn asks for, how many tokens each reply uses) replay byte
// for byte with no key, no grants, no provider request and no tool run.

// p67Gen is one random program and the script its model follows.
type p67Gen struct {
	r     *rand.Rand
	plan  map[string][][]string // goal -> each turn's tool calls
	usage map[string][2]int     // goal -> input and output tokens of each reply
	goals int
}

func (g *p67Gen) goal(toolless bool) string {
	g.goals++
	name := fmt.Sprintf("g%d", g.goals)
	var turns [][]string
	for range g.r.Intn(3) {
		if toolless {
			break
		}
		var calls []string
		for range 1 + g.r.Intn(3) {
			calls = append(calls, []string{"weather", "notes", "ghost"}[g.r.Intn(3)])
		}
		turns = append(turns, calls)
	}
	g.plan[name] = turns
	g.usage[name] = [2]int{[]int{40, 3000, 6000}[g.r.Intn(3)], []int{5, 500}[g.r.Intn(2)]}
	return name
}

func (g *p67Gen) program() string {
	var b strings.Builder
	b.WriteString("let m = " + sonnet + ";\n" + weatherTools +
		`let try = fn(f) { match scope { f() } { ok(v) => v, err(e) => e["kind"] + ": " + e["message"] } };
let worker = fn(goal) { try(fn() { agent(m, goal, tools) }) };
`)
	opts := []string{fmt.Sprintf("tokens: %d", []int{9000, 13000, 1000000}[g.r.Intn(3)])}
	if g.r.Intn(3) == 0 {
		opts = append(opts, fmt.Sprintf("usd_cents: %d", 5+g.r.Intn(60)))
	}
	if g.r.Intn(2) == 0 {
		opts = append(opts, fmt.Sprintf("max_turns: %d", 1+g.r.Intn(3)))
	}
	fmt.Fprintf(&b, "scope (%s) {\n", strings.Join(opts, ", "))
	grants := []string{`tool("weather")`, `tool("notes")`, `net("127.0.0.1")`}
	for range 1 + g.r.Intn(3) {
		switch g.r.Intn(3) {
		case 0:
			fmt.Fprintf(&b, "  puts(try(fn() { agent(m, %q, tools) }));\n", g.goal(false))
		case 1:
			fmt.Fprintf(&b, "  puts(try(fn() { ask(m, %q) }));\n", g.goal(true))
		default:
			var ts []string
			for i := range 1 + g.r.Intn(3) {
				with := []string{"llm(m)"}
				for _, c := range grants {
					if g.r.Intn(2) == 0 {
						with = append(with, c)
					}
				}
				name := fmt.Sprintf("t%d_%d", g.goals, i)
				fmt.Fprintf(&b, "  let %s = spawn worker(%q) with [%s];\n", name, g.goal(false), strings.Join(with, ", "))
				ts = append(ts, name)
			}
			for _, t := range ts {
				fmt.Fprintf(&b, "  puts(await %s);\n", t)
			}
		}
	}
	b.WriteString("  \"end\"\n}\n")
	return b.String()
}

// respond answers a request by the plan: the goal is the first message's
// text, the turn the number of assistant messages so far. Each reply
// depends only on its request, so it is the same whatever order calls
// arrive in.
func (g *p67Gen) respond(body string) apiReply {
	var req struct {
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal([]byte(body), &req); err != nil || len(req.Messages) == 0 {
		return apiError(400, "invalid_request_error", "unparsable request")
	}
	var goal string
	var blocks []struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(req.Messages[0].Content, &blocks) == nil && len(blocks) > 0 {
		goal = blocks[0].Text
	} else {
		json.Unmarshal(req.Messages[0].Content, &goal)
	}
	turn := 0
	for _, m := range req.Messages {
		if m.Role == "assistant" {
			turn++
		}
	}
	u := g.usage[goal]
	if turn < len(g.plan[goal]) {
		var uses []string
		for i, name := range g.plan[goal][turn] {
			input := `{"city":"Lisbon"}`
			if name != "weather" {
				input = `{"path":"/x"}`
			}
			uses = append(uses, use(fmt.Sprintf("u%d_%d", turn, i), name, input))
		}
		return okTools(u[0], u[1], uses...)
	}
	return okText(fmt.Sprintf("%s done after %d turns", goal, turn), u[0], u[1])
}

func TestP67PropAgentRecordReplay(t *testing.T) {
	t.Parallel()
	for i := range p6Cases(24) {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			t.Parallel()
			g := &p67Gen{r: rand.New(rand.NewSource(int64(i))), plan: map[string][][]string{}, usage: map[string][2]int{}}
			r := newRecorded(t)
			rec, api := runLLMCase(t, llmCase{src: g.program(), respond: g.respond, anyStdout: true, calls: -1, path: r.path, dir: r.dir,
				args: []string{"--record", r.log, "--allow", allowLLM, "--allow", "tool=*", "--allow", "net=127.0.0.1"}})
			if !strings.Contains(rec.stdout, "done after") && !strings.Contains(rec.stdout, "budget") {
				t.Fatalf("the recording answered nothing\n%s", rec)
			}
			noKeyIn(t, r.log)
			api.drain(t) // a call or tool run cancelled in flight is the recording's
			sent, w, n := len(api.requests()), gateArrivals(t, "w-Lisbon"), gateArrivals(t, "notes")
			rep := replayLog(t, r, api)
			grants, ended, _ := strings.Cut(rep.stderr, "\n")
			if rep.code != rec.code || rep.stdout != rec.stdout || ended != rec.stderr || !strings.HasPrefix(grants, "replay: grants: ") {
				t.Fatalf("replay differs\nrecording:\n%s\nreplay:\n%s", rec, rep)
			}
			if len(api.requests()) != sent || gateArrivals(t, "w-Lisbon") != w || gateArrivals(t, "notes") != n {
				t.Fatalf("the replay went live: %d requests, %d tool runs", len(api.requests())-sent,
					gateArrivals(t, "w-Lisbon")+gateArrivals(t, "notes")-w-n)
			}
		})
	}
}
