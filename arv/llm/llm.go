// Package llm is how Arv calls language models: a Provider interface the
// host implements or picks (Anthropic, or Fake in tests), the request and
// response shapes the evaluator's ask and agent effects build, and the
// prices that turn usage into the usd_cents budget. The host builds the
// providers, with their API keys; Arv code names models only through
// llm("provider:model") capabilities and never sees a key.
package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/delavalom/arvlang/arv/runtime"
)

// Provider sends one request to a model and returns its reply. It must
// return once ctx ends, and must never put a credential into an error.
type Provider interface {
	Complete(ctx context.Context, req Request) (Response, error)
}

// Request is one call: the conversation so far, the tools the model may
// ask for, and the most output tokens it may write.
type Request struct {
	Model     string // the provider's model ID, without the "provider:" prefix
	System    string // "" for none
	Messages  []Message
	Tools     []ToolDef
	MaxTokens int
}

// Roles of a message.
const (
	RoleUser      = "user"
	RoleAssistant = "assistant"
)

// Message is one turn of the conversation.
type Message struct {
	Role    string
	Content []Block
}

// Block types the evaluator reads or writes. A provider keeps any other
// block it returns (thinking) in Raw and sends it back unchanged.
const (
	BlockText       = "text"
	BlockToolUse    = "tool_use"
	BlockToolResult = "tool_result"
)

// Block is one piece of a message.
type Block struct {
	Type    string
	Text    string          // text; a tool_result's content
	ID      string          // tool_use: its ID; tool_result: the ID of the tool_use it answers
	Name    string          // tool_use: the tool's name
	Input   json.RawMessage // tool_use: the arguments, a JSON object
	IsError bool            // tool_result: the content is an error
	Raw     json.RawMessage // a block of another type, as the provider returned it
}

// ToolDef describes a tool to the model. In v1 every parameter is a
// required string.
type ToolDef struct {
	Name        string
	Description string
	Params      []string
}

// Stop reasons a Response may carry.
const (
	StopEndTurn   = "end_turn"
	StopSequence  = "stop_sequence"
	StopToolUse   = "tool_use"
	StopMaxTokens = "max_tokens"
	StopRefusal   = "refusal"
)

// Response is a model's reply and what it cost, in tokens.
type Response struct {
	Content      []Block
	StopReason   string
	InputTokens  int
	OutputTokens int
}

// Text is the reply's text blocks, joined.
func (r Response) Text() string {
	var b strings.Builder
	for _, c := range r.Content {
		if c.Type == BlockText {
			b.WriteString(c.Text)
		}
	}
	return b.String()
}

// Kinds of Error.
const (
	ErrAuth       = "auth"            // 401, 403: the key is missing, wrong or not allowed
	ErrBilling    = "billing"         // 402
	ErrNotFound   = "not_found"       // 404: often an unknown model
	ErrInvalid    = "invalid_request" // 400, 413 and other 4xx
	ErrRateLimit  = "rate_limit"      // 429
	ErrOverloaded = "overloaded"      // 529
	ErrServer     = "server"          // other 5xx
	ErrNetwork    = "network"         // no response
	ErrDecode     = "decode"          // a response that could not be read
)

// Error is a provider's failure. Its message never holds a credential: a
// provider builds it from the response's status and error message only.
// Status 0 with Kind ErrInvalid means the provider refused the request
// before sending it (see CostNothing).
type Error struct {
	Provider string
	Kind     string // one of the Err* constants
	Status   int    // the HTTP status, 0 if there was none
	Message  string // the provider's own message, clipped to maxErrorText bytes
}

// maxErrorText caps the provider message an Error keeps: an error value is
// not charged to alloc_mb.
const maxErrorText = 512

func (e *Error) Error() string {
	status := ""
	if e.Status != 0 {
		status = fmt.Sprintf(" (HTTP %d)", e.Status)
	}
	msg := e.Message
	if len(msg) > maxErrorText {
		msg = msg[:maxErrorText] + "..."
	}
	if msg != "" {
		msg = ": " + msg
	}
	return fmt.Sprintf("%s: %s%s%s", e.Provider, e.Kind, status, msg)
}

// CostNothing reports whether err shows that a call was never processed,
// so it cannot have been billed: the endpoint answered with a redirect
// (not followed), a 4xx or 529 (overloaded), or the provider refused the
// request before sending it (ErrInvalid with no status). Anything else
// may have been billed: a cancellation or a network error once the
// request may have gone out, a 200 whose body could not be read, another
// 5xx (a gateway's 502 or 504 can come after the API started work), an
// error that is not an *Error.
func CostNothing(err error) bool {
	var e *Error
	if !errors.As(err, &e) {
		return false
	}
	if e.Status == 0 {
		return e.Kind == ErrInvalid
	}
	return (e.Status >= 300 && e.Status < 500) || e.Status == 529
}

// ErrorKind makes a provider failure an Arv effect error: the world
// refused or failed, and restart may retry it.
func (e *Error) ErrorKind() string { return runtime.KindEffect }

// ParseModel splits "provider:model" into its parts. It refuses a wildcard:
// a call names exactly one model (a grant may name many).
func ParseModel(spec string) (provider, model string, err error) {
	c, err := runtime.Literal(runtime.LLM, spec)
	if err != nil {
		return "", "", err
	}
	provider, model, _ = strings.Cut(c.Scope, ":")
	if c.Scope == "*" || model == "*" {
		return "", "", fmt.Errorf("model %q: name one model, not a wildcard", spec)
	}
	return provider, model, nil
}
