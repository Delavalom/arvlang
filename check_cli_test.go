package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runArv runs the CLI with args and returns its exit code and output.
func runArv(args ...string) (int, string, string) {
	var out, errOut bytes.Buffer
	code := runCLI(args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestCheckPrintsGrantsAndNotes(t *testing.T) {
	script := writeScript(t, `let get = fn(url) { http_get(url) };
let t = spawn get("https://api.github.com/zen") with [net("api.github.com")];
puts(getenv("HOME"), read_file("data/in.txt"));`)
	code, out, errOut := runArv("check", script)
	want := "--allow net=api.github.com\n--allow fs_read=data/in.txt\n--allow env=HOME\n" +
		"# http_get at 1:21: host not statically known\n"
	if code != 0 || out != want || errOut != "" {
		t.Fatalf("exit %d\nstdout %q\nwant   %q\nstderr %q", code, out, want, errOut)
	}
}

// check reads the program; it never runs it.
func TestCheckDoesNotRunTheProgram(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	script := writeScript(t, `write_file("made.txt", "x"); puts("ran");
let f = fn() { f() }; f();`)
	code, out, errOut := runArv("check", script)
	if code != 0 || out != "--allow fs_write=made.txt\n" || errOut != "" {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errOut)
	}
	if _, err := os.Stat(filepath.Join(dir, "made.txt")); !os.IsNotExist(err) {
		t.Fatalf("check created made.txt: %v", err)
	}
}

// A scope that would not survive the shell, or a second line, is quoted.
func TestCheckQuotesOddScopes(t *testing.T) {
	script := writeScript(t, "getenv(\"A B\"); getenv(\"X\n--allow fs_read=/\");")
	code, out, _ := runArv("check", script)
	want := "--allow $'env=A B'\n--allow $'env=X\\x0a--allow fs_read=/'\n"
	if code != 0 || out != want {
		t.Fatalf("exit %d, stdout %q, want %q", code, out, want)
	}
}

func TestCheckWithAManifest(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	covered := writeScript(t, "// arv:allow net=api.github.com\n// arv:allow env=HOME\n"+
		`http_get("https://api.github.com/zen"); getenv("HOME");`)
	if code, out, errOut := runArv("check", covered); code != 0 || errOut != "" {
		t.Fatalf("covered: exit %d, stdout %q, stderr %q", code, out, errOut)
	}
	outside := writeScript(t, "// arv:allow net=api.github.com\n"+
		`http_get("https://api.github.com/zen");`+"\n"+`getenv("HOME"); read_file("x");`)
	code, out, errOut := runArv("check", outside)
	if code != 1 || out != "--allow net=api.github.com:443\n--allow fs_read=x\n--allow env=HOME\n" {
		t.Fatalf("outside: exit %d, stdout %q", code, out)
	}
	want := "error: " + outside + " needs env=HOME (getenv at 3:1), fs_read=x (read_file at 3:17), outside its arv:allow manifest\n"
	if errOut != want {
		t.Fatalf("stderr %q\nwant   %q", errOut, want)
	}
	bad := writeScript(t, "// arv:allow disk=/\nputs(1);")
	if code, _, errOut := runArv("check", bad); code != 1 || !strings.Contains(errOut, "line 1: ") {
		t.Fatalf("bad manifest: exit %d, stderr %q", code, errOut)
	}
}

func TestCheckUsageAndErrors(t *testing.T) {
	for _, args := range [][]string{{"check"}, {"check", "a.arv", "b.arv"}, {"check", "--allow", "net=x"}} {
		if code, _, errOut := runArv(args...); code != 2 || !strings.HasPrefix(errOut, "usage:") {
			t.Errorf("%q: exit %d, stderr %q", args, code, errOut)
		}
	}
	if code, _, errOut := runArv("check", filepath.Join(t.TempDir(), "missing.arv")); code != 1 || !strings.HasPrefix(errOut, "error: ") {
		t.Errorf("missing file: exit %d, stderr %q", code, errOut)
	}
	script := writeScript(t, "let = ;")
	code, _, errOut := runArv("check", script)
	if code != 1 || !strings.HasPrefix(errOut, "error: parse error: ") || !strings.Contains(errOut, script+":1:") {
		t.Errorf("parse error: exit %d, stderr %q", code, errOut)
	}
}
