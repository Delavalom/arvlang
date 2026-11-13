package main

import (
	"regexp"
	"runtime"
	"runtime/debug"
	"strings"
	"testing"
)

// pseudoShape is a Go pseudo-version (before any "+build" suffix): a
// 14-digit timestamp after "-" or ".", then "-" and 12 lowercase hex
// digits, which it captures.
var pseudoShape = regexp.MustCompile(`^.*[-.][0-9]{14}-([0-9a-f]{12})$`)

// pseudoRevision agrees with pseudoShape on any string: it returns the
// captured commit when the string (less its "+" suffix) has that shape,
// and "" otherwise.
func FuzzPseudoRevision(f *testing.F) {
	for _, s := range []string{
		"v0.0.0-20261112233516-c1b8296f0c55", "v0.3.1-0.20261112233516-c1b8296f0c55+dirty",
		"v0.4.0-rc.1.0.20261112233516-c1b8296f0c55", "v1.0.0-c1b8296f0c55", "v0.3.0", "(devel)", "",
		"-", "+", "20261112233516-c1b8296f0c55", "v0.0.0-2026111223351-c1b8296f0c55",
		"v0.0.0-20261112233516-C1B8296F0C55", "v0.0.0-20261112233516-c1b8296f0c5", "v0.0.0-20261112233516-c1b8296f0c55-",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, v string) {
		got := pseudoRevision(v)
		base, _, _ := strings.Cut(v, "+")
		want := ""
		if m := pseudoShape.FindStringSubmatch(base); m != nil {
			want = m[1]
		}
		if got != want {
			t.Fatalf("pseudoRevision(%q) = %q, want %q", v, got, want)
		}
	})
}

// versionText prints three lines whatever the build information holds:
// "arv " and the first version known, "commit " and the first commit known
// (or "unknown"), and the toolchain. "(modified)" follows only the commit
// the go command stamped in a changed tree.
func FuzzVersionText(f *testing.F) {
	f.Add("", "", "(devel)", "", false, true)
	f.Add("v0.3.0", sha, "v0.0.0-20261112233516-c1b8296f0c55", sha, true, true)
	f.Add("", "", "v0.0.0-20261112233516-c1b8296f0c55+dirty", sha, true, true)
	f.Add("", "", "v0.3.1-0.20261112233516-c1b8296f0c55", "", false, true)
	f.Add("", "", "", "", false, false)
	f.Fuzz(func(t *testing.T, ldVersion, ldCommit, mainVersion, rev string, modified, haveInfo bool) {
		for _, s := range []string{ldVersion, ldCommit, mainVersion, rev} {
			if strings.ContainsAny(s, "\n") {
				return // link-time values and stamps are one line each
			}
		}
		var bi *debug.BuildInfo
		if haveInfo {
			bi = &debug.BuildInfo{Main: debug.Module{Version: mainVersion}}
			if rev != "" {
				bi.Settings = append(bi.Settings, debug.BuildSetting{Key: "vcs.revision", Value: rev})
			}
			m := "false"
			if modified {
				m = "true"
			}
			bi.Settings = append(bi.Settings, debug.BuildSetting{Key: "vcs.modified", Value: m})
		} else {
			mainVersion, rev, modified = "", "", false
		}
		lines := strings.Split(versionText(ldVersion, ldCommit, bi), "\n")
		if len(lines) != 4 || lines[3] != "" || lines[2] != runtime.Version()+" "+runtime.GOOS+"/"+runtime.GOARCH {
			t.Fatalf("not three lines ending in the toolchain: %q", lines)
		}
		wantVer := ldVersion
		if wantVer == "" {
			wantVer = mainVersion
		}
		if wantVer == "" {
			wantVer = "(devel)"
		}
		if lines[0] != "arv "+wantVer {
			t.Fatalf("version line %q, want %q", lines[0], "arv "+wantVer)
		}
		wantRev := ldCommit
		if wantRev == "" {
			wantRev = rev
		}
		if wantRev == "" {
			wantRev = pseudoRevision(mainVersion)
		}
		want := "commit unknown"
		if wantRev != "" {
			want = "commit " + wantRev
			if modified && wantRev == rev {
				want += " (modified)"
			}
		}
		if lines[1] != want {
			t.Fatalf("commit line %q, want %q", lines[1], want)
		}
	})
}
