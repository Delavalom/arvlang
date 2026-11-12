package main

import (
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
)

func TestAllowAgentGrants(t *testing.T) {
	opts, file, err := parseRun([]string{"--allow", "llm=anthropic:*", "--allow", "tool=weather", "x.arv"}, "/")
	if err != nil || file != "x.arv" {
		t.Fatalf("parseRun: %v, %q", err, file)
	}
	want := []runtime.Capability{{Kind: runtime.LLM, Scope: "anthropic:*"}, {Kind: runtime.Tool, Scope: "weather"}}
	if len(opts.Caps) != 2 || opts.Caps[0] != want[0] || opts.Caps[1] != want[1] {
		t.Fatalf("caps = %v, want %v", opts.Caps, want)
	}
	if !strings.Contains(usage, "llm=PROVIDER:MODEL") || !strings.Contains(usage, "tool=NAME") {
		t.Fatal("usage does not list the llm and tool kinds")
	}
}

func TestModelBudgetFlags(t *testing.T) {
	opts, _, err := parseRun([]string{"--tokens", "50000", "--usd-cents", "25", "--max-turns", "3", "x.arv"}, "/")
	if err != nil {
		t.Fatal(err)
	}
	if l := opts.Limits; l.Tokens != 50000 || l.USDCents != 25 || l.MaxTurns != 3 {
		t.Fatalf("limits = %+v", l)
	}
	script := writeScript(t, "1")
	for _, args := range [][]string{{"--tokens", "0"}, {"--usd-cents", "922337203685478"}, {"--max-turns", "x"}} {
		code, _, stderr := cli(append(append([]string{"run"}, args...), script)...)
		if code != 2 || !strings.HasPrefix(stderr, "error: "+args[0]) {
			t.Errorf("%v: exit %d, stderr %q", args, code, stderr)
		}
	}
	for _, flag := range []string{"--tokens N", "--usd-cents N", "--max-turns N"} {
		if !strings.Contains(usage, flag) {
			t.Errorf("usage does not list %s", flag)
		}
	}
}
