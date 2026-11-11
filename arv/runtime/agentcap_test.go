package runtime

import (
	"strings"
	"testing"
)

func TestAgentScopesValidate(t *testing.T) {
	good := []Capability{
		{LLM, "*"}, {LLM, "anthropic:*"}, {LLM, "anthropic:claude-sonnet-5-5"},
		{LLM, "fake:model.v1@2026"}, {Tool, "*"}, {Tool, "weather"}, {Tool, "get_weather-2"},
		{Tool, strings.Repeat("a", MaxToolName)},
	}
	for _, c := range good {
		got, err := Literal(c.Kind, c.Scope)
		if err != nil || got != c {
			t.Errorf("Literal(%s) = %v, %v; want it unchanged", c, got, err)
		}
	}
	bad := []Capability{
		{LLM, "anthropic"}, {LLM, ":model"}, {LLM, "Anthropic:x"}, {LLM, "anthropic:"},
		{LLM, "anthropic:-x"}, {LLM, "anthropic:a b"}, {LLM, "*:x"}, {LLM, "anthropic:x:y"},
		{Tool, "a b"}, {Tool, "a.b"}, {Tool, "we*"}, {Tool, strings.Repeat("a", MaxToolName+1)},
		{LLM, ""}, {Tool, ""},
	}
	for _, c := range bad {
		if _, err := Literal(c.Kind, c.Scope); err == nil {
			t.Errorf("Literal(%s) succeeded, want an error", c)
		}
	}
}

func TestAgentCoverage(t *testing.T) {
	cases := []struct {
		grant, req Capability
		want       bool
	}{
		{Capability{LLM, "anthropic:claude-haiku-5-5"}, Capability{LLM, "anthropic:claude-haiku-5-5"}, true},
		{Capability{LLM, "anthropic:claude-haiku-5-5"}, Capability{LLM, "anthropic:claude-sonnet-5-5"}, false},
		{Capability{LLM, "anthropic:*"}, Capability{LLM, "anthropic:claude-sonnet-5-5"}, true},
		{Capability{LLM, "anthropic:*"}, Capability{LLM, "anthropic:*"}, true},
		{Capability{LLM, "anthropic:*"}, Capability{LLM, "anthropicx:m"}, false},
		{Capability{LLM, "anthropic:*"}, Capability{LLM, "*"}, false},
		{Capability{LLM, "*"}, Capability{LLM, "fake:m"}, true},
		{Capability{LLM, "anthropic:claude-haiku-5-5"}, Capability{LLM, "anthropic:*"}, false},
		{Capability{Tool, "weather"}, Capability{Tool, "weather"}, true},
		{Capability{Tool, "weather"}, Capability{Tool, "notes"}, false},
		{Capability{Tool, "*"}, Capability{Tool, "notes"}, true},
		{Capability{Tool, "weather"}, Capability{Tool, "*"}, false},
		{Capability{Tool, "*"}, Capability{LLM, "*"}, false},
	}
	for _, c := range cases {
		if got := c.grant.Covers(c.req); got != c.want {
			t.Errorf("%s covers %s = %v, want %v", c.grant, c.req, got, c.want)
		}
	}
}

// I2: a child can narrow an llm or tool grant, never widen it.
func TestAgentAttenuation(t *testing.T) {
	parent := NewCapSet(Capability{LLM, "anthropic:*"}, Capability{Tool, "weather"})
	if _, err := parent.Attenuate([]Capability{{LLM, "anthropic:claude-haiku-5-5"}, {Tool, "weather"}}); err != nil {
		t.Fatalf("narrowing: %v", err)
	}
	for _, wider := range []Capability{{LLM, "*"}, {LLM, "other:m"}, {Tool, "*"}, {Tool, "notes"}} {
		_, err := parent.Attenuate([]Capability{wider})
		if _, ok := err.(*DeniedError); !ok {
			t.Errorf("Attenuate(%s) = %v, want *DeniedError", wider, err)
		}
	}
}

func TestParseAgentGrants(t *testing.T) {
	for in, want := range map[string]Capability{
		"llm=anthropic:*":               {LLM, "anthropic:*"},
		"llm=anthropic:claude-opus-5-5": {LLM, "anthropic:claude-opus-5-5"},
		"tool=weather":                  {Tool, "weather"},
		"tool=*":                        {Tool, "*"},
	} {
		got, err := Parse(in, "/")
		if err != nil || got != want {
			t.Errorf("Parse(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	if _, err := Parse("llm=anthropic", "/"); err == nil {
		t.Error(`Parse("llm=anthropic") succeeded`)
	}
}

func TestAgentScopeErrorIsClipped(t *testing.T) {
	_, err := Literal(Tool, strings.Repeat("x", 4000)+" ")
	if err == nil || len(err.Error()) > 200 {
		t.Fatalf("error = %v (%d bytes), want a short error", err, len(err.Error()))
	}
}
