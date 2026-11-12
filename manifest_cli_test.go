package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSplitManifest(t *testing.T) {
	for _, tt := range []struct {
		in, rest []string
		manifest bool
	}{
		{[]string{"f.arv"}, []string{"f.arv"}, false},
		{[]string{"--manifest", "f.arv"}, []string{"f.arv"}, true},
		{[]string{"--timeout", "5s", "--manifest", "f.arv"}, []string{"--timeout", "5s", "f.arv"}, true},
		{[]string{"--allow", "--manifest", "f.arv"}, []string{"--allow", "--manifest", "f.arv"}, false},
		{[]string{"--manifest"}, nil, true},
	} {
		rest, manifest := splitManifest(tt.in)
		if !reflect.DeepEqual(rest, tt.rest) || manifest != tt.manifest {
			t.Errorf("splitManifest(%q) = %q, %v", tt.in, rest, manifest)
		}
	}
}

func TestRunManifestGrantsTheHeader(t *testing.T) {
	t.Setenv("ARV_P81", "hola")
	script := writeScript(t, "// arv:allow env=ARV_P81\nputs(getenv(\"ARV_P81\"));")
	if code, out, errOut := runArv("run", "--timeout", "30s", "--manifest", script); code != 0 || out != "hola\n" {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errOut)
	}
	// Without --manifest the header grants nothing.
	if code, _, errOut := runArv("run", script); code != 1 || !strings.Contains(errOut, "capability denied") {
		t.Fatalf("without --manifest: exit %d, stderr %q", code, errOut)
	}
}

// A need arv check cannot see still runs, holding the header's grants
// alone, so the runtime denies it.
func TestRunManifestIsTheWholeGrantSet(t *testing.T) {
	t.Setenv("ARV_P81_B", "secret")
	script := writeScript(t, "// arv:allow env=ARV_P81_A\nlet get = fn(name) { getenv(name) };\nputs(get(\"ARV_P81_B\"));")
	code, out, errOut := runArv("run", "--manifest", script)
	if code != 1 || out != "" || !strings.Contains(errOut, `capability denied: env("ARV_P81_B")`) {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errOut)
	}
}

func TestRunManifestRefusesBeforeRunning(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	script := writeScript(t, "// arv:allow env=A\nputs(\"ran\");\nwrite_file(\"made.txt\", \"x\");\ngetenv(\"B\");")
	code, out, errOut := runArv("run", "--manifest", script)
	want := "error: " + script + " needs fs_write=made.txt (write_file at 3:1), env=B (getenv at 4:1), outside its arv:allow manifest\n"
	if code != 1 || out != "" || errOut != want {
		t.Fatalf("exit %d, stdout %q\nstderr %q\nwant   %q", code, out, errOut, want)
	}
	if _, err := os.Stat(filepath.Join(dir, "made.txt")); !os.IsNotExist(err) {
		t.Fatalf("made.txt exists: %v", err)
	}
}

func TestRunManifestErrors(t *testing.T) {
	pure := writeScript(t, "puts(1 + 1);")
	if code, out, errOut := runArv("run", "--manifest", pure); code != 0 || out != "2\n" {
		t.Errorf("no header, pure program: exit %d, stdout %q, stderr %q", code, out, errOut)
	}
	if code, _, errOut := runArv("run", "--allow", "env=A", "--manifest", pure); code != 2 || !strings.Contains(errOut, "cannot be combined with --allow") {
		t.Errorf("with --allow: exit %d, stderr %q", code, errOut)
	}
	bad := writeScript(t, "// arv:allow fs_read=\nputs(1);")
	if code, _, errOut := runArv("run", "--manifest", bad); code != 1 || !strings.Contains(errOut, bad+": line 1: ") {
		t.Errorf("bad header: exit %d, stderr %q", code, errOut)
	}
	broken := writeScript(t, "// arv:allow env=A\nlet = ;")
	code, _, errOut := runArv("run", "--manifest", broken)
	if code != 1 || !strings.HasPrefix(errOut, "error: parse error: ") || !strings.Contains(errOut, broken+":2:") {
		t.Errorf("parse error: exit %d, stderr %q", code, errOut)
	}
	if code, _, errOut := runArv("run", "--manifest"); code != 2 || !strings.HasPrefix(errOut, "usage:") {
		t.Errorf("no file: exit %d, stderr %q", code, errOut)
	}
}
