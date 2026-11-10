package value

import (
	"bytes"
	"fmt"
	"hash/fnv"
	"strings"

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

type Error struct {
	Message    string
	StackTrace string
}

func (e *Error) Type() ObjectType { return ERROR_VAL }
func (e *Error) Inspect() string  { return "ERROR: " + e.Message }

type Function struct {
	Parameters []*ast.Identifier
	Body       *ast.BlockStatement
	Env        *Environment
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
type Builtin struct {
	Fn BuiltinFunction
}

func (b *Builtin) Type() ObjectType { return BUILTIN_VAL }
func (b *Builtin) Inspect() string  { return "builtin function" }

type Array struct {
	Elements []Object
}

func (ao *Array) Type() ObjectType { return ARRAY_VAL }
func (ao *Array) Inspect() string {
	var out bytes.Buffer
	elements := []string{}
	for _, e := range ao.Elements {
		elements = append(elements, e.Inspect())
	}
	out.WriteString("[")
	out.WriteString(strings.Join(elements, ", "))
	out.WriteString("]")
	return out.String()
}

type Hashable interface {
	HashKey() HashKey
}

type Hash struct {
	Pairs map[HashKey]HashPair
}

func (h *Hash) Type() ObjectType { return HASH_VAL }

func (h *Hash) Inspect() string {
	var out bytes.Buffer
	pairs := []string{}
	for _, pair := range h.Pairs {
		pairs = append(pairs, fmt.Sprintf("%s: %s",
			pair.Key.Inspect(), pair.Value.Inspect()))
	}
	out.WriteString("{")
	out.WriteString(strings.Join(pairs, ", "))
	out.WriteString("}")
	return out.String()
}

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
func NewHash(fields map[string]Object) *Hash {
	pairs := make(map[HashKey]HashPair, len(fields))
	for k, v := range fields {
		key := &String{Value: k}
		pairs[key.HashKey()] = HashPair{Key: key, Value: v}
	}
	return &Hash{Pairs: pairs}
}

// TaskHandle is what spawn returns; await reads its result.
type TaskHandle struct {
	ID     string
	done   chan struct{}
	result Object
}

func NewTaskHandle(id string) *TaskHandle {
	return &TaskHandle{ID: id, done: make(chan struct{})}
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
