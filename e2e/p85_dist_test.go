package e2e

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The command is package main in cmd/arv, so go install names its binary
// arv, and the module root holds no Go package.
func TestP85CommandInCmdArv(t *testing.T) {
	out, err := exec.Command("go", "list", "-f", "{{.Name}} {{.Target}}", cmdArv).CombinedOutput()
	if err != nil {
		t.Fatalf("go list %s: %v\n%s", cmdArv, err, out)
	}
	name, target, _ := strings.Cut(strings.TrimSpace(string(out)), " ")
	if name != "main" || filepath.Base(target) != "arv" {
		t.Fatalf("go list: package %q installs as %q, want package main installed as arv", name, target)
	}
	// Any .go file at the root, whatever its build constraints, would make
	// it a package again; go list failing there could have other causes.
	root, err := filepath.Glob("../*.go")
	if err != nil || len(root) != 0 {
		t.Fatalf("the module root holds Go files %v (%v)", root, err)
	}
}

// make build writes the binary built from cmd/arv; BIN moves it out of the
// checkout so the test leaves the tree as it was. In a git checkout the
// binary names the checkout's HEAD.
func TestP85MakeBuild(t *testing.T) {
	checkMakeBuild(t)
}

func checkMakeBuild(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("make"); err != nil {
		t.Skip("make is not installed")
	}
	bin := t.TempDir()
	cmd := exec.Command("make", "-s", "build", "BIN="+bin)
	cmd.Dir = ".."
	cmd.Env = cleanEnv()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("make build: %v\n%s", err, out)
	}
	made := run(t, spec{script: "basics.arv", bin: filepath.Join(bin, "arv")})
	want := run(t, spec{script: "basics.arv"})
	if made.code != 0 || made.stdout != want.stdout || made.stderr != "" {
		t.Fatalf("the binary make built differs from the suite's\n=== make build ===\n%s\n=== suite ===\n%s", made, want)
	}
	if head := checkoutHead(); head != "" {
		if _, commit := version(t, filepath.Join(bin, "arv")); !strings.HasPrefix(commit, head) {
			t.Fatalf("make build names commit %q, want the checkout's HEAD %s", commit, head)
		}
	}
}
