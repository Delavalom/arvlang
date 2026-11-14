# Phase 8.5, task p85.2: arv version

> **For agentic workers:** this file is one task, p85.2. Do it exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: p85.1 is committed on `phase/p85` in `/Users/delavalom/delavalom-labs/arvlang-wt/p85` (the command is in `cmd/arv`; `e2e/harness_test.go` declares `cmdArv`). Start every shell command with `cd /Users/delavalom/delavalom-labs/arvlang-wt/p85 &&`. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green (first check `df -h /System/Volumes/Data`: below 1 GB free, stop and report "blocked: disk"; the only thing you may delete is the build cache, `go clean -cache`). End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify`, never push, never tag. Keep Go files under 15 KB and markdown under 18 KB. Create and edit files with the Write and Edit tools, not shell heredocs. The phase overview, guarantees D1-D6 and decisions are in `p85-plan-1.md`.

## Task p85.2: arv version

Covers D4.

`arv version` prints three lines:

```text
arv v0.0.0-20261112233516-c1b8296f0c55+dirty
commit c1b8296f0c55552966f28c02fad0fc2ea0a607da (modified)
go1.26.2 darwin/arm64
```

The version and commit come from the link-time variables `main.version` and `main.commit` when a release build sets them (p85.3), else from what the go command stamped (`debug.ReadBuildInfo`), else `(devel)` and `commit unknown`. **The go command does not stamp a git worktree** (it looks for a `.git` directory; a worktree's `.git` is a file), so in this worktree the suite's binary prints `commit unknown`, and the test that checks a real SHA builds in a fresh repository (decision 1 of `p85-plan-1.md`).

**Files:**
- Create: `cmd/arv/version.go`, `cmd/arv/version_test.go`, `e2e/p85_version_test.go`
- Modify: `cmd/arv/main.go` (usage text, `runCLI`, imports), `README.md`

- [ ] **Step 1: Write the failing unit tests**

Create `cmd/arv/version_test.go`:

```go
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
```

- [ ] **Step 2: Write the failing e2e tests**

Create `e2e/p85_version_test.go`. `git` runs with its own empty hooks directory and without the user's or the system's git configuration, so a developer's global hooks or signing settings cannot break the throwaway repository's commit (this is a test fixture, not a commit to this project). `-trimpath` lets later runs reuse the build cache although the repository's path changes. The second build only relinks: an untracked file changes `vcs.modified`, not the sources.

```go
package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// goEnv is a go env variable as the go that builds the suite's binaries
// sees it.
func goEnv(t *testing.T, name string) string {
	t.Helper()
	out, err := exec.Command("go", "env", name).Output()
	if err != nil {
		t.Fatalf("go env %s: %v", name, err)
	}
	return strings.TrimSpace(string(out))
}

// version runs bin version and returns its lines: the version, the commit
// and the toolchain, which must be the go that built bin.
func version(t *testing.T, bin string) (ver, commit string) {
	t.Helper()
	res := run(t, spec{args: []string{"version"}, bin: bin})
	lines := strings.Split(res.stdout, "\n")
	toolchain := goEnv(t, "GOVERSION") + " " + goEnv(t, "GOOS") + "/" + goEnv(t, "GOARCH")
	if res.code != 0 || res.stderr != "" || len(lines) != 4 || lines[3] != "" ||
		!strings.HasPrefix(lines[0], "arv ") || !strings.HasPrefix(lines[1], "commit ") || lines[2] != toolchain {
		t.Fatalf("want arv VERSION, commit SHA and %q, one per line\n%s", toolchain, res)
	}
	return strings.TrimPrefix(lines[0], "arv "), strings.TrimPrefix(lines[1], "commit ")
}

// git runs git in dir, isolated from the user's and the system's
// configuration and hooks, and returns its trimmed output.
func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "core.hooksPath=" + t.TempDir(), "-c", "commit.gpgsign=false"}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=arv e2e", "GIT_AUTHOR_EMAIL=e2e@example.invalid",
		"GIT_COMMITTER_NAME=arv e2e", "GIT_COMMITTER_EMAIL=e2e@example.invalid")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// The suite's binary names the commit it was built from, or says it does
// not know. A git worktree is not stamped by the go command, so there it
// is "unknown"; in a plain checkout it is HEAD.
func TestP85VersionOfSuiteBinary(t *testing.T) {
	t.Parallel()
	_, commit := version(t, arvBin)
	if commit == "unknown" {
		return
	}
	commit = strings.TrimSuffix(commit, " (modified)")
	if head := git(t, "..", "rev-parse", "HEAD"); len(commit) < 12 || !strings.HasPrefix(head, commit) {
		t.Fatalf("commit %q, want HEAD %s", commit, head)
	}
}

// Built in a git repository, arv version prints the commit HEAD names and
// the version the go command derives from it, and says "(modified)" once
// the tree has changes. The repository is a copy of the sources arv is
// built from, committed afresh: the checkout the suite runs in may be a
// worktree, which the go command does not stamp, or have uncommitted work.
func TestP85VersionFromGit(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	files := strings.Fields(git(t, "..", "ls-files", "-co", "--exclude-standard", "--", "go.mod", "arv/*.go", "cmd/arv/*.go"))
	repo := t.TempDir()
	copied := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") || strings.Contains(f, "/testdata/") {
			continue
		}
		src, err := os.ReadFile(filepath.Join("..", f))
		if os.IsNotExist(err) {
			continue // deleted from the worktree, not yet from the index
		}
		if err != nil {
			t.Fatal(err)
		}
		dst := filepath.Join(repo, f)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dst, src, 0o644); err != nil {
			t.Fatal(err)
		}
		copied++
	}
	if copied < 10 {
		t.Fatalf("copied %d files of %v", copied, files)
	}
	git(t, repo, "init", "-q")
	git(t, repo, "add", "-A")
	git(t, repo, "commit", "-q", "-m", "arv e2e")
	head := git(t, repo, "rev-parse", "HEAD")

	bin := filepath.Join(t.TempDir(), "arv")
	build := func() {
		t.Helper()
		cmd := exec.Command("go", append(append([]string{"build", "-buildvcs=true", "-trimpath", "-o", bin}, buildFlags()...), "./cmd/arv")...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("go build in a fresh repository: %v\n%s", err, out)
		}
	}
	build()
	ver, commit := version(t, bin)
	if commit != head || !strings.Contains(ver, head[:12]) || strings.HasSuffix(ver, "+dirty") {
		t.Fatalf("clean tree: version %q, commit %q; want a version naming %s and commit %s", ver, commit, head[:12], head)
	}

	if err := os.WriteFile(filepath.Join(repo, "notes.txt"), []byte("not committed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	build()
	ver, commit = version(t, bin)
	if commit != head+" (modified)" || !strings.HasSuffix(ver, "+dirty") {
		t.Fatalf("changed tree: version %q, commit %q; want +dirty and %s (modified)", ver, commit, head)
	}
}
```

- [ ] **Step 3: Run them and watch them fail**

```bash
cd /Users/delavalom/delavalom-labs/arvlang-wt/p85 && go vet ./cmd/arv/
```

Expected: `vet: cmd/arv/version_test.go:54:19: undefined: versionText` (and `version`, `commit` once that is fixed).

```bash
cd /Users/delavalom/delavalom-labs/arvlang-wt/p85 && go test -count=1 -run TestP85Version ./e2e/
```

Expected: FAIL in both tests: `arv version` prints the usage to stderr and exits 2, so `version` reports `want arv VERSION, commit SHA and "go1.26.2 darwin/arm64", one per line` with `exit code: 2`.

- [ ] **Step 4: Implement**

Create `cmd/arv/version.go`:

```go
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
```

In `cmd/arv/main.go`:

1. Add `"runtime/debug"` to the imports, between `"os"` and `"strconv"`.
2. In `usage`, after the line `  arv check FILE         print the --allow grants FILE can need, without running it`, add:

```text
  arv version            print the version, the git commit and the Go toolchain
```

3. In `runCLI`, between the `if args[0] == "check" { ... }` block and `if args[0] != "run" {`, add:

```go
	if args[0] == "version" {
		if len(args) != 1 {
			fmt.Fprintln(stderr, usage)
			return 2
		}
		bi, _ := debug.ReadBuildInfo()
		fmt.Fprint(stdout, versionText(version, commit, bi))
		return 0
	}
```

- [ ] **Step 5: Run the tests and watch them pass**

```bash
cd /Users/delavalom/delavalom-labs/arvlang-wt/p85 && gofmt -l . && go vet ./... && go test -race -count=1 ./cmd/arv/ && go test -race -count=1 -run TestP85 -v ./e2e/ 2>&1 | grep -- '^--- \|^ok\|FAIL'
```

Expected: `ok` for `cmd/arv`; `--- PASS` for `TestP85CommandInCmdArv`, `TestP85MakeBuild`, `TestP85VersionOfSuiteBinary` and `TestP85VersionFromGit` (about 11 s the first time, in the scratch copy), then `ok`. Also by hand: `go run ./cmd/arv version` prints `arv (devel)`, `commit unknown` and the toolchain (`go run` never stamps, and neither does a worktree build), and `go run ./cmd/arv version x` prints the usage and then go's `exit status 2`.

Mutation check before committing (restore each with `git checkout cmd/arv/version.go` or an undo): drop `&& rev == vcsRev` from the `(modified)` condition: `TestVersionText` fails ("release build from a checkout"). Make `pseudoRevision` return `""` always: `TestVersionText` fails (three "go install of a commit" rows). Swap `v, rev := ldVersion, ldCommit` for `v, rev := "", ""`: `TestVersionText` fails (release rows).

- [ ] **Step 6: README**

In `README.md`, section `## Running`: after the line `bin/arv check FILE               # the grants FILE can need; runs nothing` (inside the code block) add

```text
bin/arv version                  # the version, the git commit and the Go toolchain
```

and after that code block's closing fence add a blank line and this paragraph:

```markdown
`arv version` prints the commit the binary was built from: the one a release build names, or the one the go command stamps when it builds in a git checkout (with `(modified)` if the tree had changes). A build from a git worktree or with `-buildvcs=false`, and `go install` of a tag, print `commit unknown`; `go install` of a commit (`@latest` before the first tag, or `@<sha>`) prints its first 12 hex digits.
```

- [ ] **Step 7: The whole suite, and commit**

```bash
cd /Users/delavalom/delavalom-labs/arvlang-wt/p85 && df -h /System/Volumes/Data && go test -race ./...
```

Expected: every package `ok`.

```bash
cd /Users/delavalom/delavalom-labs/arvlang-wt/p85 && git add -A && git status --short && git commit -m "feat(cli): arv version prints the version, the git commit and the toolchain

The commit comes from a release build's link-time variables, else from
what the go command stamped (vcs.revision, with (modified) for a changed
tree), else from a pseudo-version; anything else is commit unknown. The
go command does not stamp a git worktree, so the e2e test builds in a
fresh repository and checks HEAD there, clean and changed.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
