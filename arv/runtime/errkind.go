package runtime

import (
	"context"
	"errors"
)

// Error kinds: what an Arv error value's "kind" says about its cause.
// match shows it to the program, and a restart supervisor retries only the
// kinds its policy names.
const (
	KindCapability = "capability" // a grant was missing (*DeniedError)
	KindBudget     = "budget"     // a budget ran out (*ExceededError)
	KindCancelled  = "cancelled"  // the task's scope was cancelled
	KindEffect     = "effect"     // an effect failed in the world: refused, not found, ...
	KindRuntime    = "runtime"    // the program is wrong: types, arguments, overflow
	KindUser       = "user"       // the program's own error(msg)
)

// Kinded is an error that knows its kind.
type Kinded interface {
	ErrorKind() string
}

func (*DeniedError) ErrorKind() string   { return KindCapability }
func (*ExceededError) ErrorKind() string { return KindBudget }

// ErrorKind is the kind of the first error in err's chain that knows its
// kind (Kinded); else KindCancelled for a context's cancellation or
// deadline; else fallback.
func ErrorKind(err error, fallback string) string {
	var k Kinded
	if errors.As(err, &k) {
		return k.ErrorKind()
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return KindCancelled
	}
	return fallback
}
