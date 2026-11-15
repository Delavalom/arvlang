package check

import (
	"reflect"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/ast"
)

// checked is src's report as two lists: the needs in --allow form and
// the notes.
func checked(t *testing.T, src string) (needs, notes []string) {
	t.Helper()
	r, err := Source(src)
	if err != nil {
		t.Fatalf("Source(%q): %v", src, err)
	}
	for _, n := range r.Needs {
		needs = append(needs, n.String())
	}
	for _, n := range r.Notes {
		notes = append(notes, n.String())
	}
	return needs, notes
}

func TestLiteralCallsBecomeNeeds(t *testing.T) {
	needs, notes := checked(t, `
read_file("data/in.txt");
write_file("out/report.json", "x");
getenv("HOME");
http_get("https://example.com/zen");
let t = spawn f() with [net("api.github.com"), fs_read("/etc"), fs_write("out"), env("USER")];
`)
	want := []string{
		"net=api.github.com", "net=example.com:443",
		"fs_read=/etc", "fs_read=data/in.txt",
		"fs_write=out", "fs_write=out/report.json",
		"env=HOME", "env=USER",
	}
	if !reflect.DeepEqual(needs, want) || notes != nil {
		t.Errorf("needs = %q, notes = %q\nwant needs %q", needs, notes, want)
	}
}

// Every syntactic position an expression can take is searched: the
// report covers code that never runs, too.
func TestEverySyntaxFormIsSearched(t *testing.T) {
	for _, src := range []string{
		`let x = getenv("A");`,
		`fn() { return getenv("A") };`,
		`-len(getenv("A"));`,
		`!getenv("A");`,
		`1 + len(getenv("A"));`,
		`len(getenv("A")) + 1 + 2 + 3;`,
		`if (getenv("A") == "") { 1 };`,
		`if (true) { getenv("A") };`,
		`if (false) { 1 } else { getenv("A") };`,
		`let f = fn(x) { getenv("A") };`,
		`len(getenv("A"));`,
		`[1, getenv("A")];`,
		`[1][len(getenv("A"))];`,
		`{"k": 1}[getenv("A")];`,
		`{"k": getenv("A")};`,
		`{getenv("A"): 1};`,
		`scope { getenv("A") };`,
		`scope (timeout_ms: len(getenv("A"))) { 1 };`,
		`scope (supervise: "collect") { await spawn f() with [env("A")] };`,
		`scope (supervise: "restart", retries: 2, retry_on: [getenv("A")]) { 1 };`,
		`spawn f(getenv("A"));`,
		`spawn f() with [env("A")];`,
		`await spawn f() with [env("A")];`,
		`await getenv("A");`,
		`for x in [getenv("A")] { x };`,
		`for x in [1] { getenv("A") };`,
		`match getenv("A") { ok(v) => v, err(e) => e };`,
		`match 1 { ok(v) => getenv("A"), err(e) => e };`,
		`match 1 { ok(v) => v, err(e) => getenv("A") };`,
		`match scope (supervise: "restart") { await spawn f() with [env("A")] } { ok(v) => v, err(e) => e };`,
	} {
		needs, notes := checked(t, src)
		if !reflect.DeepEqual(needs, []string{"env=A"}) || notes != nil {
			t.Errorf("%s\nneeds = %q, notes = %q; want [env=A]", src, needs, notes)
		}
	}
}

func TestUnknownArgumentsAreNoted(t *testing.T) {
	needs, notes := checked(t, `let get = fn(url) { http_get(url) };
let load = fn(name) { read_file(name + ".txt") };
let check = fn(host) { spawn get(host) with [net(host)] };
getenv(1);`)
	want := []string{
		"http_get at 1:21: host not statically known",
		"read_file at 2:23: path not statically known",
		"net at 3:46: scope not statically known",
		"getenv at 4:1: name not statically known",
	}
	if needs != nil || !reflect.DeepEqual(notes, want) {
		t.Errorf("needs = %q\nnotes = %q\nwant  %q", needs, notes, want)
	}
}

// A builtin passed around as a value can be called with anything.
func TestBuiltinUsedAsAValueIsNoted(t *testing.T) {
	needs, notes := checked(t, `let get = http_get;
let apply = fn(f, x) { f(x) };
apply(read_file, "a");
{"f": getenv}["f"]("HOME");
let net = fn(x) { x };`)
	want := []string{
		"http_get at 1:11: used as a value: what it needs is not statically known",
		"read_file at 3:7: used as a value: what it needs is not statically known",
		"getenv at 4:7: used as a value: what it needs is not statically known",
	}
	if needs != nil || !reflect.DeepEqual(notes, want) {
		t.Errorf("needs = %q\nnotes = %q\nwant  %q", needs, notes, want)
	}
}

// A user binding that shadows an effect's name is still treated as the
// effect: that can only over-report.
func TestShadowedNamesOverReport(t *testing.T) {
	needs, _ := checked(t, `let getenv = fn(x) { x }; getenv("A");`)
	if !reflect.DeepEqual(needs, []string{"env=A"}) {
		t.Errorf("needs = %q", needs)
	}
}

func TestCallsTheRuntimeRefusesAreNoted(t *testing.T) {
	needs, notes := checked(t, `http_get("ftp://x/");
http_get("a", "b");
read_file("");
net("a b");
write_file("x");`)
	if needs != nil || len(notes) != 5 {
		t.Fatalf("needs = %q, notes = %q", needs, notes)
	}
	for _, n := range notes {
		if !strings.Contains(n, ": never allowed: ") {
			t.Errorf("note %q does not say never allowed", n)
		}
	}
	if notes[1] != "http_get at 2:1: never allowed: 2 arguments (it takes 1)" {
		t.Errorf("notes[1] = %q", notes[1])
	}
}

func TestPureProgramNeedsNothing(t *testing.T) {
	needs, notes := checked(t, `let f = fn(n) { if (n == 0) { 0 } else { f(n - 1) } };
puts(len(split("a,b", ",")), json([1]), clock_ms()); sleep(1); error("x");`)
	if needs != nil || notes != nil {
		t.Errorf("needs = %q, notes = %q", needs, notes)
	}
}

func TestParseErrorIsReturned(t *testing.T) {
	_, err := Source(`let = ;`)
	pe, ok := err.(*ParseError)
	if !ok || len(pe.Errs) == 0 {
		t.Fatalf("Source = %v, want a *ParseError", err)
	}
}

// A long infix chain is walked without a Go frame per term.
func TestLongInfixChain(t *testing.T) {
	src := "getenv(\"A\")" + strings.Repeat(" + 1", 200000) + ";"
	needs, _ := checked(t, src)
	if !reflect.DeepEqual(needs, []string{"env=A"}) {
		t.Errorf("needs = %q", needs)
	}
}

// Program fails closed on syntax it does not know (see walk.go).
func TestProgramFailsOnUnknownSyntax(t *testing.T) {
	prog := &ast.Program{Statements: []ast.Statement{
		&ast.ExpressionStatement{Expression: alienNode{}},
	}}
	if r, err := Program(prog); err == nil || r != nil {
		t.Fatalf("Program = %+v, %v; want an error", r, err)
	}
}
