package check

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// FuzzCheck: arv check reads untrusted programs. It must never panic, must
// give the same report every time, and must print every grant as one line.
func FuzzCheck(f *testing.F) {
	for _, dir := range []string{"../../examples", "../../e2e/testdata"} {
		paths, _ := filepath.Glob(filepath.Join(dir, "*.arv"))
		for _, p := range paths {
			if src, err := os.ReadFile(p); err == nil {
				f.Add(string(src))
			}
		}
	}
	f.Add(`let u = "http://a" + "/"; http_get(u); spawn f() with [net("*"), fs_read("x")];`)
	f.Add("// arv:allow env=A\ngetenv(\"A\\n\");")
	f.Fuzz(func(t *testing.T, src string) {
		r, err := Source(src)
		if _, merr := Manifest(src); merr != nil && !strings.Contains(merr.Error(), "arv:allow wants") {
			t.Fatalf("Manifest: unexpected error %v", merr)
		}
		if err != nil {
			if _, ok := err.(*ParseError); !ok {
				t.Fatalf("Source: %v (not a parse error)", err)
			}
			return
		}
		again, err := Source(src)
		if err != nil || !reflect.DeepEqual(r, again) {
			t.Fatalf("Source is not deterministic:\n%+v\n%+v", r, again)
		}
		for i, n := range r.Needs {
			if q := n.Quoted(); strings.ContainsAny(q, "\n\r") {
				t.Fatalf("need %q prints as more than one line: %q", n.String(), q)
			}
			if i > 0 && rank(r.Needs[i-1].Kind) == rank(n.Kind) && r.Needs[i-1].Scope >= n.Scope {
				t.Fatalf("needs not sorted and distinct: %v", r.Needs)
			}
		}
		if len(r.Needs) > len(r.Sites) {
			t.Fatalf("%d needs from %d sites", len(r.Needs), len(r.Sites))
		}
	})
}
