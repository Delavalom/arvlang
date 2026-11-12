package evaluator

import (
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
)

// fit is the smallest limit, made by limit, under which src runs without
// an error: the exact steps or bytes src is charged, since the root scope
// charges its last partial chunk when it closes. A program that allocates
// nothing fits 1 byte (0 means no limit).
func fit(t *testing.T, src string, limit func(n int64) runtime.Limits) int64 {
	t.Helper()
	lo, hi := int64(1), int64(1<<24)
	if got := runLimited(t, src, limit(hi)); isError(got) {
		t.Fatalf("%s fails under %d: %s", src, hi, got.Inspect())
	}
	for lo < hi {
		mid := (lo + hi) / 2
		if isError(runLimited(t, src, limit(mid))) {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	return lo
}

func stepsLimit(n int64) runtime.Limits { return runtime.Limits{Steps: n} }
func bytesLimit(n int64) runtime.Limits { return runtime.Limits{AllocBytes: n} }

// sameCost fails unless a and b are charged the same steps and the same
// bytes. Each pair says one form is exactly the other.
func sameCost(t *testing.T, a, b string) {
	t.Helper()
	if sa, sb := fit(t, a, stepsLimit), fit(t, b, stepsLimit); sa != sb {
		t.Errorf("steps: %s takes %d, %s takes %d", a, sa, b, sb)
	}
	if ba, bb := fit(t, a, bytesLimit), fit(t, b, bytesLimit); ba != bb {
		t.Errorf("bytes: %s takes %d, %s takes %d", a, ba, b, bb)
	}
}
