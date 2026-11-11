package evaluator

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/llm"
	"github.com/delavalom/arvlang/arv/value"
)

func TestAgentToolDefinitions(t *testing.T) {
	src := `[{"weather": fn(city) { city }, "notes": fn(path, query) { path }, "now": fn() { 1 }},
	         {"weather": "Current weather for a city"}]`
	got, _ := runWith(t, src)
	pair := got.(*value.Array).Elements
	byName, defs, errv := agentTools(pair[0], pair[1])
	if errv != nil {
		t.Fatal(errv.Message)
	}
	want := []llm.ToolDef{
		{Name: "notes", Params: []string{"path", "query"}},
		{Name: "now", Params: []string{}},
		{Name: "weather", Description: "Current weather for a city", Params: []string{"city"}},
	}
	if !reflect.DeepEqual(defs, want) {
		t.Fatalf("defs = %+v\nwant %+v", defs, want)
	}
	if len(byName) != 3 || byName["notes"].fn == nil {
		t.Fatalf("byName = %v", byName)
	}
}

func TestAgentToolErrors(t *testing.T) {
	for _, c := range []struct{ tools, descs, msg string }{
		{`[1]`, ``, "agent: tools must be a HASH of name to fn, got ARRAY"},
		{`{"a b": fn() { 1 }}`, ``, `agent: tool name "a b"`},
		{`{1: fn() { 1 }}`, ``, "agent: tool name INTEGER"},
		{`{"t": len}`, ``, `agent: tool "t" must be a fn, got BUILTIN`},
		{`{"t": fn() { 1 }}`, `{"u": "x"}`, `agent: description for "u", which is not a tool`},
		{`{"t": fn() { 1 }}`, `{"t": 1}`, "agent: descriptions must map STRING to STRING, got STRING to INTEGER"},
		{`{"t": fn() { 1 }}`, `"x"`, "agent: descriptions must be a HASH of tool name to STRING, got STRING"},
	} {
		tools, _ := runWith(t, c.tools)
		var descs value.Object
		if c.descs != "" {
			descs, _ = runWith(t, c.descs)
		}
		if _, _, errv := agentTools(tools, descs); errv == nil || !strings.Contains(errv.Message, c.msg) {
			t.Errorf("%s, %s: error %v, want %q", c.tools, c.descs, errv, c.msg)
		}
	}
}

func TestToolArgs(t *testing.T) {
	args, err := toolArgs(json.RawMessage(`{"city": "Lisbon", "when": "today"}`), []string{"city", "when"})
	if err != nil || !reflect.DeepEqual(args, []string{"Lisbon", "today"}) {
		t.Fatalf("toolArgs = %v, %v", args, err)
	}
	if args, err := toolArgs(json.RawMessage(`{}`), nil); err != nil || len(args) != 0 {
		t.Fatalf("no params: %v, %v", args, err)
	}
	for in, msg := range map[string]string{
		`[1]`:                         "arguments must be a JSON object",
		`null`:                        "arguments must be a JSON object",
		`{"city": 3}`:                 `argument "city" must be a string`,
		`{"city": null}`:              `argument "city" must be a string`,
		`{}`:                          `missing argument "city"`,
		`{"city": "x", "extra": "y"}`: `unknown argument "extra"`,
		`{"city": "x"`:                "arguments must be a JSON object",
	} {
		if _, err := toolArgs(json.RawMessage(in), []string{"city"}); err == nil || err.Error() != msg {
			t.Errorf("toolArgs(%s) = %v, want %q", in, err, msg)
		}
	}
}

// The model writes tool inputs: decoding any input either fails with a
// message or yields exactly the declared strings.
func FuzzToolArgs(f *testing.F) {
	f.Add(`{"a": "x", "b": "y"}`, 2)
	f.Add(`{"a": 1}`, 1)
	f.Add(`{"a": "é\ud800"}`, 1)
	f.Fuzz(func(t *testing.T, input string, n int) {
		params := []string{"a", "b", "c"}[:max(0, min(n, 3))]
		args, err := toolArgs(json.RawMessage(input), params)
		if err != nil {
			if len(err.Error()) > 200 {
				t.Fatalf("error is %d bytes", len(err.Error()))
			}
			return
		}
		var obj map[string]string
		if json.Unmarshal([]byte(input), &obj) != nil || len(obj) != len(params) {
			t.Fatalf("toolArgs(%q) succeeded: %v, but it is not an object of %d strings", input, args, len(params))
		}
		for i, p := range params {
			if obj[p] != args[i] {
				t.Fatalf("toolArgs(%q)[%d] = %q, want %q", input, i, args[i], obj[p])
			}
		}
	})
}
