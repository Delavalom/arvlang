package value

import (
	"math"
	"sync"

	"github.com/delavalom/arvlang/arv/runtime"
)

func NewEnvironment() *Environment {
	return &Environment{born: runtime.Now()}
}

// Environment is a lexical scope. Closures passed to spawn are read by the
// child task while the parent may still bind names, so access is locked,
// and a name keeps what it was bound to before, for tasks spawned before
// it was bound again (I8: a child never sees its parent rebind a name
// after the spawn; see GetAs).
//
// The first envInline names live in the environment itself, so a call or
// a loop iteration that binds a few names allocates one environment and
// no map (Phase 8.4: most environments hold a call's parameters or a
// loop variable). Past envInline names, a map holds the rest.
type Environment struct {
	mu    sync.RWMutex
	slots [envInline]slot     // the first names bound here, in order
	n     int                 // slots in use
	more  map[string]*binding // names past the first envInline; nil until then
	outer *Environment
	born  uint64 // runtime.Now() when created
}

// envInline is how many names an environment holds without a map.
const envInline = 4

// slot is one name bound in an environment and its latest binding.
type slot struct {
	name string
	b    *binding
}

// binding is one value a name was bound to, and the one before it.
type binding struct {
	val  Object
	at   uint64 // runtime.Now() when bound
	prev *binding
}

// latest is name's latest binding here, or nil. The caller holds e.mu.
func (e *Environment) latest(name string) *binding {
	for i := 0; i < e.n; i++ {
		if e.slots[i].name == name {
			return e.slots[i].b
		}
	}
	return e.more[name]
}

// Get finds name as the task that owns the environment sees it: its
// latest binding.
func (e *Environment) Get(name string) (Object, bool) { return e.GetAs(name, nil) }

// GetAs finds name as task t sees it: in each environment, from e
// outwards, the latest binding made before t's horizon there
// (runtime.Task.Horizon). So a task spawned with a closure, or anything
// that holds one, keeps seeing the environments it captured as they were
// at the spawn, even through closures defined earlier or values passed to
// it, and so does everything it spawns. A nil t sees the latest bindings.
func (e *Environment) GetAs(name string, t *runtime.Task) (Object, bool) {
	for env := e; env != nil; env = env.outer {
		limit := uint64(math.MaxUint64)
		if t != nil {
			limit = t.Horizon(env.born)
		}
		env.mu.RLock()
		b := env.latest(name)
		for b != nil && b.at >= limit {
			b = b.prev
		}
		env.mu.RUnlock()
		if b != nil {
			return b.val, true
		}
	}
	return nil, false
}

// Set binds name to val. If no task was spawned since name was last bound
// here, no task can see the old value, and it is replaced; otherwise it
// is kept for the tasks spawned in between.
func (e *Environment) Set(name string, val Object) Object {
	now := runtime.Now()
	e.mu.Lock()
	defer e.mu.Unlock()
	for i := 0; i < e.n; i++ {
		if s := &e.slots[i]; s.name == name {
			if s.b.at == now {
				s.b.val = val
			} else {
				s.b = &binding{val: val, at: now, prev: s.b}
			}
			return val
		}
	}
	b := &binding{val: val, at: now}
	if e.n < envInline {
		e.slots[e.n] = slot{name: name, b: b}
		e.n++
		return val
	}
	if old := e.more[name]; old != nil {
		if old.at == now {
			old.val = val
			return val
		}
		b.prev = old
	} else if e.more == nil {
		e.more = make(map[string]*binding)
	}
	e.more[name] = b
	return val
}

func NewEnclosedEnvironment(outer *Environment) *Environment {
	env := NewEnvironment()
	env.outer = outer
	return env
}
