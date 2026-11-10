//go:build unix

package evaluator

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
)

// threeLevel is a nesting of three grants, g ⊃ g/sub ⊃ g/sub/deep (or a
// file in it), where the two outer ones are of the other kind than the
// innermost, so CapSet.Grant picks the innermost and confiningRoot must
// find the outer ones by identity.
type threeLevel struct {
	name         string
	outer, inner runtime.Kind
	innerRel     string // the innermost grant, relative to g
	call, path   string // %q is the path; path is relative to g
}

var threeLevelCases = []threeLevel{
	{"read dir", runtime.FSWrite, runtime.FSRead, "sub/deep", `read_file(%q)`, "sub/deep/f.txt"},
	{"read file", runtime.FSWrite, runtime.FSRead, "sub/deep/f.txt", `read_file(%q)`, "sub/deep/f.txt"},
	{"write dir", runtime.FSRead, runtime.FSWrite, "sub/deep", `write_file(%q, "pwned")`, "sub/deep/new.txt"},
	{"write file", runtime.FSRead, runtime.FSWrite, "sub/deep/f.txt", `write_file(%q, "pwned")`, "sub/deep/f.txt"},
}

func (c threeLevel) grants(t *testing.T, granted string) []runtime.Capability {
	return []runtime.Capability{
		grant(t, c.outer, granted),
		grant(t, c.outer, filepath.Join(granted, "sub")),
		grant(t, c.inner, filepath.Join(granted, c.innerRel)),
	}
}

// confiningRoot must return the outermost ancestor that matches a grant,
// not the innermost: the middle grant's directory (g/sub) lies inside the
// outer one, where a writer can swap it for a symlink, so starting there
// would trust a path the task does not control. The order the grants are
// held in must not matter.
func TestConfiningRootPicksOutermostOfSeveral(t *testing.T) {
	granted, _ := mixedTree(t)
	for _, c := range threeLevelCases {
		held := c.grants(t, granted)
		inner := held[2].Scope
		for _, order := range [][]runtime.Capability{held, {held[2], held[1], held[0]}, {held[1], held[2], held[0]}} {
			if got := confiningRoot(order, inner); got != granted {
				t.Errorf("%s: confiningRoot = %s, want the outermost grant %s (held %v)", c.name, got, granted, order)
			}
		}
	}
}

// I3 with three nested grants: swapping the middle grant's directory
// (g/sub) for a symlink to outside every grant, after the check and before
// the open, must not redirect an open beneath the innermost grant. An open
// confined to g/sub instead of g would follow the swapped-in link (review:
// mutation round 2, confiningRoot innermost-first read out/deep/f.txt).
func TestThreeLevelNestedGrantSwapCannotEscape(t *testing.T) {
	for _, c := range threeLevelCases {
		for _, to := range []string{"outside", "other"} {
			t.Run(c.name+" to "+to, func(t *testing.T) {
				granted, outside := mixedTree(t)
				target := "other" // relative: stays inside g
				if to == "outside" {
					target = outside
				}
				swapBeforeOpen(t, filepath.Join(granted, "sub"), target)
				got, _ := runWith(t, fmt.Sprintf(c.call, filepath.Join(granted, c.path)), c.grants(t, granted)...)
				fn := "read_file"
				if c.inner == runtime.FSWrite {
					fn = "write_file"
				}
				wantNoLeak(t, got, fn)
				wantUntouched(t, outside, "SECRET")
				wantUntouched(t, filepath.Join(granted, "other"), "other")
			})
		}
	}
}
