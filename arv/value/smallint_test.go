package value

import "testing"

// Int shares the integers from smallIntMin to smallIntMax and makes a new
// one for any other value; each has the value asked for.
func TestIntSharesSmallIntegers(t *testing.T) {
	for _, n := range []int64{smallIntMin, -1, 0, 1, 7, smallIntMax} {
		if a, b := Int(n), Int(n); a != b || a.Value != n {
			t.Errorf("Int(%d): %p %p (%d), want one shared value", n, a, b, a.Value)
		}
	}
	for _, n := range []int64{smallIntMin - 1, smallIntMax + 1, -1 << 63, 1<<63 - 1} {
		if a, b := Int(n), Int(n); a == b || a.Value != n || b.Value != n {
			t.Errorf("Int(%d): %p %p, want two new values", n, a, b)
		}
	}
}

// Nothing tells a shared integer from a new one: the codec writes both
// the same (it never numbers integers), and both have the same hash key.
func TestSharedIntegersAreIndistinguishable(t *testing.T) {
	shared, fresh := Int(5), &Integer{Value: 5}
	a, err := Encode(&Array{Elements: []Object{shared, shared}})
	if err != nil {
		t.Fatal(err)
	}
	b, err := Encode(&Array{Elements: []Object{fresh, &Integer{Value: 5}}})
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) || string(a) != "[5,5]" {
		t.Errorf("encodings %s and %s, want [5,5] for both", a, b)
	}
	if shared.HashKey() != fresh.HashKey() {
		t.Error("hash keys differ")
	}
}
