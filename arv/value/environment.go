package value

import "sync"

func NewEnvironment() *Environment {
	return &Environment{store: make(map[string]Object)}
}

// Environment is a lexical scope. Closures passed to spawn are read by the
// child task while the parent may still bind names, so access is locked.
type Environment struct {
	mu    sync.RWMutex
	store map[string]Object
	outer *Environment
}

func (e *Environment) Get(name string) (Object, bool) {
	e.mu.RLock()
	obj, ok := e.store[name]
	e.mu.RUnlock()
	if !ok && e.outer != nil {
		return e.outer.Get(name)
	}
	return obj, ok
}

func (e *Environment) Set(name string, val Object) Object {
	e.mu.Lock()
	e.store[name] = val
	e.mu.Unlock()
	return val
}

func NewEnclosedEnvironment(outer *Environment) *Environment {
	env := NewEnvironment()
	env.outer = outer
	return env
}
