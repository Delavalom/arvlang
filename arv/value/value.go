package value

import (
	"bytes"
	"fmt"
	"hash/fnv"
	"sort"
	"strings"
	"sync/atomic"

	"github.com/delavalom/arvlang/arv/ast"
	"github.com/delavalom/arvlang/arv/runtime"
)

type ObjectType string

type Object interface {
	Type() ObjectType
	Inspect() string
}

const (
	INTEGER_VAL      = "INTEGER"
	BOOLEAN_VAL      = "BOOLEAN"
	NIL_VAL          = "NIL"
	RETURN_VALUE_VAL = "RETURN_VALUE"
	ERROR_VAL        = "ERROR"
	FUNCTION_VAL     = "FUNCTION"
	STRING_VAL       = "STRING"
	BUILTIN_VAL      = "BUILTIN"
	ARRAY_VAL        = "ARRAY"
	HASH_VAL         = "HASH"
	CAPABILITY_VAL   = "CAPABILITY"
	TASK_VAL         = "TASK"
)

type Integer struct {
	Value int64
}

func (i *Integer) Inspect() string  { return fmt.Sprintf("%d", i.Value) }
func (i *Integer) Type() ObjectType { return INTEGER_VAL }

type Boolean struct {
	Value bool
}

func (b *Boolean) Type() ObjectType { return BOOLEAN_VAL }
func (b *Boolean) Inspect() string  { return fmt.Sprintf("%t", b.Value) }

type Nil struct{}

func (n *Nil) Type() ObjectType { return NIL_VAL }
func (n *Nil) Inspect() string  { return "nil" }

type ReturnValue struct {
	Value Object
}

func (rv *ReturnValue) Type() ObjectType { return RETURN_VALUE_VAL }
func (rv *ReturnValue) Inspect() string  { return rv.Value.Inspect() }

// Error is an Arv error value. Pos is "line:col" of the innermost node it
// arose in or passed through ("" if none), and Task the ID of the task it
// arose in; the evaluator sets each once, on a copy, and never changes it
// after. Kind is one of the runtime.Kind* constants ("" reads as
// runtime.KindRuntime, see ErrKind).
type Error struct {
	Message    string
	StackTrace string
	Pos        string
	Kind       string
	Task       string
}

func (e *Error) Type() ObjectType { return ERROR_VAL }
func (e *Error) Inspect() string  { return "ERROR: " + e.Message }

// ErrKind is e's kind, runtime.KindRuntime if none was set.
func (e *Error) ErrKind() string {
	if e.Kind == "" {
		return runtime.KindRuntime
	}
	return e.Kind
}

type Function struct {
	Parameters []*ast.Identifier
	Body       *ast.BlockStatement
	Env        *Environment
	// Code is Body compiled (Phase 8.4), set when compiled code made the
	// function; nil when the tree-walker did. Only package evaluator
	// reads it: both mean the same, so a call runs whichever is there.
	Code any
}

func (f *Function) Type() ObjectType { return FUNCTION_VAL }
func (f *Function) Inspect() string {
	var out bytes.Buffer
	params := []string{}
	for _, p := range f.Parameters {
		params = append(params, p.String())
	}
	out.WriteString("fn")
	out.WriteString("(")
	out.WriteString(strings.Join(params, ", "))
	out.WriteString(") {\n")
	out.WriteString(f.Body.String())
	out.WriteString("\n}")
	return out.String()
}

type String struct {
	Value string
}

func (s *String) Type() ObjectType { return STRING_VAL }
func (s *String) Inspect() string  { return s.Value }

// BuiltinFunction receives the calling task so effects can check its
// capabilities, honour its cancellation and write to its output.
type BuiltinFunction func(t *runtime.Task, args ...Object) Object

// ChargingFunction is a BuiltinFunction that charges what it allocates to
// the caller's allocation budget (alloc_mb) itself, through charge, while
// it builds its result, and stops with charge's error once the budget is
// spent. A builtin whose result can be large, such as a file read, needs
// it: a result charged only once built could use far more memory than the
// limit, once per task running it.
type ChargingFunction func(t *runtime.Task, charge func(n int64) error, args ...Object) Object

type Builtin struct {
	Fn BuiltinFunction
	// Charging, when set, is called instead of Fn, and its result is not
	// charged again.
	Charging ChargingFunction
	// Shares is set when Fn may return an existing value from its
	// arguments (as last does) instead of a new one, so its result is not
	// charged to the allocation budget.
	Shares bool
	// Effect, unless empty, names an effect builtin (I7): its result
	// depends on the world (network, files, environment, time,
	// randomness, output), so every call goes through the run's effect
	// log under this name, to be recorded or replayed.
	Effect string
}

func (b *Builtin) Type() ObjectType { return BUILTIN_VAL }
func (b *Builtin) Inspect() string  { return "builtin function" }

type Array struct {
	Elements []Object
}

func (ao *Array) Type() ObjectType { return ARRAY_VAL }
func (ao *Array) Inspect() string  { return inspect(ao) }

// MaxInspectDepth bounds how deeply Inspect descends into nested arrays and
// hashes. Values can nest without limit (`let a = [a];` repeated), and
// recursing once per level would overflow the Go stack, which is fatal.
// Deeper levels print as "...".
const MaxInspectDepth = 10_000

// inspect renders o into one builder, so nesting costs linear time.
func inspect(o Object) string {
	var ins inspector
	ins.write(o, 0)
	return ins.b.String()
}

// InspectChunk is how many bytes InspectChecked writes between checks.
const InspectChunk = 64 << 10

// InspectChecked is Inspect for a caller with a budget. A value can print
// far larger than it is (an array holding the same sub-array twice, n
// levels deep, prints as about 5 * 2^n bytes), so every InspectChunk bytes
// of text it calls check with the bytes written since the last call, and
// stops with check's error if it returns one. Text under InspectChunk
// bytes is never checked.
func InspectChecked(o Object, check func(n int64) error) (string, error) {
	ins := inspector{check: check}
	ins.write(o, 0)
	if ins.err != nil {
		return "", ins.err
	}
	return ins.b.String(), nil
}

type inspector struct {
	b       strings.Builder
	check   func(n int64) error // nil: never checked
	checked int                 // bytes of b already passed to check
	err     error               // check's error; writing has stopped
}

// put appends s, calling check once another InspectChunk bytes are written.
func (ins *inspector) put(s string) {
	ins.b.WriteString(s)
	if n := ins.b.Len() - ins.checked; ins.check != nil && n >= InspectChunk {
		ins.checked = ins.b.Len()
		ins.err = ins.check(int64(n))
	}
}

func (ins *inspector) write(o Object, depth int) {
	if ins.err != nil {
		return
	}
	switch o := o.(type) {
	case *Array:
		if depth >= MaxInspectDepth {
			ins.put("...")
			return
		}
		ins.put("[")
		for i, e := range o.Elements {
			if i > 0 {
				ins.put(", ")
			}
			if ins.write(e, depth+1); ins.err != nil {
				return
			}
		}
		ins.put("]")
	case *Hash:
		if depth >= MaxInspectDepth {
			ins.put("...")
			return
		}
		ins.put("{")
		for i, k := range o.Keys() {
			if i > 0 {
				ins.put(", ")
			}
			pair := o.Pairs[k]
			ins.write(pair.Key, depth+1)
			ins.put(": ")
			if ins.write(pair.Value, depth+1); ins.err != nil {
				return
			}
		}
		ins.put("}")
	default:
		ins.put(o.Inspect())
	}
}

type Hashable interface {
	HashKey() HashKey
}

// Hash maps keys to values and remembers the order its keys were first
// set in (Set), so Inspect, json and the Phase 6 log see the same order on
// every run: Go's map order is random, and replay needs identical output.
type Hash struct {
	Pairs map[HashKey]HashPair
	keys  []HashKey // Pairs' keys in the order Set first added them
}

// Set binds k to p. A new key goes after every key already in h; setting
// a key again keeps its place.
func (h *Hash) Set(k HashKey, p HashPair) {
	if h.Pairs == nil {
		h.Pairs = make(map[HashKey]HashPair)
	}
	if _, ok := h.Pairs[k]; !ok {
		h.keys = append(h.keys, k)
	}
	h.Pairs[k] = p
}

// NewHashSize is an empty hash with room for n keys.
func NewHashSize(n int) *Hash {
	return &Hash{Pairs: make(map[HashKey]HashPair, n), keys: make([]HashKey, 0, n)}
}

// Keys is h's keys in the order Set first added them. A hash whose Pairs
// were filled without Set (by hand, in a test) has no order recorded: its
// keys come back sorted by type and hash value, which is still the same
// order every time.
func (h *Hash) Keys() []HashKey {
	if len(h.keys) == len(h.Pairs) {
		return h.keys
	}
	keys := make([]HashKey, 0, len(h.Pairs))
	for k := range h.Pairs {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].Type != keys[j].Type {
			return keys[i].Type < keys[j].Type
		}
		return keys[i].Value < keys[j].Value
	})
	return keys
}

func (h *Hash) Type() ObjectType { return HASH_VAL }

func (h *Hash) Inspect() string { return inspect(h) }

type HashPair struct {
	Key   Object
	Value Object
}

type HashKey struct {
	Type  ObjectType
	Value uint64
}

func (b *Boolean) HashKey() HashKey {
	var value uint64
	if b.Value {
		value = 1
	} else {
		value = 0
	}
	return HashKey{Type: b.Type(), Value: value}
}
func (i *Integer) HashKey() HashKey {
	return HashKey{Type: i.Type(), Value: uint64(i.Value)}
}
func (s *String) HashKey() HashKey {
	h := fnv.New64a()
	h.Write([]byte(s.Value))
	return HashKey{Type: s.Type(), Value: h.Sum64()}
}

// Capability is an Arv value naming one grant, e.g. net("api.github.com").
// Holding the value grants nothing; spawn ... with [...] hands authority the
// parent already holds to a child, and effects check the task's set.
type Capability struct {
	Cap runtime.Capability
}

func (c *Capability) Type() ObjectType { return CAPABILITY_VAL }
func (c *Capability) Inspect() string  { return c.Cap.String() }

// NewHash builds a hash with string keys, for builtins returning records.
// Its keys are in sorted order, so a record prints the same every time.
func NewHash(fields map[string]Object) *Hash {
	names := make([]string, 0, len(fields))
	for k := range fields {
		names = append(names, k)
	}
	sort.Strings(names)
	h := NewHashSize(len(fields))
	for _, k := range names {
		key := &String{Value: k}
		h.Set(key.HashKey(), HashPair{Key: key, Value: fields[k]})
	}
	return h
}

// TaskHandle is what spawn returns; await reads its result. Owner is the
// task that spawned it: only Owner and its ancestors may await it (see
// evalAwait).
type TaskHandle struct {
	ID      string
	Owner   *runtime.Task
	done    chan struct{}
	result  Object
	awaited atomic.Bool
}

func NewTaskHandle(id string, owner *runtime.Task) *TaskHandle {
	return &TaskHandle{ID: id, Owner: owner, done: make(chan struct{})}
}

func (h *TaskHandle) Type() ObjectType { return TASK_VAL }
func (h *TaskHandle) Inspect() string  { return "task(" + h.ID + ")" }

// Finish records the result and wakes every waiter. Call it exactly once.
func (h *TaskHandle) Finish(result Object) {
	h.result = result
	close(h.done)
}

// Done is closed once the task has finished.
func (h *TaskHandle) Done() <-chan struct{} { return h.done }

// Result is valid only after Done is closed.
func (h *TaskHandle) Result() Object { return h.result }

// MarkAwaited records that an await has read the result. A collect scope
// reports the failures of tasks nobody awaited.
func (h *TaskHandle) MarkAwaited() { h.awaited.Store(true) }

// Awaited reports whether an await has read the result.
func (h *TaskHandle) Awaited() bool { return h.awaited.Load() }
