package evaluator

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/delavalom/arvlang/arv/llm"
	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// The model calls of ask and agent go through the effect gate (I7) as two
// effects: "llm_model", which asks the host for a model's provider, ID
// and price, and "llm", one call to the provider. A replay answers both
// from the log, so it needs no provider and no key, and sends nothing.
// What a call does around them (the reservation from the tokens and
// usd_cents budgets, the alloc_mb charge for the reply, the tool calls an
// agent makes) is the program's own work, which a replay does again.
const (
	effectModel = "llm_model"
	effectCall  = "llm"
)

// hostModel finds the host's provider, model ID and price for spec, which
// the task holds. With a log it is the effect llm_model, so a replay
// prices a call as the recorded run did without asking the host.
func (in *Interpreter) hostModel(fn, spec string) (model, *value.Error) {
	look := func() (model, *value.Error) {
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
	if in.Task.Log() == nil {
		return look()
	}
	var live model
	v := in.effect(effectModel, []value.Object{&value.String{Value: fn}, &value.String{Value: spec}}, func(func(int64) error) value.Object {
		m, errv := look()
		if errv != nil {
			return errv
		}
		live = m
		return modelValue(m)
	})
	if e, ok := v.(*value.Error); ok {
		return model{}, e
	}
	m, err := modelOf(spec, v)
	if err != nil {
		return model{}, errorOf(in.Task.Log().Diverge("at task %s: the log's %s for %s: %s", in.Task.ID, effectModel, spec, err), runtime.KindReplay)
	}
	m.provider = live.provider // nil if the log answered: see liveProvider
	return m, nil
}

// modelValue is what the log keeps of m: its ID and price.
func modelValue(m model) value.Object {
	num := func(n int64) value.Object { return &value.Integer{Value: n} }
	return value.NewHash(map[string]value.Object{
		"id": &value.String{Value: m.id}, "priced": nativeBoolToBooleanObject(m.priced),
		"input": num(m.price.Input), "output": num(m.price.Output), "long_above": num(int64(m.price.LongAbove)),
		"long_input": num(m.price.LongInput), "long_output": num(m.price.LongOutput),
	})
}

// modelOf reads back what modelValue wrote.
func modelOf(spec string, v value.Object) (model, error) {
	h, ok := v.(*value.Hash)
	if !ok {
		return model{}, fmt.Errorf("want a HASH, got %s", v.Type())
	}
	get := func(k string) value.Object {
		p, ok := h.Pairs[(&value.String{Value: k}).HashKey()]
		if !ok {
			return nil
		}
		return p.Value
	}
	num := func(k string) (int64, error) {
		n, ok := get(k).(*value.Integer)
		if !ok {
			return 0, fmt.Errorf("no integer %s", k)
		}
		return n.Value, nil
	}
	id, ok1 := get("id").(*value.String)
	priced, ok2 := get("priced").(*value.Boolean)
	if !ok1 || !ok2 {
		return model{}, errors.New("no id or priced")
	}
	var f [5]int64
	for i, k := range []string{"input", "output", "long_above", "long_input", "long_output"} {
		n, err := num(k)
		if err != nil {
			return model{}, err
		}
		f[i] = n
	}
	return model{spec: spec, id: id.Value, priced: priced.Value,
		price: llm.Price{Input: f[0], Output: f[1], LongAbove: int(f[2]), LongInput: f[3], LongOutput: f[4]}}, nil
}

// callResult is what the log keeps of one provider call: the reply, or
// the error with what the evaluator reads of it (its text, its kind, and
// the provider's fields that CostNothing reads).
type callResult struct {
	Resp *llm.Response `json:"resp,omitempty"`
	Err  *callError    `json:"err,omitempty"`
}

type callError struct {
	Msg      string `json:"msg"`
	Kind     string `json:"kind"`
	Provider string `json:"provider,omitempty"` // set if the error was an *llm.Error
	PKind    string `json:"pkind,omitempty"`
	Status   int    `json:"status,omitempty"`
	PMsg     string `json:"pmsg,omitempty"`
	LLM      bool   `json:"llm,omitempty"`
}

// loggedError is a provider error read back from the log.
type loggedError struct{ msg, kind string }

func (e *loggedError) Error() string     { return e.msg }
func (e *loggedError) ErrorKind() string { return e.kind }

func encodeCall(resp llm.Response, err error) []byte {
	var r callResult
	if err == nil {
		r.Resp = &resp
	} else {
		r.Err = &callError{Msg: err.Error(), Kind: runtime.ErrorKind(err, runtime.KindEffect)}
		var le *llm.Error
		if errors.As(err, &le) {
			r.Err.LLM, r.Err.Provider, r.Err.PKind, r.Err.Status, r.Err.PMsg = true, le.Provider, le.Kind, le.Status, le.Message
		}
	}
	b, merr := json.Marshal(r)
	if merr != nil { // a reply whose raw JSON does not encode: keep it as an error
		b, _ = json.Marshal(callResult{Err: &callError{Msg: "the reply cannot be logged: " + merr.Error(), Kind: runtime.KindEffect}})
	}
	return b
}

func decodeCall(b []byte) (llm.Response, error, error) {
	var r callResult
	if err := json.Unmarshal(b, &r); err != nil {
		return llm.Response{}, nil, err
	}
	switch {
	case r.Resp != nil && r.Err == nil:
		return *r.Resp, nil, nil
	case r.Err != nil && r.Resp == nil && r.Err.LLM:
		return llm.Response{}, &llm.Error{Provider: r.Err.Provider, Kind: r.Err.PKind, Status: r.Err.Status, Message: r.Err.PMsg}, nil
	case r.Err != nil && r.Resp == nil:
		return llm.Response{}, &loggedError{msg: r.Err.Msg, kind: r.Err.Kind}, nil
	}
	return llm.Response{}, nil, errors.New("want one of resp and err")
}

// callModel sends req to m's provider. With a log it is the effect llm,
// whose request is the model and the whole request, so a replay checks
// that the program asked for the same call. A recording goes on with the
// reply as the log has it (its raw JSON compacted), which is what a
// replay reads back. A divergence is replayErr.
func (in *Interpreter) callModel(m model, req llm.Request) (resp llm.Response, err error, replayErr *value.Error) {
	t := in.Task
	if t.Log() == nil {
		resp, err = m.provider.Complete(t.Ctx, req)
		return resp, err, nil
	}
	reqb, merr := json.Marshal(struct {
		Model   string      `json:"model"`
		Request llm.Request `json:"request"`
	}{m.spec, req})
	if merr != nil {
		return llm.Response{}, nil, errorOf(t.Log().Diverge("at task %s: a model request that cannot be logged: %s", t.ID, merr), runtime.KindReplay)
	}
	var forged error
	o, gerr := t.Effect(effectCall, reqb, func() runtime.Outcome {
		p, e := liveProvider(t, m)
		if errors.Is(e, errForgedModel) {
			forged = e // the run has diverged: nothing is logged or sent
		}
		var r llm.Response
		if e == nil {
			r, e = p.Complete(t.Ctx, req)
		}
		res := encodeCall(r, e)
		return runtime.Outcome{Res: res, Cancelled: e != nil && t.Ctx.Err() != nil}
	})
	if gerr == nil {
		gerr = forged
	}
	if gerr != nil {
		return llm.Response{}, nil, errorOf(gerr, runtime.KindReplay)
	}
	resp, err, derr := decodeCall(o.Res)
	if derr != nil {
		return llm.Response{}, nil, errorOf(t.Log().Diverge("at task %s: the log's result for %s does not decode: %s", t.ID, effectCall, derr), runtime.KindReplay)
	}
	return resp, err, nil
}

// liveProvider is the provider of m for a call that runs live. A resume
// can replay a model's llm_model from the log and then run its llm call
// live (the crashed run died during the call, or between two turns of an
// agent), and a replayed llm_model leaves m with no provider: it is then
// looked up from the host now, with the model's ID and price. The log's
// ID and price, which the call was reserved at and would send, must be
// the host's: the log's hash chain has no key, so a forged llm_model
// could otherwise send the call to any model of the granted provider at
// any price (review round 5). If they differ, the run diverges, and the
// error wraps errForgedModel.
func liveProvider(t *runtime.Task, m model) (llm.Provider, error) {
	if m.provider != nil {
		return m.provider, nil
	}
	host := llm.HostFrom(t.Ctx)
	if host == nil {
		return nil, errors.New("the host configured no model providers")
	}
	p, id, err := host.Lookup(m.spec)
	if err != nil {
		return nil, err
	}
	price, priced := host.Price(m.spec)
	if id != m.id || price != m.price || priced != m.priced {
		err := t.Log().Diverge("at task %s: the log's %s for %s has model %q at %s, and the host has %q at %s",
			t.ID, effectModel, m.spec, m.id, priceText(m.price, m.priced), id, priceText(price, priced))
		return nil, fmt.Errorf("%w: %w", errForgedModel, err)
	}
	return p, nil
}

// errForgedModel marks liveProvider's divergence.
var errForgedModel = errors.New("the log's model is not the host's")

// priceText is a price as a divergence error names it.
func priceText(p llm.Price, priced bool) string {
	if !priced {
		return "no price"
	}
	return fmt.Sprintf("%+v", p)
}
