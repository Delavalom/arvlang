package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeScript(t *testing.T, src string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "main.arv")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRunCLI(t *testing.T) {
	t.Setenv("ARV_GREETING", "hola")
	script := writeScript(t, "// greet\nputs(getenv(\"ARV_GREETING\"));")

	var out, errOut bytes.Buffer
	if code := runCLI([]string{"run", script}, &out, &errOut); code != 1 {
		t.Fatalf("without --allow: exit %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "capability denied") {
		t.Fatalf("stderr = %q", errOut.String())
	}

	out.Reset()
	errOut.Reset()
	if code := runCLI([]string{"run", "--allow", "env=ARV_GREETING", script}, &out, &errOut); code != 0 {
		t.Fatalf("with --allow: exit %d, stderr %q", code, errOut.String())
	}
	if out.String() != "hola\n" {
		t.Fatalf("stdout = %q", out.String())
	}
}

func TestRunCLIRejectsBadFlags(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := runCLI([]string{"run", "--allow", "disk=/", "x.arv"}, &out, &errOut); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	if code := runCLI([]string{"build"}, &out, &errOut); code != 2 {
		t.Fatalf("unknown command exit %d, want 2", code)
	}
}
