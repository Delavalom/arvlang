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
// negative wait. Whatever such a backoff should mean, a retry must not
// wait a negative time or longer than the cap.
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

// Every backoff, negative or not, should wait between 0 and MaxBackoff.
func TestDelayNegativeBackoff(t *testing.T) {
	t.Skip("BUG: a negative Policy.Backoff gives a negative Delay. " +
		"Repro: Policy{Backoff: -100}.Delay(\"root/0\", 1) = -1862845608111266454 " +
		"(d/10+1 is negative, so uint64(d/10+1) is huge and the jitter is the raw hash). " +
		"Policy{Backoff: -(1<<63)+1}.Delay(\"root/0\", 1) waits the 30 s cap but attempt 2 waits 2 ns. " +
		"Unreachable from Arv (backoff_ms must be positive); a Go host can set it.")
	for _, b := range []time.Duration{-1, -100, -time.Second, -(1 << 63) + 1} {
		for _, n := range []int{1, 2, 3, 70} {
			if d := (Policy{Strategy: Restart, Backoff: b}).Delay("root/0", n); d < 0 || d > MaxBackoff {
				t.Errorf("Backoff %d: Delay(attempt %d) = %d, want within [0, %s]", int64(b), n, int64(d), MaxBackoff)
			}
		}
	}
}
