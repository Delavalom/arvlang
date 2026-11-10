package parser

import (
	"testing"

	"github.com/delavalom/arvlang/arv/lexer"
)

// Parse errors say where they happened, as "(at line:col)".
func TestParseErrorsHavePositions(t *testing.T) {
	tests := []struct{ input, want string }{
		{"let x = 1;\nlet y = ;", "no prefix parse function for ; found (at 2:9)"},
		{"let x 5;", "expected next token to be =, got INT instead (at 1:7)"},
		{"1;\n  spawn x", "spawn expects a function call, e.g. spawn f(x) (at 2:3)"},
		{"99999999999999999999", `could not parse "99999999999999999999" as integer (at 1:1)`},
	}
	for _, tt := range tests {
		p := New(lexer.New(tt.input))
		p.ParseProgram()
		errs := p.Errors()
		if len(errs) == 0 || errs[0] != tt.want {
			t.Errorf("%q: errors %q, want first %q", tt.input, errs, tt.want)
		}
	}
}
