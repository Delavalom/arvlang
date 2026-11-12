package arv

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"slices"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/llm"
	"github.com/delavalom/arvlang/arv/runtime"
)

// A denied tool never executes: random tool hashes, random grants for the
// run and for the agent (spawned with a subset, I2, or run in the root
// task), and a model that asks for random tools over several turns, with
// well-formed or malformed arguments. Every tool logs its runs, so the
// test sees exactly which ran. A call runs iff its tool is in the hash,
// the agent holds it, and its arguments are well formed; it then runs once,
// in the model's order. Every other call gets an error result (exactly
// "capability denied: tool(...)" for an unheld tool in the hash), and the
// run always ends with the model's answer.

var propTools = []string{"alpha", "beta", "gamma", "delta"}

// toolHash is an Arv HASH of the named tools; tool NAME(x) logs
// ["NAME", x] as a JSON line and returns "NAME:" + x.
func toolHash(names []string) string {
	var pairs []string
	for _, n := range names {
		pairs = append(pairs, fmt.Sprintf(`%q: fn(x) { puts(json([%q, x])); %q + x }`, n, n, n+":"))
	}
	return "{" + strings.Join(pairs, ", ") + "}"
}

func pick(rng *rand.Rand, from []string) []string {
	var out []string
	for _, s := range from {
		if rng.Intn(2) == 0 {
			out = append(out, s)
		}
	}
	return out
}

func toolGrants(names []string) string {
	var g []string
	for _, n := range names {
		g = append(g, fmt.Sprintf("tool(%q)", n))
	}
	return strings.Join(g, ", ")
}

func TestDeniedToolNeverExecutesProperty(t *testing.T) {
	rng := rand.New(rand.NewSource(23))
	rounds := 300
	if testing.Short() {
		rounds = 60
	}
	ran, denied := 0, 0
	for round := 0; round < rounds; round++ {
		inHash := pick(rng, propTools)
		rootAll := rng.Intn(4) == 0
		rootHeld := pick(rng, propTools)
		caps := []runtime.Capability{{Kind: runtime.LLM, Scope: "anthropic:*"}}
		if rootAll {
			caps = append(caps, runtime.Capability{Kind: runtime.Tool, Scope: "*"})
			rootHeld = propTools
		} else {
			for _, n := range rootHeld {
				caps = append(caps, runtime.Capability{Kind: runtime.Tool, Scope: n})
			}
		}
		held, call := rootHeld, `agent(m, "g", tools)`
		switch rng.Intn(3) {
		case 0: // the root task's own grants
		case 1: // a spawned agent holding a subset of them
			held = pick(rng, rootHeld)
			call = `scope { await spawn agent(m, "g", tools) with [llm(m)` + strings.TrimSuffix(", "+toolGrants(held), ", ") + `] }`
		case 2: // a spawned agent holding tool("*"), when the root does
			if rootAll {
				call = `scope { await spawn agent(m, "g", tools) with [llm(m), tool("*")] }`
			}
		}
		// The model's turns: 1 to 3 calls each, to any name, well formed or not.
		var turns [][]llm.Block
		var want [][2]string // name, argument, of each call that must run
		var runs []bool      // whether each call, in order, must run
		names := append([]string{"zeta", "*"}, propTools...)
		for range 1 + rng.Intn(3) {
			var uses []llm.Block
			for range 1 + rng.Intn(3) {
				name, arg := names[rng.Intn(len(names))], fmt.Sprintf("v%d", rng.Intn(100))
				input, ok := fmt.Sprintf(`{"x":%q}`, arg), true
				switch rng.Intn(5) {
				case 0:
					input, ok = `{"x":1}`, false
				case 1:
					input, ok = fmt.Sprintf(`{"x":%q,"y":"z"}`, arg), false
				}
				id := fmt.Sprintf("u%d", len(want)+len(uses)+100*len(turns))
				uses = append(uses, llm.ToolUse(id, name, input))
				must := slices.Contains(inHash, name) && slices.Contains(held, name) && ok
				if must {
					want = append(want, [2]string{name, arg})
				}
				runs = append(runs, must)
			}
			turns = append(turns, uses)
		}
		var replies []llm.Reply
		for _, uses := range turns {
			replies = append(replies, llm.ToolUseReply(10, 10, uses...))
		}
		f := llm.NewFake(append(replies, llm.TextReply("done", 10, 1))...)
		src := "let m = \"anthropic:claude-sonnet-5-5\";\nlet tools = " + toolHash(inHash) + ";\nputs(json([\"answer\", " + call + "]))\n"
		var out bytes.Buffer
		if _, err := Run(src, Options{Out: &out, Caps: caps, LLM: &llm.Host{Providers: map[string]llm.Provider{"anthropic": f}}}); err != nil {
			t.Fatalf("round %d: %v\n%s", round, err, src)
		}
		var got [][2]string
		lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
		for _, line := range lines[:len(lines)-1] {
			var e []string
			if json.Unmarshal([]byte(line), &e) != nil || len(e) != 2 {
				t.Fatalf("round %d: bad log line %q", round, line)
			}
			got = append(got, [2]string{e[0], e[1]})
		}
		if lines[len(lines)-1] != `["answer","done"]` {
			t.Fatalf("round %d: the run did not end with the answer:\n%s\n%s", round, out.String(), src)
		}
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("round %d: tools ran %v, want %v (hash %v, held %v)\n%s", round, got, want, inHash, held, src)
		}
		ran += len(got)
		// Every call got one result, in order; only the ones that ran are ok.
		calls := f.Calls()
		if len(calls) != len(turns)+1 {
			t.Fatalf("round %d: %d model calls, want %d", round, len(calls), len(turns)+1)
		}
		k, w := 0, 0
		for i, uses := range turns {
			msgs := calls[i+1].Messages
			results := msgs[len(msgs)-1].Content
			if len(results) != len(uses) {
				t.Fatalf("round %d turn %d: %d results for %d calls", round, i, len(results), len(uses))
			}
			for j, u := range uses {
				r, must := results[j], runs[k]
				k++
				switch {
				case r.ID != u.ID:
					t.Fatalf("round %d: result %q for call %q", round, r.ID, u.ID)
				case must:
					if r.IsError || r.Text != want[w][0]+":"+want[w][1] {
						t.Fatalf("round %d: call %s(%s) got %+v", round, u.Name, u.Input, r)
					}
					w++
				case !r.IsError:
					t.Fatalf("round %d: call %s(%s) got an ok result %q", round, u.Name, u.Input, r.Text)
				case slices.Contains(inHash, u.Name) && !slices.Contains(held, u.Name):
					denied++
					if r.Text != fmt.Sprintf("capability denied: tool(%q)", u.Name) {
						t.Fatalf("round %d: unheld %s got %q", round, u.Name, r.Text)
					}
				}
			}
		}
	}
	if ran == 0 || denied == 0 {
		t.Fatalf("%d runs and %d denials: the generator does not cover both", ran, denied)
	}
}
