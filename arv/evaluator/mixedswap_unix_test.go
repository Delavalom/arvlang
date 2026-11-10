//go:build unix

package evaluator

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// mixedTree builds root/g/{sub,other}/f.txt and .../deep/f.txt ("inside"
// under sub, "other" under other) and the same files under root/out
// ("SECRET").
func mixedTree(t *testing.T) (granted, outside string) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir()) // canonical, as beforeStep sees it
	if err != nil {
		t.Fatal(err)
	}
	granted, outside = filepath.Join(root, "g"), filepath.Join(root, "out")
	for dir, text := range map[string]string{
		filepath.Join(granted, "sub"):   "inside",
		filepath.Join(granted, "other"): "other",
		outside:                         "SECRET",
	} {
		for _, d := range []string{dir, filepath.Join(dir, "deep")} {
			mustMkdir(t, d)
			if err := os.WriteFile(filepath.Join(d, "f.txt"), []byte(text), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	return granted, outside
}

// wantUntouched fails if dir/f.txt, dir/deep/f.txt no longer hold text or
// a new.txt appeared beside either.
func wantUntouched(t *testing.T, dir, text string) {
	t.Helper()
	for _, d := range []string{dir, filepath.Join(dir, "deep")} {
		if data, _ := os.ReadFile(filepath.Join(d, "f.txt")); string(data) != text {
			t.Errorf("%s/f.txt = %q, want %q untouched", d, data, text)
		}
		if _, err := os.Lstat(filepath.Join(d, "new.txt")); err == nil {
			t.Errorf("write_file created %s/new.txt", d)
		}
	}
}

func wantNoLeak(t *testing.T, got value.Object, fn string) {
	t.Helper()
	if s := got.Inspect(); s == "SECRET" || s == "other" {
		t.Fatalf("%s returned %q from outside its grant", fn, s)
	}
	wantErrorHas(t, got, fn+": ")
}

type nestCase struct {
	name         string
	outer, inner runtime.Kind
	innerRel     string // the inner grant, relative to the outer one
	call, path   string // the call (%q is the path) and its path, relative to the outer grant
}

var mixedCases = []nestCase{
	{"read under write, dir", runtime.FSWrite, runtime.FSRead, "sub", `read_file(%q)`, "sub/f.txt"},
	{"read under write, deep", runtime.FSWrite, runtime.FSRead, "sub/deep", `read_file(%q)`, "sub/deep/f.txt"},
	{"read under write, file", runtime.FSWrite, runtime.FSRead, "sub/f.txt", `read_file(%q)`, "sub/f.txt"},
	{"write under read, dir", runtime.FSRead, runtime.FSWrite, "sub", `write_file(%q, "pwned")`, "sub/new.txt"},
	{"write under read, deep", runtime.FSRead, runtime.FSWrite, "sub/deep", `write_file(%q, "pwned")`, "sub/deep/new.txt"},
	{"write under read, file", runtime.FSRead, runtime.FSWrite, "sub/f.txt", `write_file(%q, "pwned")`, "sub/f.txt"},
}

func fnOf(c nestCase) string {
	if c.inner == runtime.FSRead {
		return "read_file"
	}
	return "write_file"
}

// I3 with nested grants of different kinds: the inner grant's directory
// lies inside the outer grant, where a writer can swap a directory on the
// way to it for a symlink, to outside every grant or to a part of the
// outer grant the inner kind does not cover. Neither swap may redirect the
// open (review round 2).
func TestMixedKindNestedGrantSwapCannotEscape(t *testing.T) {
	for _, c := range mixedCases {
		for _, to := range []string{"outside", "other"} {
			t.Run(c.name+" to "+to, func(t *testing.T) {
				granted, outside := mixedTree(t)
				target := "other" // relative: stays inside the outer grant
				if to == "outside" {
					target = outside
				}
				swapBeforeOpen(t, filepath.Join(granted, "sub"), target)
				got, _ := runWith(t, fmt.Sprintf(c.call, filepath.Join(granted, c.path)),
					grant(t, c.outer, granted), grant(t, c.inner, filepath.Join(granted, c.innerRel)))
				wantNoLeak(t, got, fnOf(c))
				wantUntouched(t, outside, "SECRET")
				wantUntouched(t, filepath.Join(granted, "other"), "other")
			})
		}
	}
}

// The same swap made after a component was checked and before it is
// opened: the opened directory (or leaf) is not the one checked.
func TestMixedKindSwapBetweenCheckAndStep(t *testing.T) {
	for _, c := range mixedCases {
		t.Run(c.name, func(t *testing.T) {
			granted, outside := mixedTree(t)
			swapped := filepath.Join(granted, "sub")
			target := "other"
			if c.innerRel == "sub/f.txt" { // the step that matters is the leaf
				swapped, target = filepath.Join(granted, c.innerRel), "deep/f.txt"
			}
			beforeStep = func(path string) {
				if path != swapped {
					return
				}
				beforeStep = nil
				if err := os.Rename(path, path+".orig"); err != nil {
					t.Error(err)
				}
				if err := os.Symlink(target, path); err != nil {
					t.Error(err)
				}
			}
			t.Cleanup(func() { beforeStep = nil })
			got, _ := runWith(t, fmt.Sprintf(c.call, filepath.Join(granted, c.path)),
				grant(t, c.outer, granted), grant(t, c.inner, filepath.Join(granted, c.innerRel)))
			if beforeStep != nil {
				t.Fatal("the open never stepped through", swapped)
			}
			wantNoLeak(t, got, fnOf(c))
			wantUntouched(t, outside, "SECRET")
			wantUntouched(t, filepath.Join(granted, "other"), "other")
			deep := filepath.Join(granted, "sub", "deep", "f.txt") // the leaf swap's target
			if swapped == filepath.Join(granted, "sub") {
				deep = filepath.Join(granted, "sub.orig", "deep", "f.txt")
			}
			if data, _ := os.ReadFile(deep); string(data) != "inside" {
				t.Errorf("%s = %q, want untouched", deep, data)
			}
		})
	}
}

// Without a swap, mixed nesting works, and a relative symlink inside the
// inner grant is followed as usual.
func TestMixedKindNestedGrantsWork(t *testing.T) {
	granted, _ := mixedTree(t)
	if err := os.Symlink("deep", filepath.Join(granted, "sub", "link")); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(granted, "sub")
	got, _ := runWith(t, fmt.Sprintf(`read_file(%q)`, filepath.Join(sub, "link", "f.txt")),
		grant(t, runtime.FSWrite, granted), grant(t, runtime.FSRead, sub))
	if got.Inspect() != "inside" {
		t.Fatalf("read under write = %s, want inside", got.Inspect())
	}
	for _, inner := range []string{sub, filepath.Join(sub, "f.txt")} {
		path := filepath.Join(sub, "f.txt")
		got, _ = runWith(t, fmt.Sprintf(`write_file(%q, "new")`, path),
			grant(t, runtime.FSRead, granted), grant(t, runtime.FSWrite, inner))
		if _, isErr := got.(*value.Error); isErr {
			t.Fatalf("write under read (grant %s) = %s", inner, got.Inspect())
		}
		if data, _ := os.ReadFile(path); string(data) != "new" {
			t.Fatalf("f.txt = %q after write under read", data)
		}
	}
	got, _ = runWith(t, fmt.Sprintf(`write_file(%q, "made")`, filepath.Join(sub, "fresh.txt")),
		grant(t, runtime.FSRead, granted), grant(t, runtime.FSWrite, filepath.Join(sub, "fresh.txt")))
	if data, _ := os.ReadFile(filepath.Join(sub, "fresh.txt")); string(data) != "made" {
		t.Fatalf("file grant create under read = %s, file %q", got.Inspect(), data)
	}
}

// Two grants that name one directory in different spellings (case, on a
// case-insensitive filesystem such as default APFS) must still confine to
// the outer one: grants are matched by identity, not by string (review
// round 2).
func TestCaseVariantNestedGrantSwapCannotEscape(t *testing.T) {
	for _, c := range []struct {
		kind       runtime.Kind
		call, path string
	}{
		{runtime.FSRead, `read_file(%q)`, "f.txt"},
		{runtime.FSWrite, `write_file(%q, "pwned")`, "new.txt"},
	} {
		t.Run(string(c.kind), func(t *testing.T) {
			granted, outside := mixedTree(t)
			upper := filepath.Join(filepath.Dir(granted), "G")
			if _, err := os.Stat(upper); err != nil {
				t.Skip("case-sensitive filesystem")
			}
			swapBeforeOpen(t, filepath.Join(granted, "sub"), outside)
			got, _ := runWith(t, fmt.Sprintf(c.call, filepath.Join(upper, "sub", c.path)),
				grant(t, c.kind, granted), grant(t, c.kind, filepath.Join(upper, "sub")))
			fn := "read_file"
			if c.kind == runtime.FSWrite {
				fn = "write_file"
			}
			wantNoLeak(t, got, fn)
			wantUntouched(t, outside, "SECRET")
		})
	}
}

// A file grant's leaf that is missing when checked and is planted as a
// symlink before the open must not be followed: neither the file it names
// nor a new file where it points may be written.
func TestMixedKindMissingLeafPlantedSymlink(t *testing.T) {
	for _, target := range []string{"deep/f.txt", "made.txt"} {
		t.Run(target, func(t *testing.T) {
			granted, _ := mixedTree(t)
			leaf := filepath.Join(granted, "sub", "fresh.txt")
			beforeStep = func(path string) {
				if path == leaf {
					beforeStep = nil
					if err := os.Symlink(target, path); err != nil {
						t.Error(err)
					}
				}
			}
			t.Cleanup(func() { beforeStep = nil })
			got, _ := runWith(t, fmt.Sprintf(`write_file(%q, "pwned")`, leaf),
				grant(t, runtime.FSRead, granted), grant(t, runtime.FSWrite, leaf))
			if beforeStep != nil {
				t.Fatal("the open never stepped through", leaf)
			}
			wantErrorHas(t, got, "write_file: ")
			if data, _ := os.ReadFile(filepath.Join(granted, "sub", "deep", "f.txt")); string(data) != "inside" {
				t.Errorf("sub/deep/f.txt = %q, want untouched", data)
			}
			if _, err := os.Lstat(filepath.Join(granted, "sub", "made.txt")); err == nil {
				t.Error("write_file created the planted symlink's target")
			}
		})
	}
}
