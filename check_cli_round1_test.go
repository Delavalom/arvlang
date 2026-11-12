package main

import (
	"fmt"
	"strings"
	"testing"
)

// A refusal names at most ten outside needs, then how many more there
// are, so a large script cannot make it one line of half a megabyte.
func TestManifestRefusalListsAtMostTenNeeds(t *testing.T) {
	var b strings.Builder
	b.WriteString("// arv:allow env=HOME\n")
	for i := 0; i < 25; i++ {
		fmt.Fprintf(&b, "net(\"h%d.example\");\n", i)
	}
	script := writeScript(t, b.String())
	for _, args := range [][]string{{"check", script}, {"run", "--manifest", script}} {
		code, _, errOut := runArv(args...)
		if code != 1 || strings.Count(errOut, "(net at ") != 10 ||
			!strings.Contains(errOut, "net=h9.example (net at 11:1), and 15 more, outside its arv:allow manifest\n") {
			t.Errorf("%q: exit %d, stderr %q", args, code, errOut)
		}
	}
}

func TestManifestRefusalSaysWhenAPathCannotBeResolved(t *testing.T) {
	t.Chdir(t.TempDir())
	script := writeScript(t, "// arv:allow fs_read=.\nread_file(\"missing/../x\");")
	code, _, errOut := runArv("check", script)
	if code != 1 || !strings.Contains(errOut, "cannot resolve fs_read=missing/../x (read_file at 2:1: ") ||
		strings.Contains(errOut, "outside its arv:allow manifest") {
		t.Errorf("exit %d, stderr %q", code, errOut)
	}
}
