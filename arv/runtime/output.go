package runtime

import (
	"io"
	"sync"
)

// output prints a logged run's lines (puts) in the order the recorded run
// printed them, whatever order its tasks reach them in when replayed: a
// replayed line waits for every line logged before it. A live line (a
// recording, or a resumed task past its records) takes the next index and
// prints after every logged line.
type output struct {
	mu      sync.Mutex
	w       io.Writer
	logged  []uint64          // the logged lines' indices, sorted
	next    int               // logged[next] is the next logged line due
	pending map[uint64]string // logged lines reached before their turn
	queued  []string          // live lines that came before every logged one printed
	n       uint64            // the index the next live line takes
}

func newOutput(w io.Writer, logged []uint64) *output {
	o := &output{w: w, logged: logged, pending: map[uint64]string{}}
	if len(logged) > 0 {
		o.n = logged[len(logged)-1] + 1
	}
	return o
}

// print writes live lines, and returns the index each took.
func (o *output) print(lines []string) []uint64 {
	o.mu.Lock()
	defer o.mu.Unlock()
	ks := make([]uint64, len(lines))
	for i, s := range lines {
		ks[i] = o.n
		o.n++
		if o.next == len(o.logged) {
			WriteLine(o.w, s)
		} else {
			o.queued = append(o.queued, s)
		}
	}
	return ks
}

// emit prints replayed lines, each once every line logged before it has.
func (o *output) emit(ks []uint64, lines []string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	for i, k := range ks {
		o.pending[k] = lines[i]
	}
	o.drain()
}

func (o *output) drain() {
	for o.next < len(o.logged) {
		s, ok := o.pending[o.logged[o.next]]
		if !ok {
			return
		}
		delete(o.pending, o.logged[o.next])
		WriteLine(o.w, s)
		o.next++
	}
	for _, s := range o.queued {
		WriteLine(o.w, s)
	}
	o.queued = nil
}

// flush prints whatever is still held, in order: at the end of a run that
// diverged, some logged lines never came.
func (o *output) flush() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.drain()
	for ; o.next < len(o.logged); o.next++ {
		if s, ok := o.pending[o.logged[o.next]]; ok {
			WriteLine(o.w, s)
		}
	}
	for _, s := range o.queued {
		WriteLine(o.w, s)
	}
	o.queued = nil
}

// Print writes lines, the text of one puts by t, as one effect named
// "puts" whose request is req (their encoding). Replayed, it prints them
// once every line the recorded run printed before them has been: the
// output is the recorded run's, byte for byte, whatever the order its
// tasks get there in.
func (t *Task) Print(req []byte, lines []string) error {
	l := t.log
	if l == nil {
		for _, s := range lines {
			WriteLine(t.Out, s)
		}
		return nil
	}
	o, err := t.Effect("puts", req, func() Outcome { return Outcome{Out: l.out.print(lines)} })
	if err != nil || !o.Replayed {
		return err
	}
	if len(o.Out) != len(lines) {
		return l.diverge("at task %s, effect %d: the log printed %d lines, the program %d", t.ID, t.effects, len(o.Out), len(lines))
	}
	l.out.emit(o.Out, lines)
	return nil
}
