package check

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestManifestReadsTheHeader(t *testing.T) {
	src := "// Fetch a page.\r\n" +
		"//\n" +
		"// arv:allow net=api.github.com\n" +
		"\n" +
		"  //arv:allow\tfs_write=./out  \n" +
		"// arv:allowed is not the directive\n" +
		"let x = 1;\n" +
		"// arv:allow env=HOME\n"
	got, err := Manifest(src)
	if err != nil {
		t.Fatal(err)
	}
	want := []Entry{{3, "net=api.github.com"}, {5, "fs_write=./out"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Manifest = %+v, want %+v", got, want)
	}
}

func TestManifestRejectsMalformedEntries(t *testing.T) {
	for _, src := range []string{
		"// arv:allow\nputs(1);",
		"// arv:allow   \n",
		"// arv:allow net=a net=b\n",
	} {
		if _, err := Manifest(src); err == nil || !strings.Contains(err.Error(), "line 1: arv:allow wants one kind=scope") {
			t.Errorf("Manifest(%q) = %v", src, err)
		}
	}
}

func TestNoHeaderNoEntries(t *testing.T) {
	for _, src := range []string{"", "puts(1);", "puts(1);\n// arv:allow net=x\n"} {
		got, err := Manifest(src)
		if err != nil || got != nil {
			t.Errorf("Manifest(%q) = %v, %v", src, got, err)
		}
	}
}

func TestGrantsParseAsAllowDoes(t *testing.T) {
	dir := t.TempDir()
	caps, err := Grants([]Entry{{1, "net=API.github.com"}, {2, "fs_read=data"}}, dir)
	if err != nil {
		t.Fatal(err)
	}
	canon, _ := filepath.EvalSymlinks(dir) // macOS: /var is /private/var
	if caps[0].String() != `net("api.github.com")` || caps[1].Scope != filepath.Join(canon, "data") {
		t.Errorf("Grants = %v", caps)
	}
	if _, err := Grants([]Entry{{7, "disk=/"}}, dir); err == nil || !strings.HasPrefix(err.Error(), "line 7: ") {
		t.Errorf("Grants(disk=/) = %v", err)
	}
}

func TestOutside(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "out"), 0o755); err != nil {
		t.Fatal(err)
	}
	r, err := Source(`http_get("https://api.github.com/zen");
http_get("https://evil.example/");
write_file("out/report.json", "x");
read_file("secret.txt");
getenv("HOME");
http_get("https://evil.example/again");`)
	if err != nil {
		t.Fatal(err)
	}
	grants, err := Grants([]Entry{{1, "net=api.github.com"}, {2, "fs_write=out"}, {3, "env=HOME"}}, dir)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, n := range Outside(r, grants, dir) {
		got = append(got, n.String()+" "+n.Fn)
	}
	want := []string{"net=evil.example:443 http_get", "fs_read=secret.txt read_file"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Outside = %q, want %q", got, want)
	}
}
