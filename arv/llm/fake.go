package llm

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
)

// Fake is a scripted Provider for tests: it answers each call with the
// next Reply in its queue, then with Then (if set), and records every
// request it receives. It makes no network call and starts no goroutine.
type Fake struct {
	// Then answers once the queue is empty; nil makes that an error.
	Then func(ctx context.Context, req Request) (Response, error)

	mu      sync.Mutex
	replies []Reply
	calls   []Request
}

// Reply is one scripted answer: a response, or an error.
type Reply struct {
	Response Response
	Err      error
}

// NewFake returns a Fake that answers with replies, in order.
func NewFake(replies ...Reply) *Fake {
	return &Fake{replies: append([]Reply(nil), replies...)}
}

// ErrNoReply is the Fake's error when nothing is scripted for a call.
var ErrNoReply = errors.New("fake: no reply scripted")

// Complete records req and returns the next reply. Once ctx has ended it
// returns ctx's cause, still recording the call.
func (f *Fake) Complete(ctx context.Context, req Request) (Response, error) {
	f.mu.Lock()
	f.calls = append(f.calls, cloneRequest(req))
	var next *Reply
	if len(f.replies) > 0 {
		next = &f.replies[0]
		f.replies = f.replies[1:]
	}
	then := f.Then
	f.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return Response{}, context.Cause(ctx)
	}
	switch {
	case next != nil:
		return next.Response, next.Err
	case then != nil:
		return then(ctx, req)
	}
	return Response{}, ErrNoReply
}

// Calls returns a copy of every request received so far, in order.
func (f *Fake) Calls() []Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Request, len(f.calls))
	for i, r := range f.calls {
		out[i] = cloneRequest(r)
	}
	return out
}

// TextReply is a final answer: text, stop reason end_turn, and the usage.
func TextReply(text string, in, out int) Reply {
	return Reply{Response: Response{
		Content:    []Block{{Type: BlockText, Text: text}},
		StopReason: StopEndTurn, InputTokens: in, OutputTokens: out,
	}}
}

// ToolUseReply asks for tools: stop reason tool_use, the given tool_use
// blocks (see ToolUse), and the usage.
func ToolUseReply(in, out int, uses ...Block) Reply {
	return Reply{Response: Response{
		Content: append([]Block(nil), uses...), StopReason: StopToolUse, InputTokens: in, OutputTokens: out,
	}}
}

// ToolUse is a tool_use block with the given ID, tool name and JSON input.
func ToolUse(id, name, input string) Block {
	return Block{Type: BlockToolUse, ID: id, Name: name, Input: json.RawMessage(input)}
}

// cloneRequest copies req deeply, so a caller that keeps appending to its
// conversation cannot change what was recorded.
func cloneRequest(req Request) Request {
	c := req
	c.Tools = append([]ToolDef(nil), req.Tools...)
	for i := range c.Tools {
		c.Tools[i].Params = append([]string(nil), req.Tools[i].Params...)
	}
	c.Messages = make([]Message, len(req.Messages))
	for i, m := range req.Messages {
		blocks := make([]Block, len(m.Content))
		for j, b := range m.Content {
			b.Input = append(json.RawMessage(nil), b.Input...)
			b.Raw = append(json.RawMessage(nil), b.Raw...)
			blocks[j] = b
		}
		c.Messages[i] = Message{Role: m.Role, Content: blocks}
	}
	return c
}
