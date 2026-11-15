package evaluator

import "testing"

// <= and >= compare integers, like < and >; any other operands are the
// errors < and > give.
func TestLessOrEqualAndGreaterOrEqual(t *testing.T) {
	tests := []struct{ input, want string }{
		{`1 <= 2`, "true"}, {`2 <= 2`, "true"}, {`3 <= 2`, "false"},
		{`1 >= 2`, "false"}, {`2 >= 2`, "true"}, {`3 >= 2`, "true"},
		{`-5 <= -5`, "true"}, {`1 + 1 >= 2 == true`, "true"},
		{`let s = 200; if (s >= 200) { s <= 299 } else { false }`, "true"},
	}
	for _, tt := range tests {
		if got, _ := runWith(t, tt.input); got.Inspect() != tt.want {
			t.Errorf("%s = %s, want %s", tt.input, got.Inspect(), tt.want)
		}
	}
	got, _ := runWith(t, `"a" <= "b"`)
	wantErrorAt(t, got, "unknown operator: STRING <= STRING", "1:1")
	got, _ = runWith(t, "let n = 1;\n  n >= true")
	wantErrorAt(t, got, "type mismatch: INTEGER >= BOOLEAN", "2:3")
}
