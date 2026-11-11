package evaluator

import (
	"fmt"

	"github.com/delavalom/arvlang/arv/llm"
	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

var agentBuiltin = interpreterBuiltin("agent")

func init() { builtins["agent"] = agentBuiltin }

// agent(model, goal, tools) runs a model in a loop with tools, and returns
// its final text. tools is a HASH from tool name to fn (see agentTools); an
// optional fourth argument, a HASH from tool name to STRING, describes
// them to the model.
//
// Each turn is one call (see complete). When the model stops to use tools,
// each tool_use block runs in order, in this task, and its result goes back
// as a tool_result: a tool the task does not hold as tool(name) is not run
// and the model gets "capability denied: tool(...)" as an error result; a
// tool's own effects are checked against the task's other grants as usual;
// an error from the tool goes back as an error result. Every result is
// redacted (runtime.RedactDenials) before the model sees it. A budget or
// cancellation error stops the agent with that error. The agent stops at
// any other stop reason, returning the text, or with a budget error once
// it would start a turn past max_turns.
func (in *Interpreter) agent(args []value.Object) value.Object {
	if len(args) != 3 && len(args) != 4 {
		return newError("agent: want 3 or 4 arguments (model, goal, tools[, descriptions]), got %d", len(args))
	}
	spec, ok1 := args[0].(*value.String)
	goal, ok2 := args[1].(*value.String)
	if !ok1 || !ok2 {
		return newError("agent: want (STRING, STRING, HASH), got (%s, %s, %s)", args[0].Type(), args[1].Type(), args[2].Type())
	}
	var descs value.Object
	if len(args) == 4 {
		descs = args[3]
	}
	tools, defs, errv := agentTools(args[2], descs)
	if errv != nil {
		return errv
	}
	m, errv := in.model("agent", spec.Value)
	if errv != nil {
		return errv
	}
	req := llm.Request{Tools: defs, Messages: []llm.Message{userText(goal.Value)}}
	turns := in.top().budget.MaxTurns()
	for turn := 1; ; turn++ {
		if turn > turns {
			return errorOf(&runtime.ExceededError{Kind: "max_turns", Limit: int64(turns)}, runtime.KindBudget)
		}
		resp, errv := in.complete("agent", m, req)
		if errv != nil {
			return errv
		}
		// The whole reply goes back, thinking blocks included, unchanged.
		req.Messages = append(req.Messages, llm.Message{Role: llm.RoleAssistant, Content: resp.Content})
		results, errv := in.runTools(tools, resp)
		if errv != nil {
			return errv
		}
		if len(results) == 0 {
			return &value.String{Value: resp.Text()} // charged in complete
		}
		req.Messages = append(req.Messages, llm.Message{Role: llm.RoleUser, Content: results})
	}
}

// runTools runs the reply's tool_use blocks, in order, and returns one
// tool_result for each; none unless the model stopped to use tools.
func (in *Interpreter) runTools(tools map[string]agentTool, resp llm.Response) ([]llm.Block, *value.Error) {
	if resp.StopReason != llm.StopToolUse {
		return nil, nil
	}
	var results []llm.Block
	for _, use := range resp.Content {
		if use.Type != llm.BlockToolUse {
			continue
		}
		text, isErr, errv := in.runTool(tools, use)
		if errv != nil {
			return nil, errv
		}
		if e := in.charge(blockSize); e != nil {
			return nil, e
		}
		results = append(results, llm.Block{Type: llm.BlockToolResult, ID: use.ID, Text: runtime.RedactDenials(text), IsError: isErr})
	}
	return results, nil
}

// runTool runs one tool call in this task, so with exactly the agent's
// authority, and returns the text for its tool_result and whether it is an
// error. It charges what it builds. A budget or cancellation error is
// returned as errv instead: the agent must stop.
func (in *Interpreter) runTool(tools map[string]agentTool, use llm.Block) (text string, isErr bool, errv *value.Error) {
	fail := func(msg string) (string, bool, *value.Error) {
		return msg, true, in.charge(int64(len(msg)))
	}
	tool, ok := tools[use.Name]
	if !ok {
		return fail(fmt.Sprintf("unknown tool %s", quoteOption(&value.String{Value: use.Name})))
	}
	if err := in.Task.Require(runtime.Capability{Kind: runtime.Tool, Scope: use.Name}); err != nil {
		return fail(err.(*runtime.DeniedError).Redacted())
	}
	strs, err := toolArgs(use.Input, tool.params)
	if err != nil {
		return fail(err.Error())
	}
	vals := make([]value.Object, len(strs))
	for i, s := range strs {
		if e := in.charge(int64(len(s))); e != nil {
			return "", false, e
		}
		vals[i] = &value.String{Value: s}
	}
	result := in.applyFunction(tool.fn, vals)
	if e, ok := result.(*value.Error); ok {
		if k := e.ErrKind(); k == runtime.KindBudget || k == runtime.KindCancelled || in.Task.Err() != nil {
			return "", false, e
		}
		return fail(e.Message)
	}
	if s, ok := result.(*value.String); ok {
		return s.Value, false, nil // already charged
	}
	return fail(fmt.Sprintf("tool %q returned %s; a tool returns a STRING", use.Name, result.Type()))
}
