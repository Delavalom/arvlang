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
//
// A resume counts the same way (review rounds 4 and 5): its replayed part
// is a replay, and a forged log can stop it the same way, before or after
// the log has drained (a forged cancellation can be its last record). A
// live task counts as running unless it is parked too (it waits for the
// log to drain, Task.Fail, for a logged task to end, waitDone, for its
// scope's tasks, or, since review round 6, for another task in a live
// await, WaitFor, or for a slot, Acquire). A task in a live wait for
// anything else (a sleep, a request) is never parked. A resume can also
// have a live deadline timer set (deadline.arm), which ends waits when it
// fires: while one is set, the watcher waits for it rather than diverge.
// The deadline comes from the log's header, so a forged header sets how
// long that wait can be.
const (
	stallEvery  = 20 * time.Millisecond
	stallRounds = 50
)

// stall is the replay's count of running tasks.
type stall struct {
	active   atomic.Int64  // tasks running, not parked in a replay's wait
	timers   atomic.Int64  // a resume's live deadline timers set and not fired (deadline.reset)
	epoch    atomic.Uint64 // bumped each time a task parks or wakes
	watching atomic.Bool   // a watcher is running
	wg       sync.WaitGroup
}

// Enter counts a task about to start (a spawn's goroutine) as running.
func (l *EffectLog) Enter() {
	if l.counts() {
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
	if !l.counts() {
		return
	}
	l.st.epoch.Add(1)
	if l.st.active.Add(-1) <= 0 {
		l.watch()
	}
}

func (l *EffectLog) unpark() {
	if l.counts() {
		l.st.active.Add(1)
		l.st.epoch.Add(1)
	}
}

// counts reports that l counts its running tasks: a replay or a resume.
func (l *EffectLog) counts() bool { return l != nil && l.mode != Record }

// watch starts a watcher unless one runs.
func (l *EffectLog) watch() {
	if l.abort == nil || !l.st.watching.CompareAndSwap(false, true) {
		return
	}
	l.st.wg.Add(1)
	go l.watcher()
}

// watcher diverges the replay if no task runs for stallRounds checks in a
// row with no task parking or waking in between, and no live deadline
// timer set. It returns as soon as a task parks or wakes, starting again
// if none runs by then.
func (l *EffectLog) watcher() {
	defer l.st.wg.Done()
	e := l.st.epoch.Load()
	t := time.NewTimer(stallEvery)
	defer t.Stop()
	for quiet := 0; quiet < stallRounds; quiet++ {
		select {
		case <-l.abort.Done():
			l.st.watching.Store(false)
			return
		case <-t.C:
		}
		if l.st.active.Load() > 0 || l.st.epoch.Load() != e {
			l.st.watching.Store(false)
			if l.st.active.Load() <= 0 && l.abort.Err() == nil {
				l.watch()
			}
			return
		}
		if l.st.timers.Load() > 0 {
			quiet = -1 // the timer will fire: wait for it
		}
		t.Reset(stallEvery)
	}
	l.diverge("at its end: every task waits for something the log says happened, which nothing in this run will do (the log does not fit the program)")
	l.st.watching.Store(false)
}

// stopWatching waits for the watcher, if any, once the run is over.
func (l *EffectLog) stopWatching() { l.st.wg.Wait() }
