package value

import (
	"bytes"
	"strings"
	"testing"
)

// valGen builds a value from fuzz bytes: integers, booleans, nil, short
// and long strings (some not UTF-8), arrays, hashes with integer, string
// and boolean keys, errors, and parts reused in several places, as a
// value an effect returns can share them. It never builds a cycle: a
// reused part is one already finished.
type valGen struct {
	data  []byte
	built []Object // finished arrays, hashes, errors and long strings
	nodes int
}

func (g *valGen) byte() byte {
	if len(g.data) == 0 {
		return 0
	}
	b := g.data[0]
	g.data = g.data[1:]
	return b
}

func (g *valGen) str() string {
	n := int(g.byte() % 40)
	var b strings.Builder
	for range n {
		c := g.byte()
		if c >= 0xf0 { // a run of a multibyte rune, or a lone bad byte
			b.WriteString("é✓")
		}
		b.WriteByte(c)
	}
	return b.String()
}

func (g *valGen) key() Object {
	switch g.byte() % 3 {
	case 0:
		return &Integer{Value: int64(int8(g.byte()))}
	case 1:
		return &Boolean{Value: g.byte()%2 == 0}
	}
	return g.keep(&String{Value: g.str()})
}

// keep notes o as reusable if the codec numbers it, and returns it.
func (g *valGen) keep(o Object) Object {
	if shareable(o) {
		g.built = append(g.built, o)
	}
	return o
}

func (g *valGen) value(depth int) Object {
	g.nodes++
	k := g.byte() % 10
	if depth > 6 || g.nodes > 300 {
		k %= 4 // leaves only
	}
	switch k {
	case 0:
		return &Nil{}
	case 1:
		return &Boolean{Value: g.byte()%2 == 1}
	case 2:
		v := int64(g.byte()) << 56
		for range 7 {
			v |= int64(g.byte()) << (8 * (g.byte() % 7))
		}
		return &Integer{Value: v}
	case 3:
		return g.keep(&String{Value: g.str()})
	case 4, 5:
		els := make([]Object, g.byte()%5)
		for i := range els {
			els[i] = g.value(depth + 1)
		}
		return g.keep(&Array{Elements: els})
	case 6:
		h := NewHashSize(0)
		for range g.byte() % 5 {
			k := g.key()
			if _, dup := h.Pairs[k.(Hashable).HashKey()]; dup {
				continue
			}
			h.Set(k.(Hashable).HashKey(), HashPair{Key: k, Value: g.value(depth + 1)})
		}
		return g.keep(h)
	case 7:
		return g.keep(&Error{Message: g.str(), Kind: g.str(), Pos: g.str(), Task: g.str()})
	}
	if len(g.built) == 0 {
		return &Nil{}
	}
	return g.built[int(g.byte())%len(g.built)]
}

// sharing walks a and b side by side and fails if their numbered parts
// are not shared alike: each part of a maps to one part of b, and back.
func sharing(t *testing.T, a, b Object, ab, ba map[Object]Object) {
	if shareable(a) {
		if m, ok := ab[a]; ok {
			if m != b {
				t.Fatalf("a part of the value, used twice, decoded as two parts: %s", a.Inspect())
			}
			return
		}
		if m, ok := ba[b]; ok && m != a {
			t.Fatalf("two parts of the value decoded as one: %s", b.Inspect())
		}
		ab[a], ba[b] = b, a
	}
	switch a := a.(type) {
	case *Array:
		for i := range a.Elements {
			sharing(t, a.Elements[i], b.(*Array).Elements[i], ab, ba)
		}
	case *Hash:
		bh := b.(*Hash)
		for _, k := range a.Keys() {
			sharing(t, a.Pairs[k].Key, bh.Pairs[k].Key, ab, ba)
			sharing(t, a.Pairs[k].Value, bh.Pairs[k].Value, ab, ba)
		}
	}
}

// FuzzCodecRoundTrip (I7: a replayed effect returns exactly what the
// recorded one did): any value an effect can return encodes, decodes to
// the same value with the same parts shared, and encodes again to the
// same bytes. A value nested past MaxCodecDepth is refused by Encode,
// never written for Decode to refuse.
func FuzzCodecRoundTrip(f *testing.F) {
	f.Add([]byte{4, 3, 5, 3, 20, 'a', 'b', 0xff, 9, 6, 2, 1, 4, 8, 8, 7, 1, 'x', 0, 0, 0, 0})
	f.Add([]byte{5, 4, 3, 30, 'l', 'o', 'n', 'g', 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 8, 0, 8, 1})
	f.Add([]byte{6, 4, 2, 2, 0xff, 0x80, 3, 3, 3, 3, 3, 1, 2, 7, 3, 1, 0xf1, 2, 'p', 'q', 0, 0, 8, 5})
	f.Add([]byte{0, 0xfe, 0x27, 0x10, 4, 1, 5})
	f.Fuzz(func(t *testing.T, data []byte) {
		var v Object
		deep := len(data) >= 3 && data[0] == 0 && data[1] == 0xfe
		if deep && data[0] == 0 && data[1] == 0xfe {
			// Deep: one array or hash chain around MaxCodecDepth.
			n := int(data[2])<<8 | int(data[3%len(data)])
			n = MaxCodecDepth - 20 + n%40
			v = &Integer{Value: 1}
			for i := range n {
				if len(data) > 4 && data[4+i%(len(data)-4)]%3 == 0 {
					h := NewHashSize(1)
					h.Set((&Integer{Value: 1}).HashKey(), HashPair{Key: &Integer{Value: 1}, Value: v})
					v = h
				} else {
					v = &Array{Elements: []Object{v}}
				}
			}
		} else {
			g := &valGen{data: data}
			v = g.value(0)
		}
		enc, err := Encode(v)
		if err != nil {
			if !strings.Contains(err.Error(), "deeper") {
				t.Fatalf("Encode(%.200s) = %v", v.Inspect(), err)
			}
			return
		}
		got, err := Decode(enc)
		if err != nil {
			t.Fatalf("Decode(Encode(v)) = %v for %.300s", err, enc)
		}
		if !same(got, v) {
			t.Fatalf("round trip of %.300s gave %.300s", v.Inspect(), got.Inspect())
		}
		sharing(t, v, got, map[Object]Object{}, map[Object]Object{})
		if again, err := Encode(got); err != nil || !bytes.Equal(again, enc) {
			t.Fatalf("re-encoding gave %.300s, %v; want %.300s", again, err, enc)
		}
		if deep {
			return // EncodeRequest nests its arguments one level down
		}
		if req := EncodeRequest([]Object{v}); !bytes.Equal(req, append(append([]byte{'['}, enc...), ']')) {
			t.Fatalf("EncodeRequest differs from Encode: %.300s vs %.300s", req, enc)
		}
	})
}
