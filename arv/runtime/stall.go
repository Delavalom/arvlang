package runtime

import (
	"sync"
	"sync/atomic"
	"time"
)

// A replay runs nothing live, so every wait in it is for another task of
// the run: for a record to be used (waitFront), a cancellation or a first
// failure the log says happened (awaitCancel, awaitFailure), a task to
// end (WaitFor, a scope's Wait) or a slot (Acquire). If every task is in
// such a wait, nothing can end any of them: the log does not fit the run
// (a forged log can say a task was cancelled where nothing cancels it).
// Without a host deadline that would hang the replay, and through the CLI
// the Go runtime would abort with "all goroutines are asleep". So a
// replay counts its running tasks; once none runs, and that lasts
// stallRounds checks stallEvery apart with no task waking, the replay
// diverges with a clean error. A task woken by another is counted as
// running only once it runs, which is why it must last: by then the Go
// scheduler has run it.
const (
	stallEvery  = 20 * time.Millisecond
	stallRounds = 50
)

// stall is the replay's count of running tasks.
type stall struct {
	active   atomic.Int64  // tasks running, not parked in a replay's wait
	epoch    atomic.Uint64 // bumped each time a task parks or wakes
	watching atomic.Bool   // a watcher is running
	wg       sync.WaitGroup
}

// Enter counts a task about to start (a spawn's goroutine) as running.
func (l *EffectLog) Enter() {
	if l != nil && l.mode == Replay {
		l.st.active.Add(1)
		l.st.epoch.Add(1)
	}
}

// Exit stops counting a task that has ended.
func (l *EffectLog) Exit() { l.park() }

// Park runs wait, a replay's wait for another task, with the task counted
// as not running.
func (l *EffectLog) Park(wait func()) {
	l.park()
	defer l.unpark()
	wait()
}

func (l *EffectLog) park() {
	if l == nil || l.mode != Replay {
		return
	}
	l.st.epoch.Add(1)
	if l.st.active.Add(-1) <= 0 {
		l.watch()
	}
}

func (l *EffectLog) unpark() {
	if l != nil && l.mode == Replay {
		l.st.active.Add(1)
		l.st.epoch.Add(1)
	}
}

// watch starts a watcher unless one runs.
func (l *EffectLog) watch() {
	if l.abort == nil || !l.st.watching.CompareAndSwap(false, true) {
		return
	}
	l.st.wg.Add(1)
	go l.watcher()
}

// watcher diverges the replay if no task runs for stallRounds checks in a
// row with no task parking or waking in between. It returns as soon as
// one does, starting again if none runs by then.
func (l *EffectLog) watcher() {
	defer l.st.wg.Done()
	e := l.st.epoch.Load()
	stalled := true
	t := time.NewTimer(stallEvery)
	defer t.Stop()
	for i := 0; i < stallRounds; i++ {
		select {
		case <-l.abort.Done():
			l.st.watching.Store(false)
			return
		case <-t.C:
		}
		if l.st.active.Load() > 0 || l.st.epoch.Load() != e {
			stalled = false
			break
		}
		t.Reset(stallEvery)
	}
	if stalled {
		l.diverge("at its end: every task waits for something the log says happened, which nothing in this run will do (the log does not fit the program)")
		l.st.watching.Store(false)
		return
	}
	l.st.watching.Store(false)
	if l.st.active.Load() <= 0 && l.abort.Err() == nil {
		l.watch()
	}
}

// stopWatching waits for the watcher, if any, once the run is over.
func (l *EffectLog) stopWatching() { l.st.wg.Wait() }
