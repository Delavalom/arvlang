package runtime

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A denied request is charged exactly prepaidCost(path), the cost of its
// text, whatever lies outside the grants: not only the same for every
// name (TestDeniedRequestCostIgnoresOutside), but that amount, the
// buffer the walk starts with included.
func TestDeniedRequestCostsPrepaid(t *testing.T) {
	g := realTempDir(t)
	o, names := outsideWorld(t)
	s := NewCapSet(mustCap(t, FSRead, g))
	for _, kind := range []Kind{FSRead, FSWrite} {
		for _, name := range names {
			for _, tail := range []string{"/y", "/y/deeper/still/f"} {
				path := o + "/" + name + tail
				charged, err := admitCost(s, kind, path)
				var d *DeniedError
				if !errors.As(err, &d) {
					t.Fatalf("%s%s: got %v, want a denial", name, tail, err)
				}
				if want := prepaidCost(path); charged != want {
					t.Errorf("%s: denied %s%s charged %d, want prepaidCost %d", kind, name, tail, charged, want)
				}
			}
		}
	}
}

// Whether a component lies inside a grant (known) is judged against
// grants of the request's own kind. An fs_write request into a directory
// the task may only read is denied, and costs its text alone too,
// whatever that directory holds: a grant of the other kind does not make
// the walk charge step by step.
func TestDeniedRequestIntoOtherKindCostsItsText(t *testing.T) {
	g := realTempDir(t)
	if err := os.WriteFile(g+"/fileA", []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(g+"/dirAA/y/deeper/still", 0o755); err != nil {
		t.Fatal(err)
	}
	s := NewCapSet(mustCap(t, FSRead, g))
	for _, name := range []string{"missA", "fileA", "dirAA"} {
		path := g + "/" + name + "/y/deeper/still/f"
		charged, err := admitCost(s, FSWrite, path)
		var d *DeniedError
		if !errors.As(err, &d) {
			t.Fatalf("%s: got %v, want a denial", name, err)
		}
		if want := prepaidCost(path); charged != want {
			t.Errorf("fs_write %s under an fs_read grant charged %d, want prepaidCost %d", name, charged, want)
		}
	}
}

// A request Admit allows, or that fails inside a grant, is charged what
// the whole walk cost (CanonicalCharged) or prepaidCost(path), whichever
// is more: the excess past the prepaid amount is charged in full, also
// when it was spent between two symlinks, and also when the request then
// fails inside the grant. Each case follows a symlink to a target about
// 1000 bytes and 450 directories deep, whose walk costs hundreds of KB.
func TestAdmitChargesTheWalkPastPrepaid(t *testing.T) {
	deep := deepTree(t)
	deepDir := filepath.Dir(deep)
	top := deep[:strings.Index(deep, "/a/")]
	o := realTempDir(t)
	for link, target := range map[string]string{o + "/l1": deepDir, deepDir + "/up": top, top + "/lk": deepDir} {
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
	}
	s := NewCapSet(mustCap(t, FSRead, top))
	for _, c := range []struct {
		name, path string
		fails      bool
	}{
		{"one symlink", o + "/l1/f", false},
		{"two symlinks", o + "/l1/up/new", false},
		{"fails inside the grant", top + "/lk/nothere/../f", true},
	} {
		var walk int64
		CanonicalCharged(c.path, func(n int64) error { walk += n; return nil })
		charged, err := admitCost(s, FSRead, c.path)
		var ce *CanonicalError
		if c.fails != errors.As(err, &ce) || !c.fails && err != nil {
			t.Fatalf("%s: Admit = %v", c.name, err)
		}
		want := max(walk, prepaidCost(c.path))
		if walk < 200<<10 || charged != want {
			t.Errorf("%s: charged %d, want %d (walk %d, prepaid %d)", c.name, charged, want, walk, prepaidCost(c.path))
		}
	}
}

// admitCost is what s.AdmitCharged charges for a kind request for path,
// with no limit, and its error.
func admitCost(s CapSet, kind Kind, path string) (int64, error) {
	var charged int64
	_, err := s.AdmitCharged(Capability{Kind: kind, Scope: path},
		func(n int64) error { charged += n; return nil })
	return charged, err
}
