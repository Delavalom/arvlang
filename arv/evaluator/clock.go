package evaluator

import (
	"time"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// clockStart is the zero of clock_ms. time.Since reads the monotonic
// clock, so a change to the wall clock never moves clock_ms.
var clockStart = time.Now()

func init() {
	builtins["clock_ms"] = &value.Builtin{Fn: clockMS}
}

// clock_ms() is the milliseconds since the process started, on a
// monotonic clock: only the difference of two readings means anything,
// as a latency. Like sleep it needs no capability. It is an effect (I7):
// Phase 6 must record it.
func clockMS(t *runtime.Task, args ...value.Object) value.Object {
	if len(args) != 0 {
		return newError("clock_ms: want 0 arguments, got %d", len(args))
	}
	return &value.Integer{Value: time.Since(clockStart).Milliseconds()}
}
