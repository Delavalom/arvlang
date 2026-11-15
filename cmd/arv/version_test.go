package main

import (
	"bytes"
	"runtime"
	"runtime/debug"
	"strings"
	"testing"
)

const sha = "c1b8296f0c55552966f28c02fad0fc2ea0a607da"

func buildInfo(version string, settings ...string) *debug.BuildInfo {
	bi := &debug.BuildInfo{Main: debug.Module{Path: "github.com/delavalom/arvlang", Version: version}}
	for i := 0; i+1 < len(settings); i += 2 {
		bi.Settings = append(bi.Settings, debug.BuildSetting{Key: settings[i], Value: settings[i+1]})
	}
	return bi
}

// arv version prints the version, the git commit and the Go toolchain:
// from the link-time variables of a release build first, then from what
// the go command stamped (vcs.revision in a checkout, the module version
// for go install pkg@version), and "unknown" rather than a guess.
func TestVersionText(t *testing.T) {
	goLine := runtime.Version() + " " + runtime.GOOS + "/" + runtime.GOARCH + "\n"
	pseudo := "v0.0.0-20261112233516-c1b8296f0c55"
	for _, c := range []struct {
		name, ldVersion, ldCommit string
		bi                        *debug.BuildInfo
		want                      string
	}{
		{"release build", "v0.3.0", sha, buildInfo("(devel)"),
			"arv v0.3.0\ncommit " + sha + "\n"},
		{"release build from a checkout", "v0.3.0", sha,
			buildInfo(pseudo, "vcs.revision", "0123456789abcdef0123456789abcdef01234567", "vcs.modified", "true"),
			"arv v0.3.0\ncommit " + sha + "\n"},
		{"checkout", "", "", buildInfo(pseudo, "vcs.revision", sha, "vcs.modified", "false"),
			"arv " + pseudo + "\ncommit " + sha + "\n"},
		{"checkout with changes", "", "", buildInfo(pseudo+"+dirty", "vcs.revision", sha, "vcs.modified", "true"),
			"arv " + pseudo + "+dirty\ncommit " + sha + " (modified)\n"},
		{"go install of a commit", "", "", buildInfo(pseudo),
			"arv " + pseudo + "\ncommit c1b8296f0c55\n"},
		{"go install of a commit after a tag", "", "", buildInfo("v0.3.1-0.20261112233516-c1b8296f0c55"),
			"arv v0.3.1-0.20261112233516-c1b8296f0c55\ncommit c1b8296f0c55\n"},
		{"go install of a pre-release commit", "", "", buildInfo("v0.4.0-rc.1.0.20261112233516-c1b8296f0c55"),
			"arv v0.4.0-rc.1.0.20261112233516-c1b8296f0c55\ncommit c1b8296f0c55\n"},
		{"go install of a tag", "", "", buildInfo("v0.3.0"), "arv v0.3.0\ncommit unknown\n"},
		{"tag that only looks like a commit", "", "", buildInfo("v1.0.0-c1b8296f0c55"),
			"arv v1.0.0-c1b8296f0c55\ncommit unknown\n"},
		{"worktree or -buildvcs=false", "", "", buildInfo("(devel)"), "arv (devel)\ncommit unknown\n"},
		{"no build info", "", "", nil, "arv (devel)\ncommit unknown\n"},
	} {
		if got, want := versionText(c.ldVersion, c.ldCommit, c.bi), c.want+goLine; got != want {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, want)
		}
	}
}

func TestVersionCommand(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := runCLI([]string{"version"}, &out, &errOut); code != 0 || errOut.Len() != 0 {
		t.Fatalf("exit %d, stderr %q", code, errOut.String())
	}
	bi, _ := debug.ReadBuildInfo()
	if out.String() != versionText(version, commit, bi) || !strings.HasPrefix(out.String(), "arv ") {
		t.Fatalf("stdout %q", out.String())
	}
	out.Reset()
	if code := runCLI([]string{"version", "extra"}, &out, &errOut); code != 2 || out.Len() != 0 ||
		!strings.HasPrefix(errOut.String(), "usage:") {
		t.Fatalf("version extra: exit %d, stdout %q, stderr %q", code, out.String(), errOut.String())
	}
	if !strings.Contains(usage, "\n  arv version ") {
		t.Error("usage does not list arv version")
	}
}
