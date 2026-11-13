package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// releaseField is the value of the one line of .goreleaser.yaml that reads
// "key: value", or of the one list item under "key:".
func releaseField(t *testing.T, config, key string) string {
	t.Helper()
	lines := strings.Split(config, "\n")
	var found []string
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if v, ok := strings.CutPrefix(line, key+":"); ok {
			v = strings.TrimSpace(v)
			if v == "" && i+1 < len(lines) {
				v, _ = strings.CutPrefix(strings.TrimSpace(lines[i+1]), "- ")
			}
			found = append(found, v)
		}
	}
	if len(found) != 1 || found[0] == "" {
		t.Fatalf(".goreleaser.yaml: want one %s, found %q", key, found)
	}
	return found[0]
}

// A release build, made as .goreleaser.yaml makes it (its main, flags and
// ldflags, with CGO off, for this machine), is named arv and prints the
// version and commit goreleaser passes it. Any VCS stamp the go command
// adds names the checkout's HEAD.
func TestP85ReleaseBuild(t *testing.T) {
	t.Parallel()
	checkReleaseBuild(t)
}

func checkReleaseBuild(t *testing.T) {
	t.Helper()
	b, err := os.ReadFile("../.goreleaser.yaml")
	if err != nil {
		t.Fatal(err)
	}
	config := string(b)
	pkg, binary := releaseField(t, config, "main"), releaseField(t, config, "binary")
	flags, ldflags := releaseField(t, config, "flags"), releaseField(t, config, "ldflags")
	if pkg != "./cmd/arv" || binary != "arv" {
		t.Fatalf(".goreleaser.yaml builds %s as %s, want ./cmd/arv as arv", pkg, binary)
	}
	for _, want := range []string{"goos: [darwin, linux]", "goarch: [amd64, arm64]", "CGO_ENABLED=0"} {
		if !strings.Contains(config, want) {
			t.Errorf(".goreleaser.yaml lacks %q", want)
		}
	}
	const sha = "0123456789abcdef0123456789abcdef01234567"
	ldflags = strings.NewReplacer("{{ .Version }}", "9.8.7", "{{ .FullCommit }}", sha).Replace(ldflags)
	if strings.Contains(ldflags, "{{") {
		t.Fatalf("ldflags %q use a template field this test does not fill", ldflags)
	}

	bin := filepath.Join(t.TempDir(), binary)
	args := append(strings.Fields(flags), "-ldflags="+ldflags, "-o", bin, pkg)
	cmd := exec.Command("go", append([]string{"build"}, args...)...)
	cmd.Dir = ".."
	cmd.Env = cleanEnv("CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	if ver, commit := version(t, bin); ver != "v9.8.7" || commit != sha {
		t.Fatalf("version %q, commit %q; want v9.8.7 and %s", ver, commit, sha)
	}
	checkStamp(t, bin)
}
