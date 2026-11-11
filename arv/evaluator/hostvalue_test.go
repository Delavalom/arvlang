package evaluator

import (
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/value"
)

// A host effect's result is Arv data: what Phase 6 can log and what the
// program can compare. hostValue maps the host's booleans and nils to the
// evaluator's own (== compares them by identity) and says what to charge.
func TestHostValueNormalisesData(t *testing.T) {
	s := &value.String{Value: "hello"}
	for _, tt := range []struct {
		name string
		in   value.Object
		want string
		size int64
	}{
		{"go nil", nil, "nil", 0},
		{"nil value", &value.Nil{}, "nil", 0},
		{"integer", &value.Integer{Value: 7}, "7", 0},
		{"string", s, "hello", 5},
		{"array", &value.Array{Elements: []value.Object{s, &value.Integer{Value: 1}}}, "[hello, 1]", 2*arrayElemSize + 5},
		{"nested", &value.Array{Elements: []value.Object{&value.Array{Elements: []value.Object{s}}}}, "[[hello]]", 2*arrayElemSize + 5},
		{"hash", value.NewHash(map[string]value.Object{"msg": s}), "{msg: hello}", hashPairSize + 3 + 5},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, size, err := hostValue(tt.in)
			if err != nil {
				t.Fatal(err)
			}
			if got.Inspect() != tt.want || size != tt.size {
				t.Fatalf("hostValue = %s (size %d), want %s (size %d)", got.Inspect(), size, tt.want, tt.size)
			}
		})
	}
	got, _, _ := hostValue(&value.Boolean{Value: true})
	if got != TRUE {
		t.Fatal("a host's true is not the evaluator's TRUE")
	}
	got, _, _ = hostValue(&value.Nil{})
	if got != NIL {
		t.Fatal("a host's nil is not the evaluator's NIL")
	}
	in := &value.Array{Elements: []value.Object{s}}
	if got, _, _ := hostValue(in); got != in {
		t.Fatal("an array of data was copied")
	}
	in = &value.Array{Elements: []value.Object{s, &value.Boolean{Value: false}, nil}}
	got, _, _ = hostValue(in)
	arr := got.(*value.Array)
	if got == in || arr.Elements[0] != s || arr.Elements[1] != FALSE || arr.Elements[2] != NIL {
		t.Fatalf("got %s, want a copy holding FALSE and NIL", got.Inspect())
	}
	if in.Elements[1] == FALSE || in.Elements[2] != nil {
		t.Fatal("the host's array was changed")
	}
	h := value.NewHash(map[string]value.Object{"ok": &value.Boolean{Value: true}})
	got, _, _ = hostValue(h)
	if got == h || got.(*value.Hash).Pairs[(&value.String{Value: "ok"}).HashKey()].Value != TRUE {
		t.Fatalf("got %s, want a copy holding TRUE", got.Inspect())
	}
}

// Anything that is not data is refused: a function or builtin (it could
// be an effect no grant checks), a task handle, a capability value, an
// error inside data, a nil pointer, a hash whose keys lie, a cycle.
func TestHostValueRefusesNonData(t *testing.T) {
	cyclic := &value.Array{Elements: make([]value.Object, 1)}
	cyclic.Elements[0] = cyclic
	deep := value.Object(&value.Integer{Value: 1})
	for i := 0; i <= maxHostDepth; i++ {
		deep = &value.Array{Elements: []value.Object{deep}}
	}
	lying := &value.Hash{Pairs: map[value.HashKey]value.HashPair{
		(&value.String{Value: "a"}).HashKey(): {Key: &value.String{Value: "b"}, Value: NIL},
	}}
	fnKey := &value.Hash{Pairs: map[value.HashKey]value.HashPair{
		{Type: value.FUNCTION_VAL}: {Key: &value.Function{}, Value: NIL},
	}}
	for _, tt := range []struct {
		name string
		in   value.Object
		want string
	}{
		{"builtin", &value.Builtin{}, "BUILTIN"},
		{"function", &value.Function{}, "FUNCTION"},
		{"task", &value.TaskHandle{}, "TASK"},
		{"capability", &value.Capability{}, "CAPABILITY"},
		{"error in array", &value.Array{Elements: []value.Object{&value.Error{Message: "x"}}}, "ERROR"},
		{"builtin in hash", value.NewHash(map[string]value.Object{"f": &value.Builtin{}}), "BUILTIN"},
		{"nil string", (*value.String)(nil), "nil"},
		{"nil array", (*value.Array)(nil), "nil"},
		{"lying hash key", lying, "key"},
		{"function key", fnKey, "key"},
		{"cycle", cyclic, "deeper than"},
		{"too deep", deep, "deeper than"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got, _, err := hostValue(tt.in); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("hostValue = %v, %v; want an error mentioning %q", got, err, tt.want)
			}
		})
	}
}
