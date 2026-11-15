package arv

import (
	"bytes"
	"sync/atomic"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// Review round 4 (Critical): a host effect may return one of its
// arguments, or the same cached value twice. The codec writes a value met
// twice in one encoding as {"ref":n}, by identity, so a recording that
// went on with the host's own object encoded a later request (or the end)
// as [x, {"ref":1}], where the replay's decoded copy gave [x, x], and an
// honest log did not replay. A recording now goes on with the logged
// result decoded, as a replay does.
func TestHostEffectReturningItsArgumentReplays(t *testing.T) {
	var n atomic.Int64
	echo := func(t *runtime.Task, args ...value.Object) (value.Object, error) { return args[1], nil }
	effects := map[string]Effect{"k8s_echo": counted(&n, echo)}
	long := `"a manifest name longer than sixteen bytes"`
	for name, src := range map[string]string{
		"string request": "let s = " + long + ";\nlet r = k8s_echo(\"staging\", s);\nputs(r == s);\ngetenv([s, r])",
		"array request":  "let m = [" + long + "];\nlet r = k8s_echo(\"staging\", m);\nk8s_echo(\"staging\", [m, r]);\n\"end\"",
		"end line":       "let s = " + long + ";\nlet r = k8s_echo(\"staging\", s);\n[s, r]",
	} {
		t.Run(name, func(t *testing.T) {
			out, log, err := recordRun(t, src, Options{Caps: []runtime.Capability{staging}, Effects: effects})
			l, rerr := runtime.ReadLog(bytes.NewReader(log))
			if rerr != nil {
				t.Fatal(rerr)
			}
			var rout bytes.Buffer
			_, perr := Run(src, Options{Replay: l, Effects: effects, Out: &rout})
			if rout.String() != out || errText(perr) != errText(err) {
				t.Fatalf("replay of an honest log: %v, %q; recorded %v, %q", perr, rout.String(), err, out)
			}
		})
	}
}
