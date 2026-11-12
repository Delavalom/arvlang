package arv

import (
	"context"
	"fmt"
	"strings"

	"github.com/delavalom/arvlang/arv/llm"
)

// agentScript is a fake model for agent runs whose reply depends only on
// the request, never on the order in which calls arrive: plan[goal][k] is
// the tool calls of turn k+1, each "name:arg" (every tool takes one
// parameter, arg). The turn after the last is a final answer that quotes
// every tool result in the request, in order. A resumed run's live calls
// therefore get the replies an uninterrupted run got, and the output
// shows every tool result.
type agentScript map[string][][]string

func (s agentScript) reply(ctx context.Context, req llm.Request) (llm.Response, error) {
	goal := req.Messages[0].Content[0].Text
	turn := 0
	for _, m := range req.Messages {
		if m.Role == llm.RoleAssistant {
			turn++
		}
	}
	if turn < len(s[goal]) {
		var uses []llm.Block
		for i, call := range s[goal][turn] {
			name, arg, _ := strings.Cut(call, ":")
			uses = append(uses, llm.ToolUse(fmt.Sprintf("u%d_%d", turn, i), name, fmt.Sprintf(`{"arg": %q}`, arg)))
		}
		return llm.ToolUseReply(100, 10, uses...).Response, nil
	}
	var parts []string
	for _, m := range req.Messages {
		for _, b := range m.Content {
			if b.Type == llm.BlockToolResult {
				tag := "ok"
				if b.IsError {
					tag = "error"
				}
				parts = append(parts, tag+": "+b.Text)
			}
		}
	}
	return llm.TextReply(goal+" => "+strings.Join(parts, " | "), 100, 10).Response, nil
}

// fake is a provider that answers by the script.
func (s agentScript) fake() *llm.Fake { return &llm.Fake{Then: s.reply} }
