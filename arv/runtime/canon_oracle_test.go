package runtime

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// realTempDir is t.TempDir with its symlinks resolved (/var is one on
// macOS), so paths under it are already canonical.
func realTempDir(t *testing.T) string {
	t.Helper()
	d, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// outsideWorld makes, in a directory o outside any grant, one name of each
// kind a request can meet there, all five bytes long: a missing name, a
// file, a directory with more below it, and symlinks whose targets differ
// in length or in which of their components exist.
func outsideWorld(t *testing.T) (o string, names []string) {
	t.Helper()
	o = realTempDir(t)
	if err := os.WriteFile(o+"/fileA", []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(o+"/dirAA/y/deeper/still", 0o755); err != nil {
		t.Fatal(err)
	}
	links := map[string]string{
		"linkA": "/x",
		"linkB": "/" + strings.Repeat("z", 200),
		"linkC": "/" + strings.Repeat("z", 201),
		"linkD": "/usr/bin/q",
		"linkE": "/usr/zzz/q",
		"linkF": o + "/dirAA",
	}
	for name, target := range links {
		if err := os.Symlink(target, o+"/"+name); err != nil {
			t.Fatal(err)
		}
	}
	return o, []string{"missA", "fileA", "dirAA", "linkA", "linkB", "linkC", "linkD", "linkE", "linkF"}
}

// What a denied request charges must not depend on what lies outside the
// grants. Canonicalising lstats each existing component and follows each
// symlink, and charged each step as it went, so a program could fill its
// budget to an exact level and learn, from "budget exceeded" against
// "capability denied", whether a name outside its grants was a symlink,
// how long its target was and which of the target's components existed,
// and whether a path's directories existed (review round 11).
func TestDeniedRequestCostIgnoresOutside(t *testing.T) {
	g := realTempDir(t)
	o, names := outsideWorld(t)
	s := NewCapSet(mustCap(t, FSRead, g))
	for _, kind := range []Kind{FSRead, FSWrite} {
		costs := map[string]int64{}
		for _, name := range names {
			for _, tail := range []string{"/y", "/y/deeper/still/f"} {
				var charged int64
				_, err := s.AdmitCharged(Capability{Kind: kind, Scope: o + "/" + name + tail},
					func(n int64) error { charged += n; return nil })
				var d *DeniedError
				if !errors.As(err, &d) {
					t.Fatalf("%s%s: got %v, want a denial", name, tail, err)
				}
				costs[name+tail] = charged
			}
		}
		for key, c := range costs {
			want := costs["missA"+key[len("missA"):]]
			if c != want {
				t.Errorf("%s: denied %s charged %d, missA charged %d", kind, key, c, want)
			}
		}
	}
}

// The same, with the budget a byte short of what a missing name costs:
// every name fails the same way, with the charge's error, and one byte
// more lets every one through to the denial.
func TestDeniedRequestStopsAlikeOutside(t *testing.T) {
	g := realTempDir(t)
	o, names := outsideWorld(t)
	s := NewCapSet(mustCap(t, FSRead, g))
	var need int64
	if _, err := s.AdmitCharged(Capability{Kind: FSRead, Scope: o + "/missA/y"},
		func(n int64) error { need += n; return nil }); err == nil {
		t.Fatal("missA admitted")
	}
	stop := errors.New("over budget")
	for _, left := range []int64{need - 1, need} {
		for _, name := range names {
			spent := int64(0)
			_, err := s.AdmitCharged(Capability{Kind: FSRead, Scope: o + "/" + name + "/y"}, func(n int64) error {
				if spent += n; spent > left {
					return stop
				}
				return nil
			})
			var d *DeniedError
			if left < need && err != stop || left == need && !errors.As(err, &d) {
				t.Errorf("%s with %d of %d bytes: got %v", name, left, need, err)
			}
		}
	}
}

// An admitted request is still charged at least what canonicalising it
// allocated, past symlinks too.
func TestAdmitChargesSymlinksWhenAllowed(t *testing.T) {
	deep := deepTree(t)
	long := filepath.Join(realTempDir(t), "long")
	if err := os.Symlink(filepath.Dir(deep), long); err != nil {
		t.Fatal(err)
	}
	top := deep[:strings.Index(deep, "/a/")]
	link := filepath.Join(realTempDir(t), "l")
	if err := os.Symlink(top, link); err != nil {
		t.Fatal(err)
	}
	s := NewCapSet(mustCap(t, FSRead, top))
	for _, path := range []string{long + "/f", link + deep[len(top):]} {
		var charged int64
		var c Capability
		var err error
		used := heapDuring(func() {
			c, err = s.AdmitCharged(Capability{Kind: FSRead, Scope: path}, func(n int64) error { charged += n; return nil })
		})
		if err != nil || c.Scope != deep {
			t.Fatalf("%.60s: Admit = %.80v, %v", path, c, err)
		}
		if used > uint64(charged)+16<<10 {
			t.Errorf("%.60s: Admit allocated %d bytes, charged %d", path, used, charged)
		}
		// The cost past the symlink is more than the text alone is
		// charged, and charging it can fail the admit.
		if pre := prepaidCost(path); charged <= pre {
			t.Errorf("%.60s: charged %d, no more than the %d its text costs", path, charged, pre)
		}
		calls := 0
		s.AdmitCharged(Capability{Kind: FSRead, Scope: path}, func(int64) error { calls++; return nil })
		stop := errors.New("over budget")
		n := 0
		_, err = s.AdmitCharged(Capability{Kind: FSRead, Scope: path}, func(int64) error {
			if n++; n == calls {
				return stop
			}
			return nil
		})
		if err != stop {
			t.Errorf("%.60s: failing the last charge gave %v", path, err)
		}
	}
}

// A symlink inside a grant that leads out of it is denied, and costs its
// text alone too, however long its target: the task cannot read where it
// points.
func TestDeniedSymlinkInGrantCostsItsText(t *testing.T) {
	g := realTempDir(t)
	o, _ := outsideWorld(t)
	targets := map[string]string{"inLk1": "/x", "inLk2": "/" + strings.Repeat("z", 200), "inLk3": o + "/dirAA"}
	for name, target := range targets {
		if err := os.Symlink(target, g+"/"+name); err != nil {
			t.Fatal(err)
		}
	}
	s := NewCapSet(mustCap(t, FSRead, g))
	costs := map[int64][]string{}
	for name := range targets {
		var charged int64
		_, err := s.AdmitCharged(Capability{Kind: FSRead, Scope: g + "/" + name + "/y"},
			func(n int64) error { charged += n; return nil })
		var d *DeniedError
		if !errors.As(err, &d) {
			t.Fatalf("%s: got %v, want a denial", name, err)
		}
		costs[charged] = append(costs[charged], name)
	}
	if len(costs) != 1 {
		t.Errorf("denied links inside the grant cost differently: %v", costs)
	}
}
