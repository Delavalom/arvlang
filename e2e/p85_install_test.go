package e2e

import (
	"archive/zip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// modulePath is the module cmd/arv belongs to: cmdArv less "/cmd/arv".
var modulePath = strings.TrimSuffix(cmdArv, "/cmd/arv")

// proxyModule is one version of the module a file proxy serves.
type proxyModule struct {
	version string // a module version, such as v0.8.5 or a pseudo-version
	time    string // its commit time, RFC 3339
}

// writeProxy lays out a GOPROXY directory (the go command's file:// proxy
// protocol) that serves the sources of cmd/arv, as sourceFiles lists them,
// at each version given. tags is the @v/list (the versions @latest picks
// from); latest, when not empty, is what @latest answers when the list is
// empty, as a proxy does for a module without tags. It returns the
// GOPROXY value.
func writeProxy(t *testing.T, tags []string, latest string, mods ...proxyModule) string {
	t.Helper()
	dir := t.TempDir()
	at := filepath.Join(dir, filepath.FromSlash(modulePath), "@v")
	gomod, err := os.ReadFile("../go.mod")
	if err != nil {
		t.Fatal(err)
	}
	files := sourceFiles(t, "..")
	for _, m := range mods {
		info := `{"Version":"` + m.version + `","Time":"` + m.time + `"}`
		mustWrite(t, filepath.Join(at, m.version+".info"), info)
		mustWrite(t, filepath.Join(at, m.version+".mod"), string(gomod))
		if m.version == latest {
			mustWrite(t, filepath.Join(dir, filepath.FromSlash(modulePath), "@latest"), info)
		}
		f, err := os.Create(filepath.Join(at, m.version+".zip"))
		if err != nil {
			t.Fatal(err)
		}
		zw := zip.NewWriter(f)
		for _, name := range files {
			src, err := os.ReadFile(filepath.Join("..", filepath.FromSlash(name)))
			if err != nil {
				t.Fatal(err)
			}
			w, err := zw.Create(modulePath + "@" + m.version + "/" + name)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := w.Write(src); err != nil {
				t.Fatal(err)
			}
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
	}
	list := ""
	for _, tag := range tags {
		list += tag + "\n"
	}
	mustWrite(t, filepath.Join(at, "list"), list)
	return "file://" + filepath.ToSlash(dir)
}

// goInstall runs go install pkg outside any module, with modules fetched
// only from proxy into a module cache of its own, and returns GOBIN.
func goInstall(t *testing.T, proxy, pkg string) string {
	t.Helper()
	gobin, cache := t.TempDir(), t.TempDir()
	cmd := exec.Command("go", append(append([]string{"install"}, buildFlags()...), pkg)...)
	cmd.Dir = t.TempDir()
	cmd.Env = cleanEnv("GOPROXY="+proxy, "GOSUMDB=off", "GONOSUMDB=", "GOPRIVATE=", "GONOPROXY=",
		"GOINSECURE=", "GOFLAGS=-modcacherw", "GOMODCACHE="+cache, "GOBIN="+gobin,
		"GOTOOLCHAIN=local", "GOWORK=off", "GO111MODULE=on")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go install %s: %v\n%s", pkg, err, out)
	}
	return gobin
}

// The install line of README, go install .../cmd/arv@latest, run as a user
// would run it (outside a checkout, the module fetched from a proxy, here
// a local file proxy so nothing is downloaded): GOBIN then holds one
// binary, named arv, which says which version and commit it is and runs
// programs as the suite's binary does (checkDistBinary). A tag names no commit; @latest of a
// module without tags, and @pseudo-version, name the commit's first 12 hex
// digits.
func TestP85GoInstallLatest(t *testing.T) {
	t.Parallel()
	const tag, sha12 = "v0.8.5", "c1b8296f0c55"
	pseudo := "v0.0.0-20261112233516-" + sha12
	tagMod := proxyModule{tag, "2026-11-13T10:00:00Z"}
	pseudoMod := proxyModule{pseudo, "2026-11-12T23:35:16Z"}
	for _, c := range []struct {
		name, at, wantVer, wantCommit string
		tags                          []string
		latest                        string
	}{
		{"latest tag", "latest", tag, "unknown", []string{tag}, ""},
		{"latest without tags", "latest", pseudo, sha12, nil, pseudo},
		{"pseudo-version", pseudo, pseudo, sha12, []string{tag}, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			proxy := writeProxy(t, c.tags, c.latest, tagMod, pseudoMod)
			gobin := goInstall(t, proxy, cmdArv+"@"+c.at)
			entries, err := os.ReadDir(gobin)
			if err != nil || len(entries) != 1 || entries[0].Name() != "arv" {
				t.Fatalf("GOBIN holds %v (%v), want just arv", entries, err)
			}
			bin := filepath.Join(gobin, "arv")
			if ver, commit := version(t, bin); ver != c.wantVer || commit != c.wantCommit {
				t.Fatalf("version %q, commit %q; want %q and %q", ver, commit, c.wantVer, c.wantCommit)
			}
			out, err := exec.Command("go", "version", "-m", bin).Output()
			if err != nil {
				t.Fatalf("go version -m: %v", err)
			}
			for _, want := range []string{"\tpath\t" + cmdArv + "\n", "\tmod\t" + modulePath + "\t" + c.wantVer + "\t"} {
				if !strings.Contains(string(out), want) {
					t.Errorf("go version -m lacks %q:\n%s", want, out)
				}
			}
			checkDistBinary(t, bin)
		})
	}
}

// README's install line names the package the suite builds, at @latest.
func TestP85ReadmeInstallLine(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile("../README.md")
	if err != nil {
		t.Fatal(err)
	}
	want := "go install " + cmdArv + "@latest"
	var installs []string
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "go install ") {
			installs = append(installs, line)
		}
	}
	if len(installs) != 1 || !strings.HasPrefix(installs[0], want+" ") {
		t.Fatalf("README's install lines are %q, want one starting %q", installs, want)
	}
}
