package value

import (
	"strings"
	"testing"
)

// dag is an array holding the same array twice, n levels deep: n nodes,
// 2^n paths, around a string leaf.
func dag(n int) *Array {
	a := &Array{Elements: []Object{&String{Value: strings.Repeat("x", 64)}}}
	for range n {
		a = &Array{Elements: []Object{a, a}}
	}
	return a
}

// A shared value is written once and then referred to, so the encoding
// grows with the nodes, not the paths (review round 3: 2^22 paths made a
// 298 MB log line). EncodeRequest and Encode both.
func TestCodecSharesRepeatedValues(t *testing.T) {
	d := dag(60)
	b, err := Encode(d)
	if err != nil || len(b) > 2000 {
		t.Fatalf("Encode of a 60-level DAG: %d bytes, %v", len(b), err)
	}
	if r := EncodeRequest([]Object{d, d}); len(r) > 2000 {
		t.Fatalf("EncodeRequest of a 60-level DAG: %d bytes", len(r))
	}
	got, err := Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	// The shape survives: the decoded value is shared as the original
	// was, so it encodes to the same bytes, and its two halves are one.
	a := got.(*Array)
	if a.Elements[0] != a.Elements[1] {
		t.Fatal("decoding lost the sharing")
	}
	if again, _ := Encode(got); string(again) != string(b) {
		t.Fatalf("re-encoded %s, want %s", again, b)
	}
	// A short string is written out each time; a long one once.
	short, long := &String{Value: "abc"}, &String{Value: strings.Repeat("y", RefMinLen+1)}
	b, _ = Encode(&Array{Elements: []Object{short, short, long, long}})
	want := `["abc","abc","` + strings.Repeat("y", RefMinLen+1) + `",{"ref":1}]`
	if string(b) != want {
		t.Fatalf("got %s, want %s", b, want)
	}
	// Errors and hashes are shared too, and a shared long key.
	e := &Error{Message: "m", Kind: "k"}
	h := NewHash(map[string]Object{"k": e})
	b, _ = Encode(&Array{Elements: []Object{h, h, e}})
	if string(b) != `[{"hash":[["k",{"error":["m","k","",""]}]]},{"ref":1},{"ref":2}]` {
		t.Fatalf("got %s", b)
	}
	if v, err := Decode(b); err != nil || v.(*Array).Elements[2] != v.(*Array).Elements[0].(*Hash).Pairs[(&String{Value: "k"}).HashKey()].Value {
		t.Fatalf("Decode = %v, %v: the error is not shared", v, err)
	}
}

// A reference must name a value already read in full: never itself, an
// enclosing value, a later one, or one that is not numbered.
func TestDecodeRejectsBadRefs(t *testing.T) {
	for _, in := range []string{
		`{"ref":0}`, `[{"ref":0}]`, `[[1],{"ref":2}]`, `[{"ref":-1}]`, `[{"ref":"0"}]`,
		`[{"ref":1.5}]`, `["short",{"ref":1}]`, `{"hash":[[{"ref":0},1]]}`, `[[1],{"ref":1,"x":2}]`,
		`[{"ref":99999999999999999999}]`,
	} {
		if v, err := Decode([]byte(in)); err == nil {
			t.Errorf("Decode(%s) = %s, want an error", in, v.Inspect())
		}
	}
	if v, err := Decode([]byte(`[[1],{"ref":1}]`)); err != nil || v.Inspect() != "[[1], [1]]" {
		t.Errorf("Decode of a good ref = %v, %v", v, err)
	}
}
