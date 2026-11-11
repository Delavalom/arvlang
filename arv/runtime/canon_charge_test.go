package runtime

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// deepTree makes dir/a/a/.../a, about 1000 bytes long (macOS's PATH_MAX
// is 1024), with a file f at the bottom, and returns that file's path.
func deepTree(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	deep := dir + strings.Repeat("/a", (1000-len(dir)-6)/2)
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(deep+"/f", []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	return deep + "/f"
}

// longPrefix makes a directory about 900 bytes deep in four 200-byte
// components, with d and the file small in it, and returns a path of
// about 4000 bytes that lstats that long prefix again and again:
// dir/d/../d/../.../small.
func longPrefix(t *testing.T) string {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir := base + strings.Repeat("/"+strings.Repeat("q", 200), 4)
	if err := os.MkdirAll(dir+"/d", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir+"/small", []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir + strings.Repeat("/d/..", (4080-len(dir))/5) + "/small"
}

// Canonical lstats every existing component, and each lstat copies the
// resolved prefix (once into a string, once into a C string) and builds a
// FileInfo, so a deep path costs O(depth x length): 623 KB for one call
// on a 996-byte path 455 levels deep, and about 1.4 MB for a 4086-byte
// path of "/d/.." pairs under a 900-byte prefix. None of it was charged,
// so read_file and spawn churned gigabytes under alloc_mb: 1 (review
// round 10). CanonicalCharged charges at least what it allocates.
func TestCanonicalChargesWhatItAllocates(t *testing.T) {
	deep := deepTree(t)
	// Behind a symlink, the resolved prefix no longer spells a prefix of
	// the path, so each lstat copies it into a string too.
	top := deep[:strings.Index(deep, "/a/")]
	link := filepath.Join(t.TempDir(), "l")
	if err := os.Symlink(top, link); err != nil {
		t.Fatal(err)
	}
	// A long target makes Readlink grow its buffer, 128 bytes at first.
	long := filepath.Join(t.TempDir(), "long")
	if err := os.Symlink(filepath.Dir(deep), long); err != nil {
		t.Fatal(err)
	}
	cases := map[string][2]string{
		"long link":   {long + "/f", deep},
		"deep tree":   {deep, deep},
		"long prefix": {longPrefix(t), ""},
		"via symlink": {link + deep[len(top):], deep},
	}
	for name, c := range cases {
		path, want := c[0], c[1]
		if want == "" {
			want = filepath.Clean(path)
		}
		var charged int64
		var got string
		var err error
		used := heapDuring(func() {
			got, err = CanonicalCharged(path, func(n int64) error { charged += n; return nil })
		})
		if err != nil || got != want {
			t.Fatalf("%s: Canonical = %.80q, %v", name, got, err)
		}
		if used > uint64(charged)+16<<10 {
			t.Errorf("%s: Canonical allocated %d bytes, charged %d", name, used, charged)
		}
		t.Logf("%s: %d bytes long, allocated %d, charged %d", name, len(path), used, charged)
	}
}

// A charge that fails stops Canonical at once, with the charge's error.
func TestCanonicalStopsWhenChargeFails(t *testing.T) {
	path := deepTree(t)
	stop := errors.New("over budget")
	var charged int64
	var err error
	used := heapDuring(func() {
		_, err = CanonicalCharged(path, func(n int64) error {
			if charged+n > 64<<10 {
				return stop
			}
			charged += n
			return nil
		})
	})
	if err != stop {
		t.Fatalf("got %v, want the charge's error", err)
	}
	if used > 128<<10 {
		t.Errorf("Canonical allocated %d bytes past a 64 KiB charge limit", used)
	}
}

// Admit and AttenuateEach pass the charge through, and return its error
// bare, not as a denial.
func TestAdmitChargesCanonical(t *testing.T) {
	path := deepTree(t)
	s := NewCapSet(mustCap(t, FSRead, filepath.Dir(path)))
	stop := errors.New("over budget")
	charge := func(int64) error { return stop }
	if _, err := s.AdmitCharged(Capability{Kind: FSRead, Scope: path}, charge); err != stop {
		t.Errorf("AdmitCharged = %v, want the charge's error", err)
	}
	req := func(int) Capability { return Capability{Kind: FSRead, Scope: path} }
	if _, err := s.AttenuateEach(3, req, charge, nil); err != stop {
		t.Errorf("AttenuateEach = %v, want the charge's error", err)
	}
	var charged int64
	c, err := s.AdmitCharged(Capability{Kind: FSRead, Scope: path}, func(n int64) error { charged += n; return nil })
	if err != nil || c.Scope != path || charged < int64(len(path))*100 {
		t.Errorf("AdmitCharged = %.80v, %v after charging %d", c, err, charged)
	}
}
