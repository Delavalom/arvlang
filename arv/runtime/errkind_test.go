package runtime

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestErrorKind(t *testing.T) {
	denied := &DeniedError{Want: Capability{Kind: Env, Scope: "X"}}
	exceeded := &ExceededError{Kind: "steps", Limit: 10}
	for _, tt := range []struct {
		name string
		err  error
		want string
	}{
		{"denied", denied, KindCapability},
		{"wrapped denied", fmt.Errorf("spawn: %w", denied), KindCapability},
		{"exceeded", exceeded, KindBudget},
		{"wrapped exceeded", fmt.Errorf("read: %w", exceeded), KindBudget},
		{"canceled", context.Canceled, KindCancelled},
		{"deadline", fmt.Errorf("get: %w", context.DeadlineExceeded), KindCancelled},
		{"plain", errors.New("connection refused"), KindEffect},
	} {
		if got := ErrorKind(tt.err, KindEffect); got != tt.want {
			t.Errorf("%s: ErrorKind = %q, want %q", tt.name, got, tt.want)
		}
	}
	if got := ErrorKind(errors.New("x"), KindRuntime); got != KindRuntime {
		t.Errorf("fallback: got %q, want %q", got, KindRuntime)
	}
}
