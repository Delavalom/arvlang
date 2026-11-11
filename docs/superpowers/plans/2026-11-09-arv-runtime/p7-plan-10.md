# Phase 7, task p7.10: agent tool definitions and arguments

> **For agentic workers:** this file is one task, p7.10. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: task p7.9 is committed in your tree. Start every shell command with `cd <your worktree> &&` and use absolute paths: the shell's working directory does not persist. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify` and never push; keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0` (toolchain 1.26.2): `t.Chdir`, `min` and `max` are available; do not use `testing/synctest` or `sync.WaitGroup.Go`. Before a long test run, check `df -h /System/Volumes/Data`; below 1 GB free, stop and report (only `go clean -cache` may be deleted). This machine is heavily loaded: keep wall-clock bounds generous. No test may reach the network or spend money: never set `ARV_LIVE_ANTHROPIC`, and never put a real API key anywhere.
>
> Every code block below was applied, in task order, to a scratch copy of `feat/phases-3-8` (Phases 3, 4 and 5 merged; Phase 6 is not) and passed `gofmt -l .`, `go vet ./...` and `go test -race ./...` (including `e2e/`). Design: `14-phase7-agents.md`. Deviations from it, and why: `p7-plan-1.md`.

### Task p7.10: agent tool definitions and arguments

`agentTools(tools, descs)` turns the hash of tools into what the model is offered: one `llm.ToolDef` per key, sorted by name, whose parameters are the function's parameter names. A key must be a valid tool name (`runtime.ToolName`), a value a `fn`, and there are at most 128 tools. The optional descriptions hash maps a tool name to a STRING of at most 4096 bytes. `toolArgs` turns the model's JSON input into the function's arguments, in parameter order: a JSON object whose keys are exactly the parameters, each a string. Anything else is an error the model will see as a tool result (p7.11), so its text is clipped.

**Files:**
- Create: `arv/evaluator/agenttools.go`
- Create (test): `arv/evaluator/agenttools_test.go`

- [ ] **Step 1: Write the failing tests**

Create `arv/evaluator/agenttools_test.go`:

```go
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
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test -run '^(TestAgentToolDefinitions|TestAgentToolErrors|TestToolArgs|FuzzToolArgs)$' ./arv/evaluator/`

Expected: FAIL: the package does not compile. The output includes:

```text
# github.com/delavalom/arvlang/arv/evaluator [github.com/delavalom/arvlang/arv/evaluator.test]
arv/evaluator/agenttools_test.go:18:24: undefined: agentTools
arv/evaluator/agenttools_test.go:50:20: undefined: agentTools
arv/evaluator/agenttools_test.go:57:15: undefined: toolArgs
```

- [ ] **Step 3: Implement**

Create `arv/evaluator/agenttools.go`:

```go
package evaluator

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/delavalom/arvlang/arv/llm"
	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// Limits on what an agent offers a model: tools per call, and the bytes
// of one description.
const (
	maxAgentTools  = 128
	maxDescription = 4096
)

// agentTool is a tool an agent offers: the Arv function the model's call
// runs, and its parameter names.
type agentTool struct {
	fn     *value.Function
	params []string
}

// agentTools reads agent's tools argument, a HASH from tool name to fn,
// and its optional descriptions, a HASH from tool name to STRING. It
// returns the tools by name and their definitions, sorted by name, so the
// same program always sends the same request. A tool's parameters are its
// function's parameter names, each a required string. Every tool in the
// hash is offered, held or not: a call to one the task does not hold is
// refused when the model makes it (see agent).
func agentTools(tools, descs value.Object) (map[string]agentTool, []llm.ToolDef, *value.Error) {
	h, ok := tools.(*value.Hash)
	if !ok {
		return nil, nil, newError("agent: tools must be a HASH of name to fn, got %s", tools.Type())
	}
	if len(h.Pairs) > maxAgentTools {
		return nil, nil, newError("agent: %d tools, at most %d", len(h.Pairs), maxAgentTools)
	}
	byName := make(map[string]agentTool, len(h.Pairs))
	defs := make([]llm.ToolDef, 0, len(h.Pairs))
	for _, pair := range h.Pairs {
		name, ok := pair.Key.(*value.String)
		if !ok || !runtime.ToolName(name.Value) {
			return nil, nil, newError("agent: tool name %s: want 1 to %d of A-Z, a-z, 0-9, _ and -", quoteOption(pair.Key), runtime.MaxToolName)
		}
		fn, ok := pair.Value.(*value.Function)
		if !ok {
			return nil, nil, newError("agent: tool %q must be a fn, got %s", name.Value, pair.Value.Type())
		}
		params := make([]string, len(fn.Parameters))
		for i, p := range fn.Parameters {
			params[i] = p.Value
		}
		byName[name.Value] = agentTool{fn: fn, params: params}
		defs = append(defs, llm.ToolDef{Name: name.Value, Params: params})
	}
	sort.Slice(defs, func(i, j int) bool { return defs[i].Name < defs[j].Name })
	if descs == nil {
		return byName, defs, nil
	}
	d, ok := descs.(*value.Hash)
	if !ok {
		return nil, nil, newError("agent: descriptions must be a HASH of tool name to STRING, got %s", descs.Type())
	}
	for _, pair := range d.Pairs {
		name, ok1 := pair.Key.(*value.String)
		text, ok2 := pair.Value.(*value.String)
		if !ok1 || !ok2 {
			return nil, nil, newError("agent: descriptions must map STRING to STRING, got %s to %s", pair.Key.Type(), pair.Value.Type())
		}
		if _, known := byName[name.Value]; !known {
			return nil, nil, newError("agent: description for %s, which is not a tool", quoteOption(name))
		}
		if len(text.Value) > maxDescription {
			return nil, nil, newError("agent: description of %q is %d bytes, at most %d", name.Value, len(text.Value), maxDescription)
		}
		i := sort.Search(len(defs), func(i int) bool { return defs[i].Name >= name.Value })
		defs[i].Description = text.Value
	}
	return byName, defs, nil
}

// toolArgs decodes a tool_use block's input, which the model wrote, into
// the function's arguments: a JSON object holding exactly params, each a
// string. Its errors go back to the model as tool results, so they name
// what is wrong and quote at most a short key.
func toolArgs(input json.RawMessage, params []string) ([]string, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(input, &obj); err != nil || obj == nil {
		return nil, fmt.Errorf("arguments must be a JSON object")
	}
	args := make([]string, len(params))
	for i, p := range params {
		raw, ok := obj[p]
		if !ok {
			return nil, fmt.Errorf("missing argument %q", p)
		}
		if err := json.Unmarshal(raw, &args[i]); err != nil || raw[0] != '"' {
			return nil, fmt.Errorf("argument %q must be a string", p)
		}
		delete(obj, p)
	}
	for k := range obj {
		return nil, fmt.Errorf("unknown argument %s", quoteOption(&value.String{Value: k}))
	}
	return args, nil
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test -race -run '^(TestAgentToolDefinitions|TestAgentToolErrors|TestToolArgs|FuzzToolArgs)$' ./arv/evaluator/`, then `gofmt -l .`, `go vet ./...` and `go test -race ./...`.
Expected: PASS everywhere.

- [ ] **Step 5: Commit**

```bash
git add arv/evaluator/agenttools_test.go arv/evaluator/agenttools.go
git commit -m "feat(evaluator): agent tool definitions and arguments

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
