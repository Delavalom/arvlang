//go:build unix

package evaluator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// openIn confines an open to its grant: a symlink inside the grant that
// leads out of it is refused, a relative one that stays inside is
// followed, and errors name the full path, as a plain open would.
func TestOpenInStaysInsideTheGrant(t *testing.T) {
	root := t.TempDir()
	granted, outside := filepath.Join(root, "g"), filepath.Join(root, "out")
	mustMkdir(t, filepath.Join(granted, "real"))
	mustMkdir(t, outside)
	for path, text := range map[string]string{
		filepath.Join(granted, "real", "f.txt"): "inside",
		filepath.Join(outside, "f.txt"):         "SECRET",
	} {
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for link, to := range map[string]string{"esc": outside, "in": "real"} {
		if err := os.Symlink(to, filepath.Join(granted, link)); err != nil {
			t.Fatal(err)
		}
	}

	escaping := filepath.Join(granted, "esc", "f.txt")
	if f, err := openRegularIn(fsTarget{granted, granted, escaping}); err == nil {
		f.Close()
		t.Fatal("openRegularIn followed a symlink out of its grant")
	} else if !strings.Contains(err.Error(), "path escapes") || !strings.Contains(err.Error(), escaping) {
		t.Fatalf("err = %v, want a path-escape error naming %s", err, escaping)
	}
	if err := writeIn(fsTarget{granted, granted, filepath.Join(granted, "esc", "new.txt")}, []byte("x")); err == nil {
		t.Fatal("writeIn followed a symlink out of its grant")
	}
	if _, err := os.Lstat(filepath.Join(outside, "new.txt")); err == nil {
		t.Fatal("writeIn created a file outside its grant")
	}

	f, err := openRegularIn(fsTarget{granted, granted, filepath.Join(granted, "in", "f.txt")})
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if data, err := readCapped(f); err != nil || string(data) != "inside" {
		t.Fatalf("read = %q, %v; want the file the inside link reaches", data, err)
	}
}
