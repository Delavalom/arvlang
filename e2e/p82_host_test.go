package e2e

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Phase 8.2: the example Go host (examples/host), built and run as a
// binary. Its k8s_apply effect is denied without a k8s grant, allowed in
// the granted namespace, from the root and from a child handed
// k8s("staging"), and denied in a namespace the host did not grant.
func TestP82HostExample(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "host")
	args := []string{"build", "-o", bin}
	if os.Getenv("ARV_E2E_RACE") != "" {
		args = append(args, "-race")
	}
	build := exec.Command("go", append(args, "github.com/delavalom/arvlang/examples/host")...)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building examples/host: %v\n%s", err, out)
	}
	for _, tt := range []struct {
		name   string
		args   []string
		code   int
		stdout string
		stderr string
	}{
		{"no grant", nil, 1, "", `error: capability denied: k8s("staging") in task root (holds: [])`},
		{"staging", []string{"-allow", "staging"}, 0,
			"web.yaml\napi.yaml\nprod: capability\ncluster: staging=[web.yaml api.yaml]\n", ""},
		{"staging and prod", []string{"-allow", "staging", "-allow", "prod"}, 0,
			"web.yaml\napi.yaml\nprod: applied\ncluster: prod=[web.yaml] staging=[web.yaml api.yaml]\n", ""},
		{"prod only", []string{"-allow", "prod"}, 1, "", `capability denied: k8s("staging") in task root (holds: [k8s("prod")])`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			cmd := exec.CommandContext(ctx, bin, tt.args...)
			cmd.Env = []string{} // never inherit the user's env
			cmd.Dir = t.TempDir()
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			err := cmd.Run()
			code := 0
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) && exitErr.Exited() {
				code = exitErr.ExitCode()
			} else if err != nil {
				t.Fatalf("running %s: %v", bin, err)
			}
			if code != tt.code || stdout.String() != tt.stdout || !strings.Contains(stderr.String(), tt.stderr) {
				t.Fatalf("exit %d, stdout %q, stderr %q; want exit %d, stdout %q, stderr containing %q",
					code, stdout.String(), stderr.String(), tt.code, tt.stdout, tt.stderr)
			}
		})
	}
}
