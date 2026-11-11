package evaluator

import (
	"github.com/delavalom/arvlang/arv/llm"
	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// maxTokens is the most output tokens one model call may write.
const maxTokens = 4096

// blockSize is what one content block of a model's reply costs beyond its
// bytes: the Block and its place in a slice.
const blockSize = 160

// interpreterBuiltin is a builtin that applyFunction runs on the
// interpreter itself (see interpreterBuiltins): ask and agent need the
// scope's budget, and agent calls Arv functions in its own task.
func interpreterBuiltin(name string) *value.Builtin {
	return &value.Builtin{Fn: func(t *runtime.Task, args ...value.Object) value.Object {
		return newError("%s: needs an interpreter", name)
	}}
}

// model is one model a call may use: checked against the task's grants,
// with the host's provider and price for it.
type model struct {
	spec     string // "provider:model"
	provider llm.Provider
	id       string // the provider's model ID
	price    llm.Price
	priced   bool
}

// model checks that the task holds llm(spec) and finds the host's
// provider for it. The capability is checked before anything about the
// host is revealed (I1).
func (in *Interpreter) model(fn, spec string) (model, *value.Error) {
	if _, _, err := llm.ParseModel(spec); err != nil {
		return model{}, newError("%s: %s", fn, err)
	}
	if err := in.Task.Require(runtime.Capability{Kind: runtime.LLM, Scope: spec}); err != nil {
		return model{}, errorOf(err, runtime.KindCapability)
	}
	host := llm.HostFrom(in.Task.Ctx)
	if host == nil {
		return model{}, newErrorKind(runtime.KindEffect, "%s: the host configured no model providers", fn)
	}
	p, id, err := host.Lookup(spec)
	if err != nil {
		return model{}, newErrorKind(runtime.KindEffect, "%s: %s", fn, err)
	}
	price, ok := host.Price(spec)
	return model{spec: spec, provider: p, id: id, price: price, priced: ok}, nil
}

// complete makes one call to m: it calls the provider in the task's
// context and charges the reply's bytes to alloc_mb. A refusal, a reply
// cut off at max_tokens, or a stop reason it does not know is an effect
// error.
func (in *Interpreter) complete(fn string, m model, req llm.Request) (llm.Response, *value.Error) {
	req.Model, req.MaxTokens = m.id, maxTokens
	resp, err := m.provider.Complete(in.Task.Ctx, req)
	if err != nil {
		if terr := in.Task.Err(); terr != nil {
			return llm.Response{}, newErrorKind(runtime.KindCancelled, "%s in task %s cancelled: %s", fn, in.Task.ID, terr)
		}
		return llm.Response{}, newErrorKind(runtime.ErrorKind(err, runtime.KindEffect), "%s: %s", fn, err)
	}
	if e := in.charge(replySize(resp)); e != nil {
		return llm.Response{}, e
	}
	switch resp.StopReason {
	case llm.StopEndTurn, llm.StopSequence, llm.StopToolUse:
		return resp, nil
	case llm.StopRefusal:
		return llm.Response{}, newErrorKind(runtime.KindEffect, "%s: %s refused the request", fn, m.spec)
	case llm.StopMaxTokens:
		return llm.Response{}, newErrorKind(runtime.KindEffect, "%s: the reply reached max_tokens (%d) before it ended", fn, maxTokens)
	}
	return llm.Response{}, newErrorKind(runtime.KindEffect, "%s: unexpected stop reason %q", fn, clipped(resp.StopReason))
}

// replySize is what a reply costs to keep: its blocks and their bytes.
func replySize(resp llm.Response) int64 {
	n := int64(0)
	for _, c := range resp.Content {
		n += blockSize + int64(len(c.Text)+len(c.ID)+len(c.Name)+len(c.Input)+len(c.Raw))
	}
	return n
}
