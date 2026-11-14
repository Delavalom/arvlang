package main

import (
	"runtime"
	"runtime/debug"
	"strings"
)

// version and commit are set at link time by release builds
// (.goreleaser.yaml: -X main.version=... -X main.commit=...). Left empty,
// arv version reads what the go command stamped into the binary.
var (
	version string
	commit  string
)

// versionText is what arv version prints: the version, the git commit and
// the Go toolchain, one per line. The link-time variables come first. Then
// what the go command stamped: in a git checkout, vcs.revision (and
// vcs.modified, shown as "(modified)") and, since Go 1.24, a version
// derived from the commit; for go install pkg@version, the module version,
// whose pseudo-version names a commit's first 12 hex digits. A commit it
// cannot know (a tagged go install, a build with -buildvcs=false, a git
// worktree, which the go command does not stamp) prints as unknown.
func versionText(ldVersion, ldCommit string, bi *debug.BuildInfo) string {
	v, rev := ldVersion, ldCommit
	var vcsRev string
	var vcsModified bool
	if bi != nil {
		if v == "" {
			v = bi.Main.Version
		}
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				vcsRev = s.Value
			case "vcs.modified":
				vcsModified = s.Value == "true"
			}
		}
		if rev == "" {
			rev = vcsRev
		}
		if rev == "" {
			rev = pseudoRevision(bi.Main.Version)
		}
	}
	if v == "" {
		v = "(devel)"
	}
	line := "commit unknown"
	if rev != "" {
		line = "commit " + rev
		if vcsModified && rev == vcsRev {
			line += " (modified)"
		}
	}
	return "arv " + v + "\n" + line + "\n" + runtime.Version() + " " + runtime.GOOS + "/" + runtime.GOARCH + "\n"
}

// pseudoRevision is the commit a Go pseudo-version names, such as
// c1b8296f0c55 in v0.0.0-20261112233516-c1b8296f0c55 (or in
// v0.3.1-0.20261112233516-c1b8296f0c55+dirty), or "" if v is not one.
func pseudoRevision(v string) string {
	v, _, _ = strings.Cut(v, "+")
	i := strings.LastIndexByte(v, '-')
	if i < 0 {
		return ""
	}
	rev, rest := v[i+1:], v[:i]
	j := strings.LastIndexAny(rest, "-.")
	if len(rev) != 12 || strings.Trim(rev, "0123456789abcdef") != "" ||
		j < 0 || len(rest)-j-1 != 14 || strings.Trim(rest[j+1:], "0123456789") != "" {
		return ""
	}
	return rev
}
