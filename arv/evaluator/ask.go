package evaluator

import (
	"github.com/delavalom/arvlang/arv/llm"
	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

var askBuiltin = interpreterBuiltin("ask")

func init() { builtins["ask"] = askBuiltin }

// ask(model, prompt) sends prompt to model, which the task must hold as
// llm(model), and returns the reply's text: one call, no tools. The
// prompt goes out with any capability denial in it redacted
// (runtime.RedactDenials). The call is an effect: it is refused before
// the request if the scope's tokens or usd_cents cannot cover it.
func (in *Interpreter) ask(args []value.Object) value.Object {
	if len(args) != 2 {
		return newError("ask: want 2 arguments (model, prompt), got %d", len(args))
	}
	spec, ok1 := args[0].(*value.String)
	prompt, ok2 := args[1].(*value.String)
	if !ok1 || !ok2 {
		return newError("ask: want (STRING, STRING), got (%s, %s)", args[0].Type(), args[1].Type())
	}
	m, errv := in.model("ask", spec.Value)
	if errv != nil {
		return errv
	}
	req := llm.Request{Messages: []llm.Message{userText(prompt.Value)}}
	resp, errv := in.complete("ask", m, req)
	if errv != nil {
		return errv
	}
	return &value.String{Value: resp.Text()} // its bytes are charged in complete
}

// userText is a user message holding text, with denials redacted.
func userText(text string) llm.Message {
	return llm.Message{Role: llm.RoleUser, Content: []llm.Block{{Type: llm.BlockText, Text: runtime.RedactDenials(text)}}}
}
