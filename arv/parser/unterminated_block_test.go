package parser

import (
	"strings"
	"testing"
)

// A block the input ends inside is a parse error. Before, the parser
// closed it silently at EOF, so a program missing a closing brace ran
// with another meaning: `let f = fn() { 3\nputs(f())` put the puts inside
// f and printed nothing, and `scope (supervise: "collect") { ...` with no
// } ran. Found by FuzzScopeOptionsSyntax (a scope option whose value was
// `scope(A:0){0`).
func TestUnterminatedBlockIsAnError(t *testing.T) {
	for _, src := range []string{
		"let f = fn() { 3\nputs(f())",
		"scope { puts(2)",
		"scope (supervise: \"collect\") { spawn f(); 1",
		"if (x) { 1",
		"if (x) { 1 } else { 2",
		"for x in xs { x",
		"match x { ok(v) => fn() { v, err(e) => e }",
	} {
		_, errs := fuzzParse(src)
		found := false
		for _, e := range errs {
			if strings.HasPrefix(e, "expected next token to be }, got EOF instead (at ") {
				found = true
			}
		}
		if !found {
			t.Errorf("%q: errors %q, want one for the unclosed block", src, errs)
		}
	}
}

// The error is at the end of the input, after the last token.
func TestUnterminatedBlockErrorPosition(t *testing.T) {
	_, errs := fuzzParse("puts(1);\nscope { puts(2)")
	want := "expected next token to be }, got EOF instead (at 2:16)"
	if len(errs) != 1 || errs[0] != want {
		t.Fatalf("errors %q, want [%q]", errs, want)
	}
}

// Blocks nested inside each other that the input ends in give one error,
// not one per block: they all end at the same place.
func TestNestedUnterminatedBlocksGiveOneError(t *testing.T) {
	_, errs := fuzzParse("let f = fn() { if (x) { scope { 1")
	want := "expected next token to be }, got EOF instead (at 1:34)"
	if len(errs) != 1 || errs[0] != want {
		t.Fatalf("errors %q, want [%q]", errs, want)
	}
}
