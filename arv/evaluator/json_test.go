package evaluator

import (
	"context"
	"encoding/json"
	"io"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

func TestJSONValues(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{`json(42)`, `42`},
		{`json(-7)`, `-7`},
		{`json(true)`, `true`},
		{`json(if (false) { 1 })`, `null`},
		{`json("hi")`, `"hi"`},
		{`json([])`, `[]`},
		{`json({})`, `{}`},
		{`json([1, "a", false, [2]])`, `[1,"a",false,[2]]`},
		{"json(\"a\\b\nc\td\")", `"a\\b\nc\td"`},
	} {
		got := testEval(tc.in)
		if s, ok := got.(*value.String); !ok || s.Value != tc.want {
			t.Errorf("%s = %s, want %s", tc.in, got.Inspect(), tc.want)
		}
	}
}

// Hash pairs are written in key order, every time, though a hash's own
// order is random.
func TestJSONHashKeyOrder(t *testing.T) {
	src := `json({"b": 1, "a": [2], "c": {"z": "q", "y": true}, "aa": 3})`
	want := `{"a":[2],"aa":3,"b":1,"c":{"y":true,"z":"q"}}`
	for i := 0; i < 50; i++ {
		got := testEval(src)
		if s, ok := got.(*value.String); !ok || s.Value != want {
			t.Fatalf("run %d: %s, want %s", i, got.Inspect(), want)
		}
	}
}

func TestJSONErrors(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{`json()`, `json: want 1 argument, got 0`},
		{`json(1, 2)`, `json: want 1 argument, got 2`},
		{`json(fn(x) { x })`, `json: cannot encode FUNCTION`},
		{`json([1, len])`, `json: cannot encode BUILTIN`},
		{`json({1: 2})`, `json: hash keys must be STRING, got INTEGER`},
		{`json({"a": {true: 1}})`, `json: hash keys must be STRING, got BOOLEAN`},
	} {
		got := testEval(tc.in)
		if !isError(got) || got.(*value.Error).Message != tc.want {
			t.Errorf("%s = %s, want error %q", tc.in, got.Inspect(), tc.want)
		}
	}
}

// encode calls the builtin directly, with no budget.
func encode(t *testing.T, ctx context.Context, v value.Object) value.Object {
	t.Helper()
	root := runtime.NewRoot(ctx, runtime.NewCapSet(), io.Discard)
	return jsonEncode(root, func(int64) error { return nil }, v)
}

// Any string comes back unchanged through encoding/json, apart from
// invalid UTF-8, which both sides turn into U+FFFD byte by byte.
func FuzzJSONString(f *testing.F) {
	for _, s := range []string{"", "plain", "q\"b\\s/", "\x00\x01\x1f\x7f", "\b\f\n\r\t", "é€😀", "\xff\xfe a \xc3", " <&>"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		got := encode(t, context.Background(), &value.String{Value: s})
		out, ok := got.(*value.String)
		if !ok {
			t.Fatalf("json(%q) = %s", s, got.Inspect())
		}
		var back string
		if err := json.Unmarshal([]byte(out.Value), &back); err != nil {
			t.Fatalf("json(%q) = %s: not JSON: %v", s, out.Value, err)
		}
		ref, _ := json.Marshal(s)
		var want string
		if err := json.Unmarshal(ref, &want); err != nil {
			t.Fatal(err)
		}
		if back != want {
			t.Fatalf("json(%q) decodes to %q, want %q", s, back, want)
		}
	})
}

// Nesting is bounded, as Inspect's is, so no value overflows the Go stack.
func TestJSONNestingLimit(t *testing.T) {
	var v value.Object = &value.Array{}
	for i := 0; i < value.MaxInspectDepth; i++ {
		v = &value.Array{Elements: []value.Object{v}}
	}
	got := encode(t, context.Background(), v)
	if !isError(got) || got.(*value.Error).Message != "json: nested deeper than 10000 levels" {
		t.Fatalf("got %.80s", got.Inspect())
	}
}
