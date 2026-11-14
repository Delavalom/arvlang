package runtime

import (
	"testing"
	"time"
)

// Once the doubled backoff reaches MaxBackoff, Delay clamps it to the cap
// before adding the jitter. The clamp is not redundant with the final
// min: a backoff below -2^62 (a Go host can set one; Arv's backoff_ms
// must be positive) wraps on its first doubling to a positive value near
// the top of the int64 range, and adding a tenth of that overflows to a
// negative wait. A retry must not wait a negative time or longer than the
// cap; since the final fixer round a negative backoff counts as 0, and
// this test stays as a guard on that path.
func TestDelayClampsBeforeJitter(t *testing.T) {
	for _, b := range []time.Duration{-(1 << 62) - 1, -(1 << 62) - 12345, -(1 << 62) - 1<<40} {
		p := Policy{Strategy: Restart, Backoff: b}
		for _, n := range []int{2, 3, 4, 10, 70} {
			for _, id := range []string{"root/0", "root/1", "x"} {
				if d := p.Delay(id, n); d < 0 || d > MaxBackoff {
					t.Errorf("Backoff %d: Delay(%q, %d) = %d, want within [0, %s]", int64(b), id, n, int64(d), MaxBackoff)
				}
			}
		}
	}
}

// Every backoff, negative or not, waits between 0 and MaxBackoff. Until
// the final fixer round Policy{Backoff: -100}.Delay("root/0", 1) was
// -1862845608111266454: d/10+1 was negative, so uint64(d/10+1) was huge
// and the jitter was the raw hash. Arv's backoff_ms must be positive; a Go
// host can set any Backoff, and a negative one now means no wait.
func TestDelayNegativeBackoff(t *testing.T) {
	for _, b := range []time.Duration{-1, -100, -time.Second, -(1 << 63) + 1} {
		for _, n := range []int{1, 2, 3, 70} {
			if d := (Policy{Strategy: Restart, Backoff: b}).Delay("root/0", n); d < 0 || d > MaxBackoff {
				t.Errorf("Backoff %d: Delay(attempt %d) = %d, want within [0, %s]", int64(b), n, int64(d), MaxBackoff)
			}
		}
	}
}
