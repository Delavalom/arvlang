# Phase 8.5, task p85.3: goreleaser

> **For agentic workers:** this file is one task, p85.3. Do it exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: p85.1 and p85.2 are committed on `phase/p85` in `/Users/delavalom/delavalom-labs/arvlang-wt/p85` (`cmd/arv/version.go` declares `version` and `commit`; `e2e/p85_version_test.go` has the helper `version(t, bin)`). Start every shell command with `cd /Users/delavalom/delavalom-labs/arvlang-wt/p85 &&`. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green (first check `df -h /System/Volumes/Data`: below 1 GB free, stop and report "blocked: disk"; the only thing you may delete is the build cache, `go clean -cache`). End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify`, never push, never tag, and never run `goreleaser release` (goreleaser is not installed; do not install it). Keep Go files under 15 KB and markdown under 18 KB. Create and edit files with the Write and Edit tools, not shell heredocs. The phase overview, guarantees D1-D6 and decisions are in `p85-plan-1.md`.

## Task p85.3: goreleaser

Covers D5 (and D4 for release builds).

`go build -ldflags=-X main.name=value` silently does nothing when `main.name` does not exist, so a misspelt variable would ship binaries that print `commit unknown`. The test therefore reads `.goreleaser.yaml`, fills its template fields, builds exactly as goreleaser would for this machine (its `main`, `flags`, `ldflags` and `CGO_ENABLED=0`) and checks what `arv version` prints. goreleaser's `{{ .Version }}` has no leading `v` (tag `v0.3.0` gives `0.3.0`), so the ldflags write `v{{ .Version }}`, and a release prints `arv v0.3.0` as `go install ...@v0.3.0` does.

**Files:**
- Create: `e2e/p85_release_test.go`, `.goreleaser.yaml`
- Modify: `.gitignore`, `README.md`

- [ ] **Step 1: Write the failing test**

Create `e2e/p85_release_test.go`:

```go
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
// version and commit goreleaser passes it.
func TestP85ReleaseBuild(t *testing.T) {
	t.Parallel()
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
	for _, want := range []string{"goos: [darwin, linux]", "CGO_ENABLED=0"} {
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
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	if ver, commit := version(t, bin); ver != "v9.8.7" || commit != sha {
		t.Fatalf("version %q, commit %q; want v9.8.7 and %s", ver, commit, sha)
	}
}
```

- [ ] **Step 2: Run it and watch it fail**

```bash
cd /Users/delavalom/delavalom-labs/arvlang-wt/p85 && go vet ./e2e/ && go test -count=1 -run TestP85ReleaseBuild ./e2e/
```

Expected: FAIL, `open ../.goreleaser.yaml: no such file or directory`.

- [ ] **Step 3: Write the config**

Create `.goreleaser.yaml` in the repository root:

```yaml
# goreleaser (https://goreleaser.com, v2) builds arv for macOS and Linux.
#
#   goreleaser check                     # validate this file
#   goreleaser build --snapshot --clean  # build every target into dist/
#
# Release builds name their version and commit through the link-time
# variables of cmd/arv/version.go; e2e/p85_release_test.go builds with the
# ldflags below and checks what arv version prints. Keep each of main,
# binary, flags and ldflags on one line: that test reads them.
version: 2

project_name: arv

builds:
  - id: arv
    main: ./cmd/arv
    binary: arv
    env:
      - CGO_ENABLED=0
    goos: [darwin, linux]
    goarch: [amd64, arm64]
    flags:
      - -trimpath
    ldflags:
      - -s -w -X main.version=v{{ .Version }} -X main.commit={{ .FullCommit }}
    mod_timestamp: "{{ .CommitTimestamp }}"

archives:
  - formats: [tar.gz]
    name_template: "{{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}"
    files:
      - README.md

checksum:
  name_template: checksums.txt

snapshot:
  version_template: "{{ incpatch .Version }}-next"

# A release is a draft until someone publishes it by hand.
release:
  draft: true
```

Append to `.gitignore`:

```text

# goreleaser output (.goreleaser.yaml)
dist/
```

- [ ] **Step 4: Run it and watch it pass**

```bash
cd /Users/delavalom/delavalom-labs/arvlang-wt/p85 && gofmt -l . && go vet ./... && go test -race -count=1 -run TestP85 -v ./e2e/ 2>&1 | grep -- '^--- \|^ok\|FAIL'
```

Expected: five `--- PASS` lines (`TestP85CommandInCmdArv`, `TestP85MakeBuild`, `TestP85ReleaseBuild`, `TestP85VersionOfSuiteBinary`, `TestP85VersionFromGit`), then `ok`.

Mutation check (undo each before the next): in `.goreleaser.yaml` write `-X main.Commit=` for `-X main.commit=`: `TestP85ReleaseBuild` fails (the commit printed is not the test's `0123...4567`). Write `main: .` for `main: ./cmd/arv`: it fails (`builds . as arv`). Delete `v` before `{{ .Version }}`: it fails (`version "9.8.7"`).

The four release targets build (decision 5 of `p85-plan-1.md`; this is what `goreleaser build` would compile):

```bash
cd /Users/delavalom/delavalom-labs/arvlang-wt/p85 && for t in darwin/amd64 darwin/arm64 linux/amd64 linux/arm64; do CGO_ENABLED=0 GOOS=${t%/*} GOARCH=${t#*/} go build -trimpath -o /dev/null ./cmd/arv && echo "$t ok"; done
```

Expected: four `ok` lines.

- [ ] **Step 5: README**

In `README.md`, section `## Development`, after the code block that ends with `make bench-cpu  # CPU-bound speed programs (Phase 8.4)` and its closing fence, add a blank line and:

```markdown
Release binaries for macOS and Linux (amd64 and arm64) are built by [goreleaser](https://goreleaser.com) from `.goreleaser.yaml`: `goreleaser build --snapshot --clean` writes them to `dist/` without publishing anything. They are built with CGO off and name their version and commit through link-time variables, which `arv version` prints.
```

- [ ] **Step 6: The whole suite, and commit**

```bash
cd /Users/delavalom/delavalom-labs/arvlang-wt/p85 && df -h /System/Volumes/Data && go test -race ./...
```

Expected: every package `ok`.

```bash
cd /Users/delavalom/delavalom-labs/arvlang-wt/p85 && git add -A && git status --short && git commit -m "build: goreleaser config for macOS and Linux binaries

.goreleaser.yaml builds ./cmd/arv as arv for darwin and linux, amd64 and
arm64, CGO off, with the version and commit as link-time variables.
Releases are drafts. TestP85ReleaseBuild builds with the config's own
main, flags and ldflags and checks what arv version prints, since -X on
a missing variable is silently ignored. goreleaser itself is not run.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
