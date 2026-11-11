package value

import (
	"bytes"
	"math"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
)

func hashOf(pairs ...Object) *Hash {
	h := NewHashSize(len(pairs) / 2)
	for i := 0; i < len(pairs); i += 2 {
		h.Set(pairs[i].(Hashable).HashKey(), HashPair{Key: pairs[i], Value: pairs[i+1]})
	}
	return h
}

// same reports whether a and b are the same value, field by field: Inspect
// alone would not tell an error's kind or a hash's key types apart.
func same(a, b Object) bool {
	if a.Type() != b.Type() {
		return false
	}
	switch a := a.(type) {
	case *Array:
		b := b.(*Array)
		if len(a.Elements) != len(b.Elements) {
			return false
		}
		for i := range a.Elements {
			if !same(a.Elements[i], b.Elements[i]) {
				return false
			}
		}
		return true
	case *Hash:
		b := b.(*Hash)
		ak, bk := a.Keys(), b.Keys()
		if len(ak) != len(bk) {
			return false
		}
		for i := range ak {
			if ak[i] != bk[i] || !same(a.Pairs[ak[i]].Key, b.Pairs[bk[i]].Key) || !same(a.Pairs[ak[i]].Value, b.Pairs[bk[i]].Value) {
				return false
			}
		}
		return true
	case *Error:
		return *a == *b.(*Error)
	}
	return a.Inspect() == b.Inspect()
}

// Every encodable type comes back equal, byte for byte (I7: a replayed
// effect returns exactly what the recorded one did).
func TestCodecRoundTrip(t *testing.T) {
	for _, v := range []Object{
		&Nil{}, &Boolean{Value: true}, &Boolean{Value: false},
		&Integer{Value: 0}, &Integer{Value: -42}, &Integer{Value: math.MaxInt64}, &Integer{Value: math.MinInt64},
		&String{Value: ""}, &String{Value: "plain"}, &String{Value: "quote \" back \\ nl \n tab \t nul \x00 bell \x07"},
		&String{Value: "ünïcödé ✓   <&>"}, &String{Value: "bad utf-8 \xff\xfe end"},
		&Array{}, &Array{Elements: []Object{&Integer{Value: 1}, &String{Value: "a"}, &Array{Elements: []Object{&Nil{}}}}},
		hashOf(), hashOf(&String{Value: "status"}, &Integer{Value: 200}, &String{Value: "body"}, &String{Value: "x\xff"}),
		hashOf(&Integer{Value: 1}, &Boolean{Value: true}, &Boolean{Value: false}, hashOf(&String{Value: "k"}, &Nil{})),
		&Error{Message: "boom \xff", Kind: runtime.KindEffect, Pos: "3:7", Task: "root/0#2"},
		&Error{Message: "no kind"},
	} {
		b, err := Encode(v)
		if err != nil {
			t.Fatalf("Encode(%s): %v", v.Inspect(), err)
		}
		got, err := Decode(b)
		if err != nil {
			t.Fatalf("Decode(%s): %v", b, err)
		}
		if !same(got, v) {
			t.Errorf("round trip of %s gave %s (via %s)", v.Inspect(), got.Inspect(), b)
		}
		if again, _ := Encode(got); !bytes.Equal(again, b) {
			t.Errorf("re-encoding %s gave %s", b, again)
		}
	}
}

func TestCodecFormat(t *testing.T) {
	for _, tt := range []struct {
		v    Object
		want string
	}{
		{&Integer{Value: -7}, `-7`},
		{&String{Value: "a\"b"}, `"a\"b"`},
		{&String{Value: "\xff"}, `{"bytes":"/w=="}`},
		{hashOf(&String{Value: "b"}, &Integer{Value: 1}, &String{Value: "a"}, &Nil{}), `{"hash":[["b",1],["a",null]]}`},
		{&Error{Message: "m", Kind: "user", Pos: "1:2", Task: "root"}, `{"error":["m","user","1:2","root"]}`},
	} {
		if b, err := Encode(tt.v); err != nil || string(b) != tt.want {
			t.Errorf("Encode(%s) = %s, %v; want %s", tt.v.Inspect(), b, err, tt.want)
		}
	}
}

func TestCodecRefusesWhatEffectsNeverReturn(t *testing.T) {
	for _, v := range []Object{
		&Function{}, &Builtin{}, &Capability{}, NewTaskHandle("root/0", nil), &ReturnValue{Value: &Nil{}},
		&Array{Elements: []Object{&Function{}}}, hashOf(&String{Value: "f"}, &Builtin{}), nil,
	} {
		if b, err := Encode(v); err == nil {
			t.Errorf("Encode(%T) = %s, want an error", v, b)
		}
	}
}

// A value nested as deeply as Encode allows decodes; one level more is
// refused by Encode, never written for Decode to choke on.
func TestCodecDepth(t *testing.T) {
	nest := func(n int) Object {
		var v Object = &Integer{Value: 1}
		for i := 0; i < n; i++ {
			v = &Array{Elements: []Object{v}}
		}
		return v
	}
	b, err := Encode(nest(MaxCodecDepth - 1))
	if err != nil {
		t.Fatalf("Encode at the limit: %v", err)
	}
	if _, err := Decode(b); err != nil {
		t.Fatalf("Decode at the limit: %v", err)
	}
	if _, err := Encode(nest(MaxCodecDepth)); err == nil || !strings.Contains(err.Error(), "deeper") {
		t.Fatalf("Encode past the limit = %v", err)
	}
	var h Object = &Nil{}
	for i := 0; i < MaxCodecDepth/3; i++ {
		h = hashOf(&Integer{Value: 1}, h)
	}
	if b, err := Encode(h); err == nil {
		if _, err := Decode(b); err != nil {
			t.Fatalf("Encode wrote a hash Decode refuses: %v", err)
		}
	}
}

func TestDecodeRejects(t *testing.T) {
	for _, in := range []string{
		``, `1.5`, `1e3`, `99999999999999999999`, `{}`, `{"bytes":"%%"}`, `{"bytes":1}`,
		`{"hash":[[1]]}`, `{"hash":[[[1],2]]}`, `{"hash":[["a",1],["a",2]]}`, `{"hash":{}}`,
		`{"error":["a","b","c"]}`, `{"error":[1,"b","c","d"]}`, `{"nope":1}`, `{"bytes":"","hash":[]}`,
		`1 2`, `[1,`, `"\ud800"x`, `{"type":"FUNCTION"}`,
	} {
		if v, err := Decode([]byte(in)); err == nil {
			t.Errorf("Decode(%s) = %s, want an error", in, v.Inspect())
		}
	}
}

func TestEncodeRequestIsLoose(t *testing.T) {
	got := EncodeRequest([]Object{&String{Value: "u"}, &Function{}, &Integer{Value: 3}, nil})
	if want := `["u",{"type":"FUNCTION"},3,{"type":"NONE"}]`; string(got) != want {
		t.Fatalf("EncodeRequest = %s, want %s", got, want)
	}
	if got := EncodeRequest(nil); string(got) != `[]` {
		t.Fatalf("EncodeRequest(nil) = %s", got)
	}
}

// FuzzDecode: Decode never panics, and whatever it accepts encodes and
// decodes again to the same bytes.
func FuzzDecode(f *testing.F) {
	for _, s := range []string{`null`, `[1,"a",{"bytes":"/w=="}]`, `{"hash":[["k",[true]]]}`, `{"error":["m","user","1:1","root"]}`, `-0`, `"\u0000"`} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		v, err := Decode(b)
		if err != nil {
			return
		}
		enc, err := Encode(v)
		if err != nil {
			t.Fatalf("Decode(%q) gave %s, which Encode refuses: %v", b, v.Inspect(), err)
		}
		v2, err := Decode(enc)
		if err != nil {
			t.Fatalf("Decode(Encode(...)) = %v for %s", err, enc)
		}
		if enc2, _ := Encode(v2); !bytes.Equal(enc, enc2) {
			t.Fatalf("not stable: %s then %s", enc, enc2)
		}
	})
}
