package evaluator

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// mkcaps builds, in Arv, n distinct fs capabilities named p, p + "b", ...
const mkcaps = `let mk = fn(k, p, n) { if (n == 0) { [] } else { push(mk(k, p + "b", n - 1), k(p)) } };`

// Every fs effect stats each fs grant of its task's lineage (confiningRoot)
// and checked each grant with a concatenation (Covers): one copy of every
// held scope per call, and one per check, none of it charged. A task
// holding 1000 grants of about 3000 bytes made 1000 read_file calls
// allocate 11 GiB under alloc_mb: 64, and the scope succeeded. Covers now
// compares in place, and the effect charges each stat it makes.
func TestFSEffectsChargeTheirGrants(t *testing.T) {
	for _, fn := range []string{`read_file(f)`, `write_file(f, "hi")`} {
		t.Run(fn, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "small"), []byte("hi"), 0o644); err != nil {
				t.Fatal(err)
			}
			kind := "fs_read"
			if strings.HasPrefix(fn, "write") {
				kind = "fs_write"
			}
			base := dir + "/x/" + strings.Repeat("a", 3000-len(dir))
			src := fmt.Sprintf(`%s let f = "%s/small"; let z = %s; scope (alloc_mb: 8) { `+
				`let cs = push(mk(%s, "%s", 300), %s("%s")); `+
				`let g = fn() { for i in z { %s }; "ok" }; await spawn g() with cs }`,
				mkcaps, dir, zeros(1000), kind, base, kind, dir, fn)
			var got value.Object
			used := heapDuring(func() {
				got, _ = runWith(t, src, grant(t, runtime.FSRead, dir), grant(t, runtime.FSWrite, dir))
			})
			// The effect, not the spawn, runs out: about 1 MB per call.
			// (The last match: t.TempDir names the subtest, so dir holds fn.)
			at := fmt.Sprintf("1:%d", strings.LastIndex(src, fn)+1)
			wantErrorAt(t, got, "budget exceeded: alloc (limit 8388608 bytes)", at)
			if used > 128<<20 {
				t.Errorf("allocated %d MiB under alloc_mb: 8", used>>20)
			}
		})
	}
}

// One grant costs nothing more than before: read_file of a file covered by
// the task's only grant makes no stat.
func TestFSEffectWithOneGrantIsNotCharged(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "small"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	src := fmt.Sprintf(`let f = "%s/small"; scope (alloc_mb: 1) { for i in %s { read_file(f) }; "ok" }`, dir, zeros(100))
	if got, _ := runWith(t, src, grant(t, runtime.FSRead, dir)); got.Inspect() != `ok` {
		t.Fatalf("got %.300s, want ok", got.Inspect())
	}
}
