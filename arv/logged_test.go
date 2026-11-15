package arv

import (
	"testing"
	"time"
)

// The header keeps whole milliseconds: a timeout rounds up, so a replay
// never times out sooner than the recording.
func TestCeilMS(t *testing.T) {
	for d, want := range map[time.Duration]int64{0: 0, time.Millisecond: 1, 1500 * time.Microsecond: 2, time.Nanosecond: 1} {
		if got := ceilMS(d); got != want {
			t.Errorf("ceilMS(%s) = %d, want %d", d, got, want)
		}
	}
}
