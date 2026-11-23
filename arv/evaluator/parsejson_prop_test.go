package evaluator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/rand/v2"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/delavalom/arvlang/arv/value"
)

// genValue is a random value json accepts: integers (edges included),
// strings (escapes, control bytes, multi-byte runes and, when bad is set,
// invalid UTF-8), booleans, nil, and arrays and string-keyed hashes.
func genValue(r *rand.Rand, depth int, bad bool) value.Object {
	k := r.IntN(8)
	if depth <= 0 && k >= 6 {
		k = r.IntN(6)
	}
	switch k {
	case 0:
		edges := []int64{0, -1, 1, math.MaxInt64, math.MinInt64, 1e18, -1e18}
		if r.IntN(2) == 0 {
			return &value.Integer{Value: edges[r.IntN(len(edges))]}
		}
		return &value.Integer{Value: r.Int64() >> r.IntN(64)}
	case 1, 2:
		return &value.String{Value: genString(r, bad)}
	case 3:
		return &value.Boolean{Value: r.IntN(2) == 0}
	case 4:
		return NIL
	case 5:
		return &value.String{Value: ""}
	case 6:
		n := r.IntN(5)
		els := make([]value.Object, n)
		for i := range els {
			els[i] = genValue(r, depth-1, bad)
		}
		return &value.Array{Elements: els}
	default:
		h := value.NewHashSize(0)
		for i, n := 0, r.IntN(5); i < n; i++ {
			key := &value.String{Value: genString(r, bad)}
			h.Set(key.HashKey(), value.HashPair{Key: key, Value: genValue(r, depth-1, bad)})
		}
		return h
	}
}

func genString(r *rand.Rand, bad bool) string {
	parts := []string{"a", "Z", "0", " ", "\"", "\\", "/", "\n", "\t", "\x00", "\x1f", "\x7f", "é", "€", "😀", " ", "<&>", "�"}
	if bad {
		parts = append(parts, "\xff", "\xc3", "\xed\xa0\x80")
	}
	var b strings.Builder
	for i, n := 0, r.IntN(8); i < n; i++ {
		b.WriteString(parts[r.IntN(len(parts))])
	}
	return b.String()
}

// sameValue reports whether a and b are the same data: hashes compare by
// their keys, whatever order they were set in.
func sameValue(a, b value.Object) bool {
	switch a := a.(type) {
	case *value.Integer:
		b, ok := b.(*value.Integer)
		return ok && a.Value == b.Value
	case *value.String:
		b, ok := b.(*value.String)
		return ok && a.Value == b.Value
	case *value.Boolean:
		b, ok := b.(*value.Boolean)
		return ok && a.Value == b.Value
	case *value.Nil:
		_, ok := b.(*value.Nil)
		return ok
	case *value.Array:
		b, ok := b.(*value.Array)
		if !ok || len(a.Elements) != len(b.Elements) {
			return false
		}
		for i := range a.Elements {
			if !sameValue(a.Elements[i], b.Elements[i]) {
				return false
			}
		}
		return true
	case *value.Hash:
		b, ok := b.(*value.Hash)
		if !ok || len(a.Pairs) != len(b.Pairs) {
			return false
		}
		for k, p := range a.Pairs {
			q, ok := b.Pairs[k]
			if !ok || !sameValue(p.Key, q.Key) || !sameValue(p.Value, q.Value) {
				return false
			}
		}
		return true
	}
	return false
}

// validUTF8 reports whether every string in v, keys included, is valid
// UTF-8: json writes any other as U+FFFD, so only these come back equal.
func validUTF8(v value.Object) bool {
	switch v := v.(type) {
	case *value.String:
		return utf8.ValidString(v.Value)
	case *value.Array:
		for _, e := range v.Elements {
			if !validUTF8(e) {
				return false
			}
		}
	case *value.Hash:
		for _, p := range v.Pairs {
			if !validUTF8(p.Key) || !validUTF8(p.Value) {
				return false
			}
		}
	}
	return true
}

// roundTrip checks parse_json(json(v)) against v: equal when v's strings
// are valid UTF-8, and always the same JSON text again.
func roundTrip(t *testing.T, v value.Object) {
	t.Helper()
	ctx := context.Background()
	text := encode(t, ctx, v)
	s, ok := text.(*value.String)
	if !ok {
		t.Fatalf("json(%s) = %s", v.Inspect(), text.Inspect())
	}
	back := decodeText(t, ctx, s.Value)
	if isError(back) {
		t.Fatalf("parse_json(%s) = %s", s.Value, back.Inspect())
	}
	if validUTF8(v) && !sameValue(v, back) {
		t.Fatalf("parse_json(json(v)) = %s, v = %s", back.Inspect(), v.Inspect())
	}
	if !validUTF8(v) {
		// json sorts keys before writing bad bytes as U+FFFD, so the text
		// may come back in another order; what came back is valid UTF-8,
		// and round-trips exactly.
		if !validUTF8(back) {
			t.Fatalf("parse_json(%s) = %s: invalid UTF-8", s.Value, back.Inspect())
		}
		roundTrip(t, back)
		return
	}
	if again := encode(t, ctx, back); again.Inspect() != s.Value {
		t.Fatalf("json(parse_json(%s)) = %s", s.Value, again.Inspect())
	}
}

// Round trip: parse_json(json(v)) equals v for every value json accepts
// (whose strings are valid UTF-8; others come back with U+FFFD, as json
// wrote them).
func TestParseJSONRoundTripProperty(t *testing.T) {
	r := rand.New(rand.NewPCG(10, 1))
	for i := 0; i < 3000; i++ {
		roundTrip(t, genValue(r, 4, i%3 == 0))
	}
	// The deepest value json writes.
	var v value.Object = &value.Array{}
	for range value.MaxInspectDepth - 1 {
		v = &value.Array{Elements: []value.Object{v}}
	}
	roundTrip(t, v)
}

// goValue is what encoding/json (with UseNumber) decodes text to, as an
// Arv value for comparison, or ok false if it holds a number that is not
// an int64 (parse_json refuses those).
func goValue(x any) (value.Object, bool) {
	switch x := x.(type) {
	case nil:
		return NIL, true
	case bool:
		return &value.Boolean{Value: x}, true
	case string:
		return &value.String{Value: x}, true
	case json.Number:
		var n int64
		if _, err := fmt.Sscan(x.String(), &n); err != nil || fmt.Sprint(n) != x.String() {
			return nil, false
		}
		return &value.Integer{Value: n}, true
	case []any:
		els := make([]value.Object, len(x))
		for i, e := range x {
			v, ok := goValue(e)
			if !ok {
				return nil, false
			}
			els[i] = v
		}
		return &value.Array{Elements: els}, true
	case map[string]any:
		h := value.NewHashSize(len(x))
		for k, e := range x {
			v, ok := goValue(e)
			if !ok {
				return nil, false
			}
			key := &value.String{Value: k}
			h.Set(key.HashKey(), value.HashPair{Key: key, Value: v})
		}
		return h, true
	}
	return nil, false
}

// FuzzParseJSON: any text either parses, to what encoding/json reads it
// as and back through json to the same value, or is a runtime error that
// encoding/json agrees is not plain-integer JSON without duplicate keys.
// It never panics.
func FuzzParseJSON(f *testing.F) {
	for _, s := range []string{`{"a":[1,"b",true,null]}`, `"😀\u0000"`, `-0`, `1e3`, `1.5`, `[[[]]]`,
		`{"a":1,"a":2}`, "\"\xff\"", `9223372036854775808`, ` [ 1 , 2 ] `, `{"":{}}`, `"\/"`, `tru`, `[1,]`} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, text string) {
		got := decodeText(t, context.Background(), text)
		var x any
		var goErr error
		if !json.Valid([]byte(text)) {
			goErr = fmt.Errorf("not valid JSON")
		} else {
			d := json.NewDecoder(bytes.NewReader([]byte(text)))
			d.UseNumber()
			goErr = d.Decode(&x)
		}
		if e, ok := got.(*value.Error); ok {
			if e.ErrKind() != "runtime" || !strings.HasPrefix(e.Message, "parse_json: ") || len(e.Message) > 2048 {
				t.Fatalf("parse_json(%q): bad error %q (%s)", text, e.Message, e.ErrKind())
			}
			if goErr == nil {
				if _, isInt := goValue(x); isInt && !strings.Contains(e.Message, "duplicate key") &&
					!strings.Contains(e.Message, "not an integer") && !strings.Contains(e.Message, "out of the integer range") {
					t.Fatalf("parse_json(%q) = %s, but encoding/json reads it", text, e.Message)
				}
			}
			return
		}
		if goErr != nil {
			t.Fatalf("parse_json(%q) = %s, but encoding/json refuses it: %v", text, got.Inspect(), goErr)
		}
		want, ok := goValue(x)
		if ok && !sameValue(want, got) {
			t.Fatalf("parse_json(%q) = %s, encoding/json reads %s", text, got.Inspect(), want.Inspect())
		}
		roundTrip(t, got)
	})
}
