package value

import (
	"errors"
	"testing"
)

// shared returns an array holding the same sub-array twice, n levels deep:
// it prints as 5 * 2^n - 4 bytes.
func shared(n int) Object {
	var o Object = &Integer{Value: 1}
	for i := 0; i < n; i++ {
		o = &Array{Elements: []Object{o, o}}
	}
	return o
}

// InspectChecked reports the text's growth in InspectChunk steps, matches
// Inspect when check passes, and stops at check's first error.
func TestInspectChecked(t *testing.T) {
	o := shared(16)
	var total int64
	calls := 0
	got, err := InspectChecked(o, func(n int64) error {
		if n < InspectChunk {
			t.Fatalf("checked after %d bytes, want at least %d", n, InspectChunk)
		}
		total += n
		calls++
		return nil
	})
	if err != nil || got != o.Inspect() {
		t.Fatalf("got %d bytes, err %v; want Inspect's %d bytes", len(got), err, len(o.Inspect()))
	}
	if want := len(got) / InspectChunk; calls != want || total > int64(len(got)) {
		t.Fatalf("%d checks for %d bytes, want %d", calls, total, want)
	}

	stop := errors.New("over budget")
	calls = 0
	got, err = InspectChecked(shared(40), func(int64) error { calls++; return stop })
	if err != stop || got != "" || calls != 1 {
		t.Fatalf("got %q, err %v after %d checks; want the check's error after one", got, err, calls)
	}

	if got, err := InspectChecked(shared(3), func(int64) error { return stop }); err != nil || got != shared(3).Inspect() {
		t.Fatalf("small value: got %q, err %v", got, err)
	}
}
