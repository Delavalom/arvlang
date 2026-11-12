package check

import (
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestConstantsAreFolded(t *testing.T) {
	needs, notes := checked(t, `let base = "https://api.example.com";
let dir = "out";
let file = dir + "/" + "report.json";
http_get(base + "/v1/status");
write_file(file, "x");
let f = fn() { getenv(name) };
let name = "HOME";`)
	want := []string{"net=api.example.com:443", "fs_write=out/report.json", "env=HOME"}
	if !reflect.DeepEqual(needs, want) || notes != nil {
		t.Errorf("needs = %q, notes = %q; want %q", needs, notes, want)
	}
}

// A name bound more than once anywhere, by any binding form, is not a
// constant: some scope may see the other value.
func TestReboundNamesAreNotConstants(t *testing.T) {
	for _, src := range []string{
		`let u = "http://a/"; let u = "http://b/"; http_get(u);`,
		`let u = "http://a/"; let f = fn(u) { http_get(u) };`,
		`let u = "http://a/"; for u in ["http://b/"] { http_get(u) };`,
		`let u = "http://a/"; match 1 { ok(u) => http_get(u), err(e) => e };`,
		`let u = "http://a/"; match 1 { ok(v) => v, err(u) => http_get(u) };`,
		`let f = fn() { let u = "http://a/"; http_get(u) }; let u = 1;`,
	} {
		needs, notes := checked(t, src)
		if needs != nil || len(notes) != 1 || !strings.Contains(notes[0], "host not statically known") {
			t.Errorf("%s\nneeds = %q, notes = %q", src, needs, notes)
		}
	}
}

func TestOnlyStringPlusFolds(t *testing.T) {
	for _, src := range []string{
		`let u = "a" - "b"; getenv(u);`,
		`let u = "a" + 1; getenv(u);`,
		`let u = len("a"); getenv(u);`,
		`let u = u + "x"; getenv(u);`,
		`let a = b + "x"; let b = a + "y"; getenv(a);`,
	} {
		needs, notes := checked(t, src)
		if needs != nil || len(notes) != 1 {
			t.Errorf("%s\nneeds = %q, notes = %q", src, needs, notes)
		}
	}
}

// Doubling a string 60 times would be an exabyte: folding gives up past
// maxConstLen and maxFolded, and the call is noted instead.
func TestFoldingIsBounded(t *testing.T) {
	var b strings.Builder
	b.WriteString(`let s0 = "ab";` + "\n")
	for i := 1; i <= 60; i++ {
		b.WriteString("let s" + strconv.Itoa(i) + " = s" + strconv.Itoa(i-1) + " + s" + strconv.Itoa(i-1) + ";\n")
	}
	for i := 0; i <= 60; i++ {
		b.WriteString("getenv(s" + strconv.Itoa(i) + ");\n")
	}
	start := time.Now()
	r, err := Source(b.String())
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 20*time.Second {
		t.Errorf("Source took %s", time.Since(start))
	}
	if len(r.Needs) == 0 || len(r.Notes) == 0 || len(r.Needs)+len(r.Notes) != 61 {
		t.Errorf("%d needs and %d notes, want some of each and 61 in all", len(r.Needs), len(r.Notes))
	}
	for _, n := range r.Needs {
		if len(n.Scope) > maxConstLen {
			t.Errorf("a need of %d bytes", len(n.Scope))
		}
	}
}

// A long chain of names is followed only maxHops deep.
func TestNameChainsAreBounded(t *testing.T) {
	var b strings.Builder
	b.WriteString(`let n0 = "A";` + "\n")
	for i := 1; i <= 5000; i++ {
		b.WriteString("let n" + strconv.Itoa(i) + " = n" + strconv.Itoa(i-1) + ";\n")
	}
	b.WriteString("getenv(n5000); getenv(n50);")
	needs, notes := checked(t, b.String())
	if !reflect.DeepEqual(needs, []string{"env=A"}) || len(notes) != 1 {
		t.Errorf("needs = %q, notes = %q", needs, notes)
	}
}
