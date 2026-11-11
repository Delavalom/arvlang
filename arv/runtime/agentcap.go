package runtime

import (
	"fmt"
	"strings"
)

// MaxToolName is the longest tool name a tool grant may name: the
// Anthropic Messages API's limit for a tool's name.
const MaxToolName = 64

// maxModelLen caps the model part of an llm scope. Model IDs are short
// (claude-sonnet-5-5); this only keeps an error from quoting a long one.
const maxModelLen = 128

// agentScope validates the scope of an llm or tool capability. Both are
// pure: a model or tool name is not looked up anywhere.
//
//   - llm: "*" (any model of any provider), "provider:*" (any model of one
//     provider) or "provider:model". A provider is a lowercase name
//     ([a-z][a-z0-9_-]*, at most 32 bytes); a model is [A-Za-z0-9._@-]+,
//     at most 128 bytes, starting with a letter or digit.
//   - tool: "*" (any tool) or a name of [A-Za-z0-9_-]+, at most
//     MaxToolName bytes, as the Messages API requires of tool names.
func agentScope(kind Kind, scope string) error {
	if scope == "*" {
		return nil
	}
	if kind == Tool {
		if !ToolName(scope) {
			return fmt.Errorf("tool name %q: want 1 to %d of A-Z, a-z, 0-9, _ and -, or *", clip(scope), MaxToolName)
		}
		return nil
	}
	provider, model, ok := strings.Cut(scope, ":")
	if !ok {
		return fmt.Errorf("%q: want provider:model, provider:* or *", clip(scope))
	}
	if !providerName(provider) {
		return fmt.Errorf("provider %q: want a lowercase name of a-z, 0-9, _ and -", clip(provider))
	}
	if model != "*" && !modelName(model) {
		return fmt.Errorf("model %q: want a name of A-Z, a-z, 0-9, ., _, @ and -, or *", clip(model))
	}
	return nil
}

// ToolName reports whether s is a valid tool name (not "*").
func ToolName(s string) bool {
	if s == "" || len(s) > MaxToolName {
		return false
	}
	for i := 0; i < len(s); i++ {
		if c := s[i]; !alnum(c) && c != '_' && c != '-' {
			return false
		}
	}
	return true
}

func providerName(s string) bool {
	if s == "" || len(s) > 32 || s[0] < 'a' || s[0] > 'z' {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'a' && c <= 'z') && !(c >= '0' && c <= '9') && c != '_' && c != '-' {
			return false
		}
	}
	return true
}

func modelName(s string) bool {
	if s == "" || len(s) > maxModelLen || !alnum(s[0]) {
		return false
	}
	for i := 0; i < len(s); i++ {
		if c := s[i]; !alnum(c) && !strings.ContainsRune("._@-", rune(c)) {
			return false
		}
	}
	return true
}

func alnum(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

// clip shortens s for an error message: a scope can be MaxScopeLen bytes.
func clip(s string) string {
	if len(s) > 64 {
		return s[:64] + "..."
	}
	return s
}

// agentCovers reports whether llm or tool grant g covers request r, both
// valid: "*" covers everything, "provider:*" every model of that provider,
// and anything else only itself. A request is compared as written: a
// wildcard request is covered only by a grant at least as wide.
func agentCovers(g, r string) bool {
	if g == "*" || g == r {
		return true
	}
	if p, ok := strings.CutSuffix(g, ":*"); ok {
		rp, _, found := strings.Cut(r, ":")
		return found && rp == p
	}
	return false
}
