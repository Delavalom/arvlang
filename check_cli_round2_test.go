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

// Notes and refusals quote text from the program. They must not carry
// characters that reorder or hide what the reader sees: bidi controls
// (U+202E), C1 controls (U+009B, CSI), zero-width characters.
func TestNotesAndRefusalsNeutraliseBidiAndC1(t *testing.T) {
	bad := []string{"‮", "\u009b", "​", "⁦", "\x1b", "\x7f"}
	hasBad := func(s string) bool {
		for _, b := range bad {
			if strings.Contains(s, b) {
				return true
			}
		}
		return false
	}
	t.Chdir(t.TempDir())
	if err := os.Mkdir("d", 0o755); err != nil {
		t.Fatal(err)
	}
	note := writeScript(t, "http_get(\"http://a‮b.1:9/\");\n")
	code, out, _ := runArv("check", note)
	if code != 0 || !strings.Contains(out, "# http_get at 1:1: host a?b.1 is not an IP literal") || hasBad(out) {
		t.Errorf("check (note): exit %d, stdout %q", code, out)
	}
	refusal := writeScript(t, "// arv:allow fs_read=d\nread_file(\"d/missing\u009b2J‮⁦/../x\");\n")
	for _, args := range [][]string{{"check", refusal}, {"run", "--manifest", refusal}} {
		code, _, errOut := runArv(args...)
		if code != 1 || !strings.Contains(errOut, "cannot resolve") || hasBad(errOut) {
			t.Errorf("%q: exit %d, stderr %q", args, code, errOut)
		}
	}
	for in, want := range map[string]string{"a‮b": "a?b", "x\u009by": "x?y", "tab\there": "tab?here",
		"café 世": "café 世", "z​w": "z?w"} {
		if got := oneLine(in); got != want {
			t.Errorf("oneLine(%q) = %q, want %q", in, got, want)
		}
	}
}
