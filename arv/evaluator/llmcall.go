package evaluator

import (
	"math"

	"github.com/delavalom/arvlang/arv/llm"
	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// maxTokens is the most output tokens one model call may write. It is
// reserved from the tokens and usd_cents budgets before every call.
const maxTokens = 4096

// inputOverhead is added to every call's input estimate, for what the
// provider adds around the text (roles, and its tool-use prompt when the
// call offers tools).
const (
	inputOverhead     = 512
	toolInputOverhead = 1024
)

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
	return in.hostModel(fn, spec) // an effect (llmgate.go)
}

// complete makes one call to m: it reserves the most the call can use
// from the scope's tokens and usd_cents budgets (refusing it, with no
// request sent, if they cannot cover that), calls the provider in the
// task's context, settles the reservation to the real usage (a failed
// call to nothing only if llm.CostNothing, else to the whole
// reservation; negative usage is an effect error, charged in full;
// counts too large to add are charged as the most there is), and charges
// the reply's bytes to alloc_mb. A refusal, a reply cut off at
// max_tokens, or a stop reason it does not know is an effect error.
func (in *Interpreter) complete(fn string, m model, req llm.Request) (llm.Response, *value.Error) {
	req.Model, req.MaxTokens = m.id, maxTokens
	b := in.top().budget
	if b.LimitsUSD() && !m.priced {
		return llm.Response{}, newErrorKind(runtime.KindBudget, "%s: no price is known for %s, so its usd_cents cannot be counted", fn, m.spec)
	}
	est := estimateInput(req)
	maxTok, maxUSD := int64(est+req.MaxTokens), m.price.Cost(est, req.MaxTokens)
	r, errv := in.reserve(b, maxTok, maxUSD) // an effect when a pool limits it (llmbudget.go)
	if errv != nil {
		return llm.Response{}, errv
	}
	resp, err, replayErr := in.callModel(m, req) // an effect (llmgate.go)
	if replayErr != nil {
		return llm.Response{}, replayErr
	}
	if err != nil {
		// A call the provider may have billed (cancelled, a network
		// error, an unreadable 200) keeps its whole reservation: the
		// program controls cancellation, so refunding it would let a loop
		// of cancelled calls spend without limit. Only a call that was
		// provably never processed is free.
		if llm.CostNothing(err) {
			r.Settle(0, 0)
		} else {
			r.Settle(maxTok, maxUSD)
		}
		if terr := in.Task.Err(); terr != nil {
			return llm.Response{}, newErrorKind(runtime.KindCancelled, "%s in task %s cancelled: %s", fn, in.Task.ID, terr)
		}
		return llm.Response{}, newErrorKind(runtime.ErrorKind(err, runtime.KindEffect), "%s: %s", fn, err)
	}
	if resp.InputTokens < 0 || resp.OutputTokens < 0 {
		r.Settle(maxTok, maxUSD)
		return llm.Response{}, newErrorKind(runtime.KindEffect, "%s: %s reported negative usage", fn, m.spec)
	}
	used := int64(resp.InputTokens) + int64(resp.OutputTokens)
	if used < 0 { // the sum overflowed: count it as the most there is
		used = math.MaxInt64
	}
	if e := in.settle(b, r, used, m.price.Cost(resp.InputTokens, resp.OutputTokens)); e != nil {
		return llm.Response{}, e
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

// estimateInput bounds the input tokens req can use by its bytes: a token
// is at least one byte of text. The provider's own framing is
// inputOverhead, and toolInputOverhead more when tools are offered.
func estimateInput(req llm.Request) int {
	n := inputOverhead + len(req.System)
	if len(req.Tools) > 0 {
		n += toolInputOverhead
	}
	for _, t := range req.Tools {
		n += 64 + len(t.Name) + len(t.Description)
		for _, p := range t.Params {
			n += 32 + len(p)
		}
	}
	for _, msg := range req.Messages {
		for _, c := range msg.Content {
			n += 16 + len(c.Text) + len(c.ID) + len(c.Name) + len(c.Input) + len(c.Raw)
		}
	}
	return n
}

// replySize is what a reply costs to keep: its blocks and their bytes.
func replySize(resp llm.Response) int64 {
	n := int64(0)
	for _, c := range resp.Content {
		n += blockSize + int64(len(c.Text)+len(c.ID)+len(c.Name)+len(c.Input)+len(c.Raw))
	}
	return n
}
