package arv

import (
	"errors"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
)

// The host sees what caused an error that ended the run.
func TestRunErrorKind(t *testing.T) {
	for _, tt := range []struct{ src, kind string }{
		{`1 / 0`, runtime.KindRuntime},
		{`scope (timeout_ms: 10) { sleep(5000) }`, runtime.KindBudget},
		{`let f = fn() { 1 }; spawn f() with [env("ARV_KIND_X")]`, runtime.KindCapability},
	} {
		_, err := Run(tt.src, Options{})
		var ae *Error
		if !errors.As(err, &ae) || ae.Kind != tt.kind {
			t.Errorf("%s: got %#v, want an *Error of kind %q", tt.src, err, tt.kind)
		}
	}
}
