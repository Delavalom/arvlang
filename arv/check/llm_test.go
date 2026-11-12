package check

import (
	"reflect"
	"testing"
)

// Phase 7's builtins: llm and tool are constructors, ask needs llm(model),
// and agent needs llm(model) plus tool(name) for every tool it offers the
// model, since the model may ask for any of them.
func TestModelBuiltinsBecomeNeeds(t *testing.T) {
	needs, notes := checked(t, `
let m = "anthropic:claude-sonnet-5-5";
let tools = {"weather": fn(c) { c }, "notes": fn(p) { p }};
ask(m, "hi");
agent(m, "goal", tools);
agent("openai:gpt-x", "goal", {"search": fn(q) { q }}, {"search": "web"});
spawn f() with [llm(m), tool("weather")];
`)
	want := []string{
		"llm=anthropic:claude-sonnet-5-5", "llm=openai:gpt-x",
		"tool=notes", "tool=search", "tool=weather",
	}
	if !reflect.DeepEqual(needs, want) || notes != nil {
		t.Errorf("needs = %q, notes = %q\nwant needs %q", needs, notes, want)
	}
}

func TestModelBuiltinsThatCannotBeKnown(t *testing.T) {
	needs, notes := checked(t, `
let f = fn(m, ts) { ask(m, "x"); agent(m, "g", ts) };
ask("*", "x");
agent("anthropic:*", "g", {});
agent("anthropic:a", "g", {"bad name": fn() { 1 }});
agent("anthropic:a", "g");
`)
	var wantNeeds []string // the bad tool name and the arity fail before llm is required
	wantNotes := []string{
		"ask at 2:21: model not statically known",
		"agent at 2:34: tools not statically known: each tool the model calls needs tool(name)",
		"agent at 2:34: model not statically known",
		`ask at 3:1: never allowed: model "*": name one model, not a wildcard`,
		`agent at 4:1: never allowed: model "anthropic:*": name one model, not a wildcard`,
		`agent at 5:1: never allowed: tool name "bad name" is not valid`,
		"agent at 6:1: never allowed: 2 arguments (it takes 3 or 4)",
	}
	if !reflect.DeepEqual(needs, wantNeeds) || !reflect.DeepEqual(notes, wantNotes) {
		t.Errorf("needs = %q\nnotes = %q\nwant needs %q\nwant notes %q", needs, notes, wantNeeds, wantNotes)
	}
}

// A tools argument whose keys are not known is a note: each tool the model
// calls needs tool(name), and arv check cannot name them.
func TestAgentToolsNotKnown(t *testing.T) {
	_, notes := checked(t, `
let m = "anthropic:a";
let g = fn(ts) { agent(m, "g", ts) };
agent(m, "g", {"a" + "b": fn() { 1 }, k: fn() { 2 }});
`)
	want := []string{
		"agent at 3:18: tools not statically known: each tool the model calls needs tool(name)",
		"agent at 4:1: tools not statically known: each tool the model calls needs tool(name)",
	}
	if !reflect.DeepEqual(notes, want) {
		t.Errorf("notes = %q\nwant %q", notes, want)
	}
}

// tool("*") covers every tool, and llm("p:*") every model of p.
func TestWildcardModelGrantsCoverNamedOnes(t *testing.T) {
	needs, _ := checked(t, `
spawn f() with [tool("*"), llm("anthropic:*")];
agent("anthropic:a", "g", {"x": fn() { 1 }});
`)
	want := []string{"llm=anthropic:*", "tool=*"}
	if !reflect.DeepEqual(needs, want) {
		t.Errorf("needs = %q, want %q", needs, want)
	}
}
