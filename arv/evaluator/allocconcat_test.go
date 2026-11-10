package evaluator

import (
	"fmt"
	goruntime "runtime"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/value"
)

// A concatenation is charged before its string is built, so tasks that
// wake together cannot each build up to twice the limit (or a free
// literal's size) before the first failed charge cancels the scope. 500
// tasks used to allocate about 274 MiB under alloc_mb: 1.
func TestConcurrentConcatStopsAtAllocLimit(t *testing.T) {
	a := strings.Repeat("a", 150000)
	big := strings.Repeat("a", 1000000)
	for _, tt := range []struct{ name, src string }{
		{"charged operands", fmt.Sprintf(`scope (alloc_mb: 1) { let a = %q; let s = a + a; let f = fn() { sleep(300); s + s }; for i in %s { spawn f() with [] } }`, a, zeros(500))},
		{"free literal", fmt.Sprintf(`let s = %q; let f = fn() { sleep(300); s + s }; scope (alloc_mb: 1) { for i in %s { spawn f() with [] } }`, big, zeros(500))},
	} {
		t.Run(tt.name, func(t *testing.T) {
			before := goruntime.NumGoroutine()
			var got value.Object
			used := heapDuring(func() { got, _ = runWith(t, tt.src) })
			wantBareAllocError(t, got)
			settled(t, before)
			if used > 32<<20 {
				t.Errorf("500 concurrent concatenations under alloc_mb: 1 allocated %d MiB, want under 32 MiB", used>>20)
			}
		})
	}
}

// A concatenation over the limit fails before it builds anything, even
// with one task: s + s below would build 2 MB from a free literal.
func TestConcatOverLimitBuildsNothing(t *testing.T) {
	src := fmt.Sprintf(`let s = %q; scope (alloc_mb: 1) { len(s + s) }`, strings.Repeat("a", 1000000))
	var got value.Object
	used := heapDuring(func() { got, _ = runWith(t, src) })
	wantBareAllocError(t, got)
	if used > 1<<20 {
		t.Errorf("a refused concatenation allocated %d bytes, want under 1 MiB", used)
	}
}
