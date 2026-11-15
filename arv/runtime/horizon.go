package runtime

import (
	"math"
	"sort"
	"sync/atomic"
)

// clock orders bindings, environments and spawns, for I8: a spawned task
// never sees a name bound after it was spawned. It moves only at a spawn
// (Task.Child), so a binding made since the last spawn may be overwritten
// in place. It is shared by every run in the process; only its order
// matters, not its values.
var clock atomic.Uint64

// Now is the clock's current time: what an environment records when it is
// created and when it binds a name.
func Now() uint64 { return clock.Load() }

// Horizon is the time that bounds what t sees of an environment created
// at born: t sees a binding made there only if it was made before the
// returned time. That is the spawn time of the first task, from the
// root down to t, spawned after the environment was created: from then
// on, the environment was frozen for that task and for everything it
// spawns, however late (I8, transitively). An environment created after
// t was spawned (by t, or by a task t awaited) has no horizon for t:
// math.MaxUint64.
func (t *Task) Horizon(born uint64) uint64 {
	i := sort.Search(len(t.horizon), func(i int) bool { return t.horizon[i] > born })
	if i == len(t.horizon) {
		return math.MaxUint64
	}
	return t.horizon[i]
}

// HorizonLen is how many spawn times t keeps: one per task from the
// root's first child down to t. A child keeps one more.
func (t *Task) HorizonLen() int { return len(t.horizon) }
