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
		defs[i].Description = runtime.RedactDenials(text.Value) // as every text a model sees
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
