//go:build unix

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

// A program holding fs_read(G) fills its scope's alloc_mb to an exact
// level with a string, then asks for a path through a name outside G.
// Canonicalising charged each lstat and symlink as it went, so whether
// the request failed with "budget exceeded" or "capability denied" told
// a missing name from a file, a directory or a symlink, and gave a
// symlink's target length and which of the target's components existed
// (review round 11). Now every name gives the same outcome at every
// padding, through read_file and through spawn ... with.
func TestDenialRevealsNothingOutsideGrants(t *testing.T) {
	mk := func() string {
		d, err := filepath.EvalSymlinks(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	g, o := mk(), mk()
	if err := os.WriteFile(o+"/fileA", []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(o+"/dirAA/y/deep/er", 0o755); err != nil {
		t.Fatal(err)
	}
	for name, target := range map[string]string{
		"linkA": "/x", "linkB": "/" + strings.Repeat("z", 201),
		"linkD": "/usr/bin/q", "linkE": "/usr/zzz/q",
	} {
		if err := os.Symlink(target, o+"/"+name); err != nil {
			t.Fatal(err)
		}
	}
	names := []string{"missA", "fileA", "dirAA", "linkA", "linkB", "linkD", "linkE"}
	caps := grant(t, runtime.FSRead, g)
	forms := map[string]string{
		"read_file": `read_file(%q)`,
		"spawn":     `await spawn fn() { 1 }() with [fs_read(%q)]`,
	}
	for form, call := range forms {
		outcome := func(name string, pad int) string {
			src := fmt.Sprintf(`scope (alloc_mb: 1) { let pad = %q + ""; `+call+` }`,
				strings.Repeat("p", pad), o+"/"+name+"/y/deep/er/f")
			got, _ := runWith(t, src, caps)
			e, ok := got.(*value.Error)
			switch {
			case !ok:
				return "value " + got.Inspect()
			case strings.Contains(e.Message, "capability denied"):
				return "denied"
			case strings.Contains(e.Message, "budget exceeded"):
				return "budget"
			}
			return e.Message
		}
		// The largest padding that still gives the denial for a missing name.
		lo, hi := 0, 1<<20
		if got := outcome("missA", lo); got != "denied" {
			t.Fatalf("%s: missA gives %s with no padding", form, got)
		}
		for hi-lo > 1 {
			if m := (lo + hi) / 2; outcome("missA", m) == "denied" {
				lo = m
			} else {
				hi = m
			}
		}
		for _, pad := range []int{lo, hi} {
			want := outcome("missA", pad)
			for _, name := range names[1:] {
				if got := outcome(name, pad); got != want {
					t.Errorf("%s, padding %d: %s gives %s, missA %s", form, pad, name, got, want)
				}
			}
		}
	}
}
