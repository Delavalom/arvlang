package runtime

import (
	"strings"
	"testing"
)

// Scopes are capped before they are copied or normalised, and a host name
// at the DNS limit (253 bytes, without the wildcard or a trailing dot).
func TestScopeLengthCaps(t *testing.T) {
	long := strings.Repeat("a", MaxScopeLen)
	for _, tt := range []struct {
		kind  Kind
		scope string
		ok    bool
	}{
		{Env, long, true},
		{Env, long + "a", false},
		{FSRead, "/" + long[1:], true},
		{FSWrite, "/" + long, false},
		{Net, long[:253], true},
		{Net, long[:253] + ".", true},
		{Net, "*." + long[:253] + ":443", true},
		{Net, long[:254], false},
		{Net, "*." + long[:254], false},
		{Net, long + "a", false},
	} {
		_, err := Literal(tt.kind, tt.scope)
		if (err == nil) != tt.ok {
			t.Errorf("Literal(%s, %d bytes) = %v, want ok=%v", tt.kind, len(tt.scope), err, tt.ok)
		}
		if err != nil && len(err.Error()) > 100 {
			t.Errorf("Literal(%s, %d bytes): error is %d bytes long, want a short one", tt.kind, len(tt.scope), len(err.Error()))
		}
	}
}
