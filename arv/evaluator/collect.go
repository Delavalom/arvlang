package evaluator

import (
	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// collected is what a collect scope keeps to report, when it closes, the
// failures nobody awaited: its tasks' handles, in spawn order. Only the
// interpreter that opened the scope spawns into it and closes it, so it
// needs no lock.
type collected struct {
	handles []*value.TaskHandle
}

// collecting starts keeping s's tasks if s collects (supervise:
// "collect"). withScope forgets them when s closes.
func (in *Interpreter) collecting(s *runtime.Scope) {
	if s.Policy().Strategy != runtime.Collect {
		return
	}
	if in.collects == nil {
		in.collects = map[*runtime.Scope]*collected{}
	}
	in.collects[s] = &collected{}
}

// track keeps h, just spawned into s, if s collects.
func (in *Interpreter) track(s *runtime.Scope, h *value.TaskHandle) {
	if c := in.collects[s]; c != nil {
		c.handles = append(c.handles, h)
	}
}

// unawaited is the error of a collect scope s that closed with failed
// tasks nobody awaited, or nil: under collect a failure cancels nothing
// and is only its task's result, so a failure nobody looked at would
// otherwise be lost. It says how many there were and gives the first
// one's message (clipped: error text is not charged), kind, position and
// task.
func (in *Interpreter) unawaited(s *runtime.Scope) *value.Error {
	c := in.collects[s]
	if c == nil {
		return nil
	}
	var first *value.Error
	n := 0
	for _, h := range c.handles {
		if e, ok := h.Result().(*value.Error); ok && !h.Awaited() {
			if n++; first == nil {
				first = e
			}
		}
	}
	if first == nil {
		return nil
	}
	e := newErrorKind(first.ErrKind(), "%d unawaited task failures: %s", n, clipped(first.Message))
	e.Pos, e.Task = first.Pos, first.Task
	return e
}
