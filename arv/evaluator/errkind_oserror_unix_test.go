//go:build unix

package evaluator

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
)

// An OS error while canonicalising a path inside a grant (a symlink loop,
// a directory the process may not search) is a failure in the world, kind
// effect, both for spawn ... with and for an effect's own request; a path
// that leaves the grant is denied, kind capability.
func TestOSErrorInsideGrantKinds(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	g := filepath.Join(root, "g")
	for _, d := range []string{filepath.Join(g, "locked"), filepath.Join(root, "out")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, l := range [][2]string{{"loop2", "loop1"}, {"loop1", "loop2"}} {
		if err := os.Symlink(l[0], filepath.Join(g, l[1])); err != nil {
			t.Fatal(err)
		}
	}
	rows := []struct{ name, path, kind, has string }{
		{"symlink loop", filepath.Join(g, "loop1", "x"), runtime.KindEffect, "too many levels of symbolic links"},
		{"outside the grant", filepath.Join(root, "out", "x"), runtime.KindCapability, "capability denied"},
	}
	if os.Geteuid() != 0 {
		chmodTemp(t, filepath.Join(g, "locked"), 0)
		rows = append(rows, struct{ name, path, kind, has string }{
			"unsearchable directory", filepath.Join(g, "locked", "x"), runtime.KindEffect, "permission denied"})
	}
	rd := grant(t, runtime.FSRead, g)
	for _, tt := range rows {
		for _, form := range []struct{ name, src string }{
			{"spawn with", `let f = fn() { 1 }; spawn f() with [fs_read(%q)]`},
			{"read_file", `read_file(%q)`},
		} {
			t.Run(tt.name+"/"+form.name, func(t *testing.T) {
				got, _ := runWith(t, fmt.Sprintf(form.src, tt.path), rd)
				if e := wantKind(t, got, tt.kind); !strings.Contains(e.Message, tt.has) {
					t.Fatalf("got %q, want it to contain %q", e.Message, tt.has)
				}
			})
		}
	}
}
