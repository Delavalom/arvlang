package evaluator

import (
	goruntime "runtime"
	"strings"
	"testing"
)

// A string read by readCapped keeps only its own bytes alive: an Arv
// string holds its whole buffer, and the slack in it is never charged.
// Growing a strings.Builder by its length used to triple the buffer, so a
// held 9 MiB result kept about 2.3 times its size alive.
func TestReadCappedKeepsOnlyItsResult(t *testing.T) {
	for _, n := range []int{100 << 10, 1 << 20, 7_800_000, 9 << 20} {
		src := strings.Repeat("a", n)
		var ms goruntime.MemStats
		goruntime.GC()
		goruntime.ReadMemStats(&ms)
		base := int64(ms.HeapAlloc)
		kept := make([]string, 4)
		used := heapDuring(func() {
			for i := range kept {
				s, err := readCapped(strings.NewReader(src), -1, nil)
				if err != nil || len(s) != n {
					t.Fatalf("readCapped of %d bytes: got %d bytes, %v", n, len(s), err)
				}
				kept[i] = s
			}
		})
		goruntime.GC()
		goruntime.ReadMemStats(&ms)
		live := int64(ms.HeapAlloc) - base
		if want := int64(4*n)*9/8 + 1<<20; live > want {
			t.Errorf("4 reads of %d bytes keep %d bytes alive, want at most %d", n, live, want)
		}
		if want := uint64(4*n)*5/2 + 1<<20; used > want {
			t.Errorf("4 reads of %d bytes allocated %d bytes, want at most %d", n, used, want)
		}
		goruntime.KeepAlive(kept)
	}
}
