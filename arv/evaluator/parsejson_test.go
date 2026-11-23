package evaluator

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// decodeText calls the builtin directly on text, with no budget.
func decodeText(t *testing.T, ctx context.Context, text string) value.Object {
	t.Helper()
	root := runtime.NewRoot(ctx, runtime.NewCapSet(), io.Discard)
	return parseJSON(root, func(int64) error { return nil }, &value.String{Value: text})
}

// parseIn evaluates parse_json(s) with s bound to text: Arv string
// literals have no escapes, so a test cannot write JSON in one.
func parseIn(t *testing.T, text string, l runtime.Limits) value.Object {
	t.Helper()
	root := runtime.NewRoot(context.Background(), runtime.NewCapSet(), io.Discard)
	in := New(root)
	env := value.NewEnvironment()
	env.Set("s", &value.String{Value: text})
	b := runtime.Root(l)
	return in.withScope(frame{runtime.NewScope(root.Ctx), b}, func() value.Object {
		return in.Eval(parse(t, `parse_json(s)`), env)
	})
}

func TestParseJSONValues(t *testing.T) {
	for _, tt := range []struct{ text, want string }{
		{`5`, "5"},
		{` -42 `, "-42"},
		{`-0`, "0"},
		{`9223372036854775807`, "9223372036854775807"},
		{`-9223372036854775808`, "-9223372036854775808"},
		{`true`, "true"},
		{`false`, "false"},
		{`null`, "nil"},
		{`"hi"`, "hi"},
		{`""`, ""},
		{`[]`, "[]"},
		{`{}`, "{}"},
		{"[1, \"a\", true, null, [2]]", "[1, a, true, nil, [2]]"},
		{`{"b": 1, "a": [true]}`, "{b: 1, a: [true]}"}, // source order
		{"\t\r\n {\"k\" :\n\"v\"} \n", "{k: v}"},
		{`"q\"b\\s\/ \b\f\n\r\t"`, "q\"b\\s/ \b\f\n\r\t"},
		{`"Aé€"`, "Aé€"},
		{`"😀"`, "😀"},
		{`"\ud83d"`, "�"},   // a lone surrogate, as Go reads it
		{`"\ude00x"`, "�x"}, // a lone low surrogate
		{`"\ud83dA"`, "�A"}, // a high surrogate not followed by a low one
		{"\"\xff a \xc3\"", "� a �"},
		{"\"é€😀\"", "é€😀"},
		// Numbers whose value is an integer are integers, however written.
		{`1e3`, "1000"},
		{`1E+2`, "100"},
		{`1.0`, "1"},
		{`1.50e1`, "15"},
		{`100e-2`, "1"},
		{`0.000e5`, "0"},
		{`-9.223372036854775808e18`, "-9223372036854775808"},
	} {
		got := decodeText(t, context.Background(), tt.text)
		if got.Inspect() != tt.want {
			t.Errorf("parse_json(%q) = %s, want %s", tt.text, got.Inspect(), tt.want)
		}
	}
}

// The result is ordinary data: a hash with string keys and an array.
func TestParseJSONResultIsData(t *testing.T) {
	got := parseIn(t, `{"a": [1, 2], "b": {"c": "d"}}`, runtime.Limits{})
	h, ok := got.(*value.Hash)
	if !ok {
		t.Fatalf("got %s", got.Inspect())
	}
	if json := encode(t, context.Background(), h); json.Inspect() != `{"a":[1,2],"b":{"c":"d"}}` {
		t.Fatalf("json of the result: %s", json.Inspect())
	}
	src := `let s = json({"x": [1, "y"]}); let v = parse_json(s); v["x"][1] + str(len(v["x"]))`
	if _, v := runOut(t, src); v.Inspect() != "y2" {
		t.Fatalf("%s = %s", src, v.Inspect())
	}
}

func TestParseJSONErrors(t *testing.T) {
	for _, tt := range []struct{ text, want string }{
		{``, "parse_json: unexpected end of JSON input"},
		{`   `, "parse_json: unexpected end of JSON input"},
		{`[1,`, "parse_json: unexpected end of JSON input"},
		{`"abc`, "parse_json: unexpected end of JSON input"},
		{`1 2`, "parse_json: invalid character '2' after the top-level value at line 1, column 3"},
		{`[1 2]`, "parse_json: invalid character '2' after an array element at line 1, column 4"},
		{`{"a" 1}`, "parse_json: invalid character '1' after an object key at line 1, column 6"},
		{`{"a": 1 "b": 2}`, "parse_json: invalid character '\"' after an object value at line 1, column 9"},
		{`{1: 2}`, "parse_json: invalid character '1' looking for an object key at line 1, column 2"},
		{`[1,]`, "parse_json: invalid character ']' looking for a value at line 1, column 4"},
		{`{"a":1,}`, "parse_json: invalid character '}' looking for an object key at line 1, column 8"},
		{"\n\n  x", "parse_json: invalid character 'x' looking for a value at line 3, column 3"},
		{`tru`, "parse_json: unexpected end of JSON input"},
		{`trux`, "parse_json: invalid literal at line 1, column 1"},
		{`nulL`, "parse_json: invalid literal at line 1, column 1"},
		{`01`, "parse_json: invalid character '1' after the top-level value at line 1, column 2"},
		{`1.`, "parse_json: unexpected end of JSON input"},
		{`[1.]`, "parse_json: invalid number at line 1, column 2"},
		{`-a`, "parse_json: invalid number at line 1, column 1"},
		{`1ex`, "parse_json: invalid number at line 1, column 1"},
		{`1e+]`, "parse_json: invalid number at line 1, column 1"},
		{`.5`, "parse_json: invalid character '.' looking for a value at line 1, column 1"},
		{`+1`, "parse_json: invalid character '+' looking for a value at line 1, column 1"},
		{`1e`, "parse_json: unexpected end of JSON input"},
		{`-`, "parse_json: unexpected end of JSON input"},
		{"\"a\x01\"", "parse_json: invalid character '\\x01' in a string at line 1, column 3"},
		{`"\x"`, "parse_json: invalid escape '\\x' in a string at line 1, column 2"},
		{`"\u12g4"`, "parse_json: invalid escape '\\u12g4' in a string at line 1, column 2"},
		{"\xef\xbb\xbf1", "parse_json: invalid character '\\xef' looking for a value at line 1, column 1"},
		// Arv has integers only (open decision D3).
		{`1.5`, "parse_json: number 1.5 at line 1, column 1 is not an integer, and Arv has no floats yet"},
		{`[0, 1e-1]`, "parse_json: number 1e-1 at line 1, column 5 is not an integer, and Arv has no floats yet"},
		{`9223372036854775808`, "parse_json: number 9223372036854775808 at line 1, column 1 is out of the integer range"},
		{`-9223372036854775809`, "parse_json: number -9223372036854775809 at line 1, column 1 is out of the integer range"},
		{`1e19`, "parse_json: number 1e19 at line 1, column 1 is out of the integer range"},
		{`1e999999999999999999999`, "parse_json: number 1e999999999999999999999 at line 1, column 1 is out of the integer range"},
		{`1e-999999999999999999999`, "parse_json: number 1e-999999999999999999999 at line 1, column 1 is not an integer, and Arv has no floats yet"},
		// Duplicate keys are refused: two readers could see two values.
		{`{"a": 1, "a": 2}`, `parse_json: duplicate key "a" at line 1, column 10`},
		{`{"a": {"b": 1, "b": 1}}`, `parse_json: duplicate key "b" at line 1, column 16`},
	} {
		got := decodeText(t, context.Background(), tt.text)
		e, ok := got.(*value.Error)
		if !ok || e.Message != tt.want || e.ErrKind() != runtime.KindRuntime {
			t.Errorf("parse_json(%q) = %s, want runtime error %q", tt.text, got.Inspect(), tt.want)
		}
	}
}

func TestParseJSONArguments(t *testing.T) {
	for src, want := range map[string]string{
		`parse_json()`:         "ERROR: parse_json: want 1 argument, got 0",
		`parse_json("1", "2")`: "ERROR: parse_json: want 1 argument, got 2",
		`parse_json(1)`:        "ERROR: parse_json: argument must be STRING, got INTEGER",
	} {
		if _, got := runOut(t, src); got.Inspect() != want {
			t.Errorf("%s = %s, want %s", src, got.Inspect(), want)
		}
	}
}

// Error text stays small however long the input: a number or a key is
// quoted only in part.
func TestParseJSONErrorTextIsBounded(t *testing.T) {
	long := strings.Repeat("7", 1<<20)
	for _, text := range []string{
		long + ".5",
		long,
		`{"` + long + `": 1, "` + long + `": 2}`,
		`"` + long + `\q"`,
		`"\uZZZZ` + long + `"`,
	} {
		got := decodeText(t, context.Background(), text)
		e, ok := got.(*value.Error)
		if !ok {
			t.Fatalf("got %.80s", got.Inspect())
		}
		if len(e.Message) > 400 {
			t.Errorf("error is %d bytes: %.120s", len(e.Message), e.Message)
		}
	}
}

// Nesting is bounded as json's is: exactly what json can write reads back.
func TestParseJSONNestingLimit(t *testing.T) {
	deep := func(n int) string { return strings.Repeat("[", n) + strings.Repeat("]", n) }
	if got := decodeText(t, context.Background(), deep(value.MaxInspectDepth)); isError(got) {
		t.Fatalf("%d levels: %.80s", value.MaxInspectDepth, got.Inspect())
	}
	for _, text := range []string{deep(value.MaxInspectDepth + 1), strings.Repeat(`{"a":`, value.MaxInspectDepth+1), strings.Repeat("[", 1<<20)} {
		got := decodeText(t, context.Background(), text)
		if e, ok := got.(*value.Error); !ok || e.Message != "parse_json: nested deeper than 10000 levels" {
			t.Fatalf("got %.80s", got.Inspect())
		}
	}
}

// What parse_json builds is charged: string bytes, 16 per array element,
// 48 per hash pair plus its key's bytes, and 48 per array or hash.
func TestParseJSONCharges(t *testing.T) {
	for _, tt := range []struct {
		text string
		want int64
	}{
		{`5`, 0},
		{`"abc"`, 3},
		{`[]`, 48},
		{`[1, 2]`, 48 + 32},
		{`{"ab": "cde"}`, 48 + 48 + 2 + 3},
		{`[[], {}]`, 48 + 32 + 48 + 48},
	} {
		var charged int64
		root := runtime.NewRoot(context.Background(), runtime.NewCapSet(), io.Discard)
		got := parseJSON(root, func(n int64) error { charged += n; return nil }, &value.String{Value: tt.text})
		if isError(got) || charged != tt.want {
			t.Errorf("parse_json(%s) = %s charging %d, want %d", tt.text, got.Inspect(), charged, tt.want)
		}
		if tt.want == 0 {
			continue
		}
		if got := parseIn(t, tt.text, runtime.Limits{AllocBytes: tt.want}); isError(got) {
			t.Errorf("parse_json(%s) under %d bytes: %s", tt.text, tt.want, got.Inspect())
		}
		got = parseIn(t, tt.text, runtime.Limits{AllocBytes: tt.want - 1})
		if e, ok := got.(*value.Error); !ok || e.ErrKind() != runtime.KindBudget {
			t.Errorf("parse_json(%s) under %d bytes: got %s, want a budget error", tt.text, tt.want-1, got.Inspect())
		}
	}
}

// A small text that builds much more stops at the limit, charged before
// it is built, one chunk at a time.
func TestParseJSONStopsAtTheLimit(t *testing.T) {
	text := "[" + strings.Repeat("[],", 1<<20) + "[]]" // 3 MiB of text, about 64 MiB charged
	var got value.Object
	used := heapDuring(func() { got = parseIn(t, text, runtime.Limits{AllocBytes: 1 << 20}) })
	if e, ok := got.(*value.Error); !ok || e.ErrKind() != runtime.KindBudget || e.Pos != "1:1" {
		t.Fatalf("got %.80s", got.Inspect())
	}
	if used > 8<<20 {
		t.Errorf("parse_json past a 1 MiB limit allocated %d bytes", used)
	}
}

// With no alloc limit, a cancelled task stops parsing at the next chunk,
// even where the text builds nothing (white space).
func TestParseJSONStopsWhenCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, text := range []string{
		"[" + strings.Repeat("1,", 1<<20) + "1]",
		strings.Repeat(" ", 1<<20) + "1",
	} {
		got := decodeText(t, ctx, text)
		if e, ok := got.(*value.Error); !ok || e.ErrKind() != runtime.KindCancelled {
			t.Fatalf("got %.80s, want a cancelled error", got.Inspect())
		}
	}
	if got := decodeText(t, ctx, "1"); got.Inspect() != "1" { // small inputs never look
		t.Fatalf("got %s", got.Inspect())
	}
}

// Both engines agree on values, errors, positions, steps and bytes.
func TestParseJSONEnginesAgree(t *testing.T) {
	for _, src := range []string{
		`parse_json(json({"a": [1, "b", true, nil], "c": {}}))`,
		`let v = parse_json(json([1, [2, [3]]])); v[1][1][0] + 1`,
		`parse_json(json("x") + "1")`,
		`parse_json("1") + parse_json("2")`,
		`parse_json(5)`,
		`let f = fn(s) { parse_json(s) }; f("tru")`,
		`match parse_json("[") { ok(v) => v, err(e) => e["message"] }`,
	} {
		sameOnBothEngines(t, src)
	}
}
