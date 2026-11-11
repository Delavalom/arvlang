package runtime

import (
	"fmt"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"
)

// heapDuring is how many bytes f allocated on the heap.
func heapDuring(f func()) uint64 {
	var before, after goruntime.MemStats
	goruntime.GC()
	goruntime.ReadMemStats(&before)
	f()
	goruntime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// Canonical builds its result in one buffer. It used filepath.Join for
// every component, a new string each time, so a 4000-byte path of "/a"
// components below a missing one allocated about 4 MB (quadratic), and
// spawn ran it for every request, uncharged.
func TestCanonicalIsLinear(t *testing.T) {
	dir := t.TempDir()
	long := dir + "/x" + strings.Repeat("/a", (4000-len(dir))/2)
	want := filepath.Clean(long)
	if canon, err := Canonical(dir); err == nil {
		want = canon + strings.TrimPrefix(want, dir)
	}
	var got string
	var err error
	used := heapDuring(func() { got, err = Canonical(long) })
	if err != nil || got != want {
		t.Fatalf("Canonical = %q, %v; want %q", got, err, want)
	}
	if used > 64<<10 {
		t.Errorf("Canonical of a %d-byte path allocated %d bytes, want under 64 KiB", len(long), used)
	}
}

// A denial lists at most maxHeldShown grants and counts the rest, so a
// task holding many grants does not build a huge, uncharged error.
func TestDeniedErrorListsFewGrants(t *testing.T) {
	held := make([]Capability, 1000)
	for i := range held {
		held[i] = Capability{Kind: FSRead, Scope: "/" + strings.Repeat("d", 4000) + fmt.Sprint(i)}
	}
	msg := (&DeniedError{Want: Capability{Kind: Env, Scope: "X"}, Held: held}).Error()
	if n := len(msg); n > (maxHeldShown+1)*4100 {
		t.Errorf("denial is %d bytes long", n)
	}
	if !strings.HasSuffix(msg, fmt.Sprintf(", ... %d more])", 1000-maxHeldShown)) {
		t.Errorf("denial does not count the grants it leaves out: ...%s", msg[len(msg)-40:])
	}
	short := (&DeniedError{Want: Capability{Kind: Env, Scope: "X"}, Held: held[:2]}).Error()
	if strings.Contains(short, "more") {
		t.Errorf("a short list is cut: %s", short[len(short)-40:])
	}
}

// Attenuate keeps one copy of each distinct grant, and calls each with
// every request's canonical form, before the next request, so the caller
// can charge it and stop early.
func TestAttenuateEachDedupesAndCanStop(t *testing.T) {
	dir := t.TempDir()
	parent := NewCapSet(mustCap(t, FSRead, dir))
	c := Capability{Kind: FSRead, Scope: dir + "/x"}
	reqs := []Capability{c, c, c, {Kind: FSRead, Scope: dir + "/y"}}
	fetched := 0
	req := func(i int) Capability { fetched++; return reqs[i] }
	calls := 0
	child, err := parent.AttenuateEach(len(reqs), req, func(Capability) error {
		calls++
		return nil
	})
	if err != nil || calls != 4 || len(child.List()) != 2 {
		t.Fatalf("got %v, %d calls, %d grants; want 4 calls, 2 grants", err, calls, len(child.List()))
	}
	// It takes each request only when it gets to it, so a caller need
	// not copy them all before the first charge.
	stop := fmt.Errorf("stop")
	calls, fetched = 0, 0
	_, err = parent.AttenuateEach(3, req, func(Capability) error {
		calls++
		return stop
	})
	if err != stop || calls != 1 || fetched != 1 {
		t.Fatalf("got %v after %d calls and %d requests taken, want stop after 1 of each", err, calls, fetched)
	}
}
