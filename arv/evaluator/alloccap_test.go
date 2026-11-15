package evaluator

import (
	"fmt"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// A capability value keeps its scope alive, and net() builds a new string
// for it (lower case, port normalised), so it costs capabilitySize plus
// the scope's bytes, like a string. Capabilities used to cost nothing: a
// loop of net(s) over a 1 MB host kept 287 MiB alive under alloc_mb: 1,
// and the scope succeeded.
func TestCapabilityValuesAreCharged(t *testing.T) {
	for _, tt := range []struct {
		input, scope string
	}{
		{`net("Example.COM:0443")`, "example.com:443"},
		{`fs_read("/tmp/a/b")`, "/tmp/a/b"},
		{`fs_write("/tmp/a/b")`, "/tmp/a/b"},
		{`env("HOME")`, "HOME"},
	} {
		cost := capabilitySize + int64(len(tt.scope))
		if got := runCapsIn(t, cost, runtime.NewCapSet(), tt.input); got.Type() != value.CAPABILITY_VAL {
			t.Errorf("%s under %d bytes: got %s, want a capability", tt.input, cost, got.Inspect())
		}
		got := runCapsIn(t, cost-1, runtime.NewCapSet(), tt.input)
		want := fmt.Sprintf("budget exceeded: alloc (limit %d bytes)", cost-1)
		if e, ok := got.(*value.Error); !ok || e.Message != want {
			t.Errorf("%s under %d bytes: got %s, want %q", tt.input, cost-1, got.Inspect(), want)
		}
	}
}

// A scope longer than runtime.MaxScopeLen (or a host name longer than 253
// bytes) is refused before it is copied, so a free literal of any size
// cannot make the runtime hold or quote it.
func TestCapabilityScopeLengthIsCapped(t *testing.T) {
	long := strings.Repeat("a", 1000000)
	for _, tt := range []struct{ input, msg string }{
		{`net("` + long + `")`, "net: scope is 1000000 bytes long (max 4096)"},
		{`net("` + long[:254] + `")`, "net: host name is 254 bytes long (max 253)"},
		{`fs_read("/` + long + `")`, "fs_read: scope is 1000001 bytes long (max 4096)"},
		{`fs_write("/` + long + `")`, "fs_write: scope is 1000001 bytes long (max 4096)"},
		{`env("` + long + `")`, "env: scope is 1000000 bytes long (max 4096)"},
	} {
		got, _ := runWith(t, tt.input)
		if e, ok := got.(*value.Error); !ok || e.Message != tt.msg {
			t.Errorf("%.20s...: got %.200s, want %q", tt.input, got.Inspect(), tt.msg)
		}
	}
	if got, _ := runWith(t, `net("*.`+long[:253]+`:443")`); got.Type() != value.CAPABILITY_VAL {
		t.Errorf("a 253-byte host name under a wildcard: got %.200s, want a capability", got.Inspect())
	}
}

// The reviewer's case: net(s) in a loop over a 1 MB host. It now fails at
// once, without copying the host.
func TestNetLoopOverHugeHostAllocatesLittle(t *testing.T) {
	src := `let s = "` + strings.Repeat("A", 1000000) + `"; scope (alloc_mb: 1) { for i in ` + zeros(300) + ` { net(s) } }`
	var got value.Object
	used := heapDuring(func() { got, _ = runWith(t, src) })
	wantErrorHas(t, got, "net: scope is 1000000 bytes long")
	if used > 16<<20 {
		t.Errorf("300 net(s) calls over a 1 MB host allocated %d MiB, want under 16 MiB", used>>20)
	}
}
