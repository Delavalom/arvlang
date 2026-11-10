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

// leakTree builds dir/noperm (mode 000, so lstat under it fails),
// dir/loop -> itself, dir/allowed/x and dir/secret/link -> ../allowed/x.
func leakTree(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	mustMkdir(t, filepath.Join(dir, "allowed"))
	mustMkdir(t, filepath.Join(dir, "secret"))
	mustMkdir(t, filepath.Join(dir, "noperm"))
	os.WriteFile(filepath.Join(dir, "allowed", "x"), []byte("x"), 0o644)
	for link, to := range map[string]string{"loop": filepath.Join(dir, "loop"), "secret/link": "../allowed/x"} {
		if err := os.Symlink(to, filepath.Join(dir, link)); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(filepath.Join(dir, "noperm"), 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(filepath.Join(dir, "noperm"), 0o755) })
	return dir
}

// I1/I3 at use: a denied read_file or write_file names the path as the
// script wrote it (made absolute) and nothing else. Neither the resolved
// target nor an lstat error may leak.
func TestUseTimeDenialRevealsNothing(t *testing.T) {
	dir := leakTree(t)
	secret := grant(t, runtime.FSRead, filepath.Join(dir, "secret"))
	secretW := grant(t, runtime.FSWrite, filepath.Join(dir, "secret"))
	for _, tt := range []struct {
		path string
		caps []runtime.Capability
		hide []string
	}{
		{"/etc/localtime", nil, []string{"zoneinfo", "/private"}},
		{filepath.Join(dir, "noperm", "x"), nil, []string{"permission", "lstat"}},
		{filepath.Join(dir, "loop", "x"), nil, []string{"symbolic", "canonical"}},
		{filepath.Join(dir, "secret", "link"), []runtime.Capability{secret, secretW}, []string{"allowed/x"}},
	} {
		for _, call := range []struct{ src, want string }{
			{fmt.Sprintf(`read_file(%q)`, tt.path), fmt.Sprintf(`capability denied: fs_read(%q)`, tt.path)},
			{fmt.Sprintf(`write_file(%q, "w")`, tt.path), fmt.Sprintf(`capability denied: fs_write(%q)`, tt.path)},
		} {
			got, _ := runWith(t, call.src, tt.caps...)
			e, ok := got.(*value.Error)
			if !ok || !strings.Contains(e.Message, call.want) {
				t.Errorf("%s = %s, want %s", call.src, got.Inspect(), call.want)
				continue
			}
			for _, h := range tt.hide {
				if strings.Contains(e.Message, h) {
					t.Errorf("%s leaks %q: %s", call.src, h, e.Message)
				}
			}
		}
	}
}
