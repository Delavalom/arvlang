package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ARV_ANTHROPIC_URL is the tests' seam for a local fake. In a release
// binary a stray or inherited value must not send the key in clear text to
// another host: it is honoured only for https, or plain http to loopback.
func TestAnthropicURLOnlyHTTPSOrLoopback(t *testing.T) {
	for raw, ok := range map[string]bool{
		"":                                      true, // the real API
		"https://api.anthropic.com/v1/messages": true,
		"https://proxy.example/v1/messages":     true,
		"http://127.0.0.1:8080/v1/messages":     true,
		"http://127.9.9.9/v1/messages":          true,
		"http://[::1]:9/v1/messages":            true,
		"http://localhost:9/v1/messages":        true,
		"http://LOCALHOST:9/v1/messages":        true,
		"http://example.com/v1/messages":        false,
		"http://10.0.0.1/v1/messages":           false,
		"http://[::ffff:8.8.8.8]/v1/messages":   false,
		"http://localhost.example.com/":         false,
		"http://127.0.0.1.example.com/":         false,
		"ftp://127.0.0.1/":                      false,
		"//127.0.0.1/":                          false,
		"127.0.0.1:9":                           false,
		"https:///v1/messages":                  false,
		"http://user@example.com/":              false,
	} {
		got, err := anthropicURL(raw)
		if ok && (err != nil || got != raw) {
			t.Errorf("anthropicURL(%q) = %q, %v; want it accepted", raw, got, err)
		}
		if !ok && err == nil {
			t.Errorf("anthropicURL(%q) = %q, want an error", raw, got)
		}
		if err != nil && !strings.Contains(err.Error(), "ARV_ANTHROPIC_URL") {
			t.Errorf("anthropicURL(%q): error %q does not name the variable", raw, err)
		}
	}
}

// Through the CLI: with the key set, a plain-http URL to a host that is
// not loopback stops the run before any call, naming the variable.
func TestCLIRefusesPlainHTTPAnthropicURL(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", cliKey)
	t.Setenv("ARV_ANTHROPIC_URL", "http://api.example.invalid/v1/messages")
	file := filepath.Join(t.TempDir(), "x.arv")
	if err := os.WriteFile(file, []byte(`puts(ask("anthropic:claude-haiku-4-5", "hi"))`), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	code := runCLI([]string{"run", "--allow", "llm=anthropic:claude-haiku-4-5", file}, &out, &errb)
	if code == 0 || !strings.HasPrefix(errb.String(), "error: ARV_ANTHROPIC_URL") || out.Len() != 0 {
		t.Fatalf("code %d, stdout %q, stderr %q; want the run refused naming ARV_ANTHROPIC_URL", code, out.String(), errb.String())
	}
	noKey(t, out.String(), errb.String())
}
