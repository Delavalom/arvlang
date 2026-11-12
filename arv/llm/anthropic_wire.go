package llm

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
)

var errNoContent = errors.New("reply has no content array")

// The Messages API's JSON, as much of it as Arv sends and reads.
type (
	apiRequest struct {
		Model     string       `json:"model"`
		MaxTokens int          `json:"max_tokens"`
		System    string       `json:"system,omitempty"`
		Messages  []apiMessage `json:"messages"`
		Tools     []apiTool    `json:"tools,omitempty"`
	}
	apiMessage struct {
		Role    string            `json:"role"`
		Content []json.RawMessage `json:"content"`
	}
	apiTool struct {
		Name        string    `json:"name"`
		Description string    `json:"description,omitempty"`
		InputSchema apiSchema `json:"input_schema"`
	}
	apiSchema struct {
		Type                 string                       `json:"type"`
		Properties           map[string]map[string]string `json:"properties"`
		Required             []string                     `json:"required"`
		AdditionalProperties bool                         `json:"additionalProperties"`
	}
	apiBlock struct {
		Type      string          `json:"type"`
		Text      *string         `json:"text,omitempty"`
		ID        string          `json:"id,omitempty"`
		Name      string          `json:"name,omitempty"`
		Input     json.RawMessage `json:"input,omitempty"`
		ToolUseID string          `json:"tool_use_id,omitempty"`
		Content   *string         `json:"content,omitempty"`
		IsError   bool            `json:"is_error,omitempty"`
	}
	apiResponse struct {
		Content    []json.RawMessage `json:"content"`
		StopReason string            `json:"stop_reason"`
		Usage      *apiUsage         `json:"usage"`
	}
	apiUsage struct {
		InputTokens              int `json:"input_tokens"`
		OutputTokens             int `json:"output_tokens"`
		CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
		CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	}
)

// encodeRequest is req as a Messages API body. A block that came from a
// reply goes back as it came (Raw); one Arv built is encoded from its
// fields. Every parameter is a required string, and no other is allowed.
func encodeRequest(req Request) ([]byte, error) {
	out := apiRequest{Model: req.Model, MaxTokens: req.MaxTokens, System: req.System}
	for _, m := range req.Messages {
		am := apiMessage{Role: m.Role, Content: make([]json.RawMessage, 0, len(m.Content))}
		for _, b := range m.Content {
			raw, err := encodeBlock(b)
			if err != nil {
				return nil, err
			}
			am.Content = append(am.Content, raw)
		}
		out.Messages = append(out.Messages, am)
	}
	for _, t := range req.Tools {
		s := apiSchema{Type: "object", Properties: map[string]map[string]string{}, Required: append([]string{}, t.Params...)}
		for _, p := range t.Params {
			s.Properties[p] = map[string]string{"type": "string"}
		}
		out.Tools = append(out.Tools, apiTool{Name: t.Name, Description: t.Description, InputSchema: s})
	}
	return json.Marshal(out)
}

func encodeBlock(b Block) (json.RawMessage, error) {
	if len(b.Raw) > 0 {
		return b.Raw, nil
	}
	switch b.Type {
	case BlockText:
		return json.Marshal(apiBlock{Type: BlockText, Text: &b.Text})
	case BlockToolUse:
		input := b.Input
		if len(input) == 0 {
			input = json.RawMessage("{}")
		}
		return json.Marshal(apiBlock{Type: BlockToolUse, ID: b.ID, Name: b.Name, Input: input})
	case BlockToolResult:
		return json.Marshal(apiBlock{Type: BlockToolResult, ToolUseID: b.ID, Content: &b.Text, IsError: b.IsError})
	}
	return nil, fmt.Errorf("cannot send a %q block", b.Type)
}

// decodeResponse reads a 200 reply. Every block keeps its JSON in Raw, to
// go back unchanged; text and tool_use blocks also fill their fields.
// Cache tokens count as input. Negative usage is an error, and a sum too
// large for an int is the largest int. A reply without usage, or with no
// input tokens (the API counts at least one for any prompt), is an error
// (review round 2): settling it to zero would make a call free to the
// budget that the endpoint may still bill.
func decodeResponse(data []byte) (Response, error) {
	var r apiResponse
	if err := json.Unmarshal(data, &r); err != nil {
		return Response{}, err
	}
	if r.Content == nil {
		return Response{}, errNoContent
	}
	if r.Usage == nil {
		return Response{}, errNoUsage
	}
	u := *r.Usage
	if u.InputTokens < 0 || u.OutputTokens < 0 || u.CacheCreationInputTokens < 0 || u.CacheReadInputTokens < 0 {
		return Response{}, errNegativeUsage
	}
	in := addSatInt(addSatInt(u.InputTokens, u.CacheCreationInputTokens), u.CacheReadInputTokens)
	if in == 0 {
		return Response{}, errNoInputUsage
	}
	out := Response{
		StopReason:   r.StopReason,
		InputTokens:  in,
		OutputTokens: u.OutputTokens,
	}
	for _, raw := range r.Content {
		var b apiBlock
		if err := json.Unmarshal(raw, &b); err != nil {
			return Response{}, err
		}
		blk := Block{Type: b.Type, Raw: raw}
		switch b.Type {
		case BlockText:
			if b.Text != nil {
				blk.Text = *b.Text
			}
		case BlockToolUse:
			blk.ID, blk.Name, blk.Input = b.ID, b.Name, b.Input
		}
		out.Content = append(out.Content, blk)
	}
	return out, nil
}

var (
	errNegativeUsage = errors.New("reply reports negative usage")
	errNoUsage       = errors.New("reply has no usage")
	errNoInputUsage  = errors.New("reply's usage reports no input tokens")
)

// addSatInt is a+b for non-negative a and b, or math.MaxInt if that
// overflows.
func addSatInt(a, b int) int {
	if a > math.MaxInt-b {
		return math.MaxInt
	}
	return a + b
}
