package evaluator

import (
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

func TestAgentCapabilityValues(t *testing.T) {
	for src, want := range map[string]string{
		`llm("anthropic:claude-sonnet-5-5")`: `llm("anthropic:claude-sonnet-5-5")`,
		`llm("anthropic:*")`:                 `llm("anthropic:*")`,
		`tool("weather")`:                    `tool("weather")`,
	} {
		got, _ := runWith(t, src)
		if c, ok := got.(*value.Capability); !ok || c.Inspect() != want {
			t.Errorf("%s = %v, want %s", src, got, want)
		}
	}
	got, _ := runWith(t, `tool("no spaces")`)
	wantErrorHas(t, got, "tool name")
}

// I2: spawn hands a child an llm or tool grant only if the parent holds it.
func TestSpawnAttenuatesAgentGrants(t *testing.T) {
	src := `scope { await spawn len("x") with [llm("anthropic:claude-haiku-5-5"), tool("weather")] }`
	got, _ := runWith(t, src, grant(t, runtime.LLM, "anthropic:*"), grant(t, runtime.Tool, "*"))
	if i, ok := got.(*value.Integer); !ok || i.Value != 1 {
		t.Fatalf("got %v, want 1", got)
	}
	got, _ = runWith(t, src, grant(t, runtime.LLM, "anthropic:claude-opus-5-5"), grant(t, runtime.Tool, "*"))
	wantDenied(t, got, `llm("anthropic:claude-haiku-5-5")`)
}
