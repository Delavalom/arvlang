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
