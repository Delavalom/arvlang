package check

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLeastSetDropsCoveredNetAndEnvNeeds(t *testing.T) {
	needs, _ := checked(t, `
let t = spawn f() with [net("api.github.com"), net("*.example.com"), env("*")];
http_get("https://api.github.com/zen");
http_get("http://api.github.com:8080/");
http_get("https://www.example.com/");
http_get("http://127.0.0.1:9/");
getenv("HOME");`)
	want := []string{"net=*.example.com", "net=127.0.0.1:9", "net=api.github.com", "env=*"}
	if !reflect.DeepEqual(needs, want) {
		t.Errorf("needs = %q, want %q", needs, want)
	}
}

// The wildcard covers every host name but no internal address, as at run
// time, so the loopback request keeps its own grant.
func TestWildcardDoesNotCoverLoopback(t *testing.T) {
	needs, _ := checked(t, `spawn f() with [net("*")]; http_get("http://127.0.0.1/"); http_get("http://example.com/");`)
	want := []string{"net=*", "net=127.0.0.1:80"}
	if !reflect.DeepEqual(needs, want) {
		t.Errorf("needs = %q, want %q", needs, want)
	}
}

// Paths are compared as written: only exact duplicates go.
func TestPathsAreNotMergedBySubdirectory(t *testing.T) {
	needs, _ := checked(t, `spawn f() with [fs_write("out")]; write_file("out/a", ""); write_file("out/a", "");`)
	want := []string{"fs_write=out", "fs_write=out/a"}
	if !reflect.DeepEqual(needs, want) {
		t.Errorf("needs = %q, want %q", needs, want)
	}
}

func TestSitesKeepEveryCall(t *testing.T) {
	r, err := Source(`getenv("A"); getenv("A");`)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Sites) != 2 || len(r.Needs) != 1 || r.Sites[1].Col != 14 {
		t.Errorf("sites = %+v, needs = %+v", r.Sites, r.Needs)
	}
}

func TestQuoted(t *testing.T) {
	for _, tt := range []struct{ scope, want string }{
		{"api.github.com", "net=api.github.com"},
		{"[::1]:443", `$'net=[::1]:443'`}, // [ and ] are glob characters
		{"a[b]", `$'net=a[b]'`},
		{"my file", `$'net=my file'`},
		{"a'b", `$'net=a\'b'`},
		{"x\n--allow fs_read=/", `$'net=x\x0a--allow fs_read=/'`},
		{"*.example.com", `$'net=*.example.com'`},
	} {
		if got := (Need{Kind: "net", Scope: tt.scope}).Quoted(); got != tt.want {
			t.Errorf("Quoted(%q) = %s, want %s", tt.scope, got, tt.want)
		}
	}
}

// Each printed grant reads back, in bash and zsh, as exactly one word: the
// --allow argument itself, whatever files the directory holds.
func TestQuotedSurvivesTheShell(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"net=1:443", "fs_read=ab", "env=Ax"} { // what a glob would match
		if err := os.WriteFile(filepath.Join(dir, f), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	needs := []Need{
		{Kind: "net", Scope: "[::1]:443"}, {Kind: "fs_read", Scope: "a[b]"}, {Kind: "env", Scope: "A?"},
		{Kind: "env", Scope: "A*"}, {Kind: "fs_read", Scope: "x y'z\\\n$HOME`id`"}, {Kind: "net", Scope: "api.github.com"},
	}
	for _, sh := range []string{"bash", "zsh"} {
		path, err := exec.LookPath(sh)
		if err != nil {
			t.Logf("%s: not found", sh)
			continue
		}
		for _, n := range needs {
			cmd := exec.Command(path, "-c", "printf '%s\\n' "+n.Quoted())
			cmd.Dir = dir
			out, err := cmd.Output()
			if err != nil || string(out) != n.String()+"\n" {
				t.Errorf("%s: %s read back as %q, %v; want %q", sh, n.Quoted(), out, err, n.String())
			}
		}
	}
}
