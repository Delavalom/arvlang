package main

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
)

// A relative path that only becomes too long once the working directory
// is prepended is refused by the runtime before admission, so a header
// granting fs_read=/ covers the program, as --allow fs_read=/ does.
func TestManifestAcceptsAPathTheRuntimeNeverAdmits(t *testing.T) {
	t.Chdir(t.TempDir())
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	long := strings.Repeat("a", runtime.MaxScopeLen-len(cwd))
	script := writeScript(t, fmt.Sprintf("// arv:allow fs_read=/\nif (false) { read_file(%q) } else { puts(\"hi\") };", long))
	code, out, errOut := runArv("check", script)
	if code != 0 || strings.Contains(out, "--allow") || !strings.Contains(out, "never allowed: fs_read: scope is 4097 bytes long") {
		t.Errorf("check: exit %d, stdout %.200q, stderr %.200q", code, out, errOut)
	}
	if code, out, errOut := runArv("run", "--manifest", script); code != 0 || out != "hi\n" {
		t.Errorf("run --manifest: exit %d, stdout %q, stderr %.200q", code, out, errOut)
	}
}
