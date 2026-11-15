package value

import "testing"

func str(s string) *String { return &String{Value: s} }

// I7: Phase 6 replays a run and compares its output byte for byte, so a
// hash must print the same every time: in the order its keys were set.
func TestHashInspectsInInsertionOrder(t *testing.T) {
	h := &Hash{}
	for _, k := range []string{"zeta", "alpha", "mid", "beta", "omega", "kappa"} {
		h.Set(str(k).HashKey(), HashPair{Key: str(k), Value: &Integer{Value: int64(len(k))}})
	}
	h.Set(str("alpha").HashKey(), HashPair{Key: str("alpha"), Value: &Integer{Value: 9}}) // keeps its place
	want := "{zeta: 4, alpha: 9, mid: 3, beta: 4, omega: 5, kappa: 5}"
	for i := 0; i < 100; i++ {
		if got := h.Inspect(); got != want {
			t.Fatalf("run %d: Inspect = %s, want %s", i, got, want)
		}
	}
}

func TestNewHashIsSorted(t *testing.T) {
	fields := map[string]Object{"status": &Integer{Value: 200}, "body": str("ok"), "a": &Nil{}, "z": &Nil{}}
	for i := 0; i < 100; i++ {
		if got := NewHash(fields).Inspect(); got != "{a: nil, body: ok, status: 200, z: nil}" {
			t.Fatalf("run %d: NewHash = %s", i, got)
		}
	}
}

// A hash whose Pairs were filled by hand has no recorded order; it still
// prints the same every time.
func TestHandBuiltHashIsStable(t *testing.T) {
	pairs := map[HashKey]HashPair{}
	for _, k := range []string{"c", "a", "b", "d", "e", "f", "g"} {
		pairs[str(k).HashKey()] = HashPair{Key: str(k), Value: str(k)}
	}
	h := &Hash{Pairs: pairs}
	first := h.Inspect()
	for i := 0; i < 100; i++ {
		if got := h.Inspect(); got != first {
			t.Fatalf("run %d: %s, then %s", i, first, got)
		}
	}
}
