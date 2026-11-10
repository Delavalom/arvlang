package runtime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fuzzTree builds a small tree with every kind of entry Canonical and the
// fs grants must handle, and returns its canonical root (on macOS the temp
// dir sits behind the /var -> /private/var symlink):
//
//	g/a/f          file
//	g/b/           empty dir
//	g/file         file
//	g/in    -> ROOT/g/a     absolute link inside g
//	g/up    -> ../g/b       relative link inside g
//	g/esc   -> ROOT/out     absolute link out of g
//	g/rel   -> ../out       relative link out of g
//	g/dang  -> ROOT/nowhere/x  dangling
//	g/loop  -> loop         self loop
//	g/chain -> in           link to a link
//	out/s          file
//	out/back -> ../g        link from out into g
func fuzzTree(tb testing.TB, dir string) string {
	tb.Helper()
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		tb.Fatal(err)
	}
	for _, d := range []string{"g/a", "g/b", "out"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			tb.Fatal(err)
		}
	}
	for _, f := range []string{"g/a/f", "g/file", "out/s"} {
		if err := os.WriteFile(filepath.Join(root, f), []byte(f), 0o644); err != nil {
			tb.Fatal(err)
		}
	}
	links := map[string]string{
		"g/in":     filepath.Join(root, "g/a"),
		"g/up":     "../g/b",
		"g/esc":    filepath.Join(root, "out"),
		"g/rel":    "../out",
		"g/dang":   filepath.Join(root, "nowhere/x"),
		"g/loop":   "loop",
		"g/chain":  "in",
		"out/back": "../g",
	}
	for link, target := range links {
		if err := os.Symlink(target, filepath.Join(root, link)); err != nil {
			tb.Fatal(err)
		}
	}
	return root
}

// fuzzPath joins a fuzzed relative path under root. It keeps the raw
// spelling (no cleaning), so "..", "." and repeated slashes reach Canonical.
func fuzzPath(root, s string) string {
	return root + string(filepath.Separator) + s
}

// under reports whether p is root or inside it.
func under(p, root string) bool {
	return p == root || strings.HasPrefix(p, root+string(filepath.Separator))
}

// pathSeeds are fuzz seeds that walk every entry of fuzzTree.
var pathSeeds = []string{
	"g/a/f", "g/in/f", "g/chain/f", "g/up", "g/esc/s", "g/rel/s", "g/dang", "g/dang/y",
	"g/loop", "g/loop/x", "g/file", "g/file/x", "g/file/..", "g/missing/../a", "g/in/../b",
	"g/rel/../g/a/f", "out/back/a/f", "g/./a//f", "g/a/f/", "../x", "g/esc/back/esc/s", "",
}
