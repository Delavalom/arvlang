package runtime

import (
	"strconv"
	"time"
)

// elapsed is the run's milliseconds so far. A resumed run starts as far
// back as the crashed run had run (its log's largest At), so the time
// the crashed run spent counts and the log's times keep increasing.
func (l *EffectLog) elapsed() int64 { return time.Since(l.start).Milliseconds() }

// Elapsed is the run's time so far, a resumed run's counted from when the
// crashed run started (see elapsed).
func (l *EffectLog) Elapsed() time.Duration { return time.Since(l.start) }

// ClockOffset is, for a resumed run whose log holds readings of the
// effect kind (clock_ms), the least offset that keeps every live reading
// at or after each logged one: a reading v logged at At is at most
// v - At ahead of the run's time. ok is false otherwise.
// It is worked out once per run, for the first kind asked.
func (l *EffectLog) ClockOffset(kind string) (offset int64, ok bool) {
	if l.mode != Resume {
		return 0, false
	}
	l.clockOnce.Do(func() { l.clock, l.clockOK = l.clockOffset(kind) })
	return l.clock, l.clockOK
}

func (l *EffectLog) clockOffset(kind string) (offset int64, ok bool) {
	for _, recs := range l.log.tasks {
		for _, r := range recs {
			v, err := strconv.ParseInt(string(r.Res), 10, 64)
			if r.Kind != kind || r.Check != 0 || err != nil {
				continue
			}
			if !ok || v-r.At > offset {
				offset, ok = v-r.At, true
			}
		}
	}
	return offset, ok
}
