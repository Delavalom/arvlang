package check

import (
	"testing"

	"github.com/delavalom/arvlang/arv/evaluator"
	"github.com/delavalom/arvlang/arv/runtime"
)

// Every builtin is either an effect (or constructor) with a rule here, or
// pure. A builtin added by a later phase fails this test until arv check
// knows what it needs: an unclassified one would be a hole in the report.
func TestEveryBuiltinIsClassified(t *testing.T) {
	names := map[string]bool{}
	for _, name := range evaluator.BuiltinNames() {
		names[name] = true
		_, isEffect := effects[name]
		if isEffect == pure[name] {
			t.Errorf("builtin %q: add it to exactly one of effects and pure in arv/check/builtins.go", name)
		}
	}
	for name := range effects {
		if !names[name] {
			t.Errorf("effects names %q, which is not a builtin", name)
		}
	}
	for name := range pure {
		if !names[name] {
			t.Errorf("pure names %q, which is not a builtin", name)
		}
	}
}

func TestURLScope(t *testing.T) {
	for _, tt := range []struct{ url, want, err string }{
		{"https://api.github.com/zen", "api.github.com:443", ""},
		{"http://API.GitHub.com./x", "api.github.com:80", ""},
		{"http://127.0.0.1:8080/ok", "127.0.0.1:8080", ""},
		{"http://[::1]:9/", "[::1]:9", ""},
		{"ftp://example.com/", "", "invalid url"},
		{"http:///path", "", "URL has no host"},
		{"http://a b/", "", "invalid url"},
	} {
		got, err := urlScope("", tt.url)
		if tt.err != "" {
			if err == nil || err.Error() != tt.err {
				t.Errorf("urlScope(%q) = %q, %v; want error %q", tt.url, got, err, tt.err)
			}
			continue
		}
		if err != nil || got != tt.want {
			t.Errorf("urlScope(%q) = %q, %v; want %q", tt.url, got, err, tt.want)
		}
	}
}

func TestLiteralScopeKeepsPathsAsWritten(t *testing.T) {
	for _, tt := range []struct {
		kind      runtime.Kind
		arg, want string
		bad       bool
	}{
		{"fs_read", "targets.txt", "targets.txt", false},
		{"fs_write", "out/../x", "out/../x", false},
		{"fs_read", "/etc/hosts", "/etc/hosts", false},
		{"fs_read", "", "", true},
		{"net", "API.github.com", "api.github.com", false},
		{"net", "a b", "", true},
		{"env", "HOME", "HOME", false},
		{"env", "", "", true},
	} {
		got, err := literalScope(tt.kind, tt.arg)
		if (err != nil) != tt.bad || got != tt.want {
			t.Errorf("literalScope(%s, %q) = %q, %v", tt.kind, tt.arg, got, err)
		}
	}
}
