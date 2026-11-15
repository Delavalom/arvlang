package arv

import (
	"errors"
	"fmt"
	"testing"
)

// A parse error keeps the parser's messages, and In names the file at
// each position, as runtime errors are reported: "(at file:line:col)".
func TestParseErrorNamesFile(t *testing.T) {
	_, err := Run("let a = 1;\nlet = 2;\n", Options{})
	var pe *ParseError
	if !errors.As(err, &pe) {
		t.Fatalf("err = %#v, want a *ParseError", err)
	}
	const msgs = "expected next token to be IDENT, got = instead (at %s2:5); no prefix parse function for = found (at %s2:5)"
	if want := "parse error: " + fmt.Sprintf(msgs, "", ""); err.Error() != want {
		t.Fatalf("Error() = %q, want %q", err.Error(), want)
	}
	if got, want := pe.In("pe.arv"), "parse error: "+fmt.Sprintf(msgs, "pe.arv:", "pe.arv:"); got != want {
		t.Fatalf("In = %q, want %q", got, want)
	}
}

// Only a trailing "(at line:col)" is a position: other messages are kept.
func TestParseErrorInLeavesOtherText(t *testing.T) {
	pe := &ParseError{Errs: []string{"nesting deeper than 3 levels", "a (at 1:2) b", "c (at x:2)", "d (at 3:4)"}}
	want := "parse error: nesting deeper than 3 levels; a (at 1:2) b; c (at x:2); d (at f.arv:3:4)"
	if got := pe.In("f.arv"); got != want {
		t.Fatalf("In = %q, want %q", got, want)
	}
}
