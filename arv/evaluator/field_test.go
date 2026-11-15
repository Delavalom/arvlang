package evaluator

import "testing"

const record = `let r = {"up": true, "status": 200, "in": {"deep": [1, 2]}};`

// r.f is r["f"]: the same value, nil for a missing key, keywords as names.
func TestFieldAccess(t *testing.T) {
	tests := []struct{ input, want string }{
		{record + `r.up`, "true"},
		{record + `r.status + 1`, "201"},
		{record + `r.missing`, "nil"},
		{record + `r.in.deep[1]`, "2"},
		{record + `r.in.nothing`, "nil"},
		{record + `r.up == r["up"]`, "true"},
		{`let f = fn() { {"x": 7} }; f().x`, "7"},
		{`{"if": 1}.if`, "1"},
	}
	for _, tt := range tests {
		if got, _ := runWith(t, tt.input); got.Inspect() != tt.want {
			t.Errorf("%s = %s, want %s", tt.input, got.Inspect(), tt.want)
		}
	}
}

// Anything r["f"] refuses, r.f refuses with the same error at the same
// place: the start of r.
func TestFieldErrorsMatchIndexErrors(t *testing.T) {
	for _, c := range []struct{ field, index, msg string }{
		{"let n = 5;\n  n.up", "let n = 5;\n  n[\"up\"]", "index operator not supported: INTEGER"},
		{"let a = [1];\n  a.up", "let a = [1];\n  a[\"up\"]", "index operator not supported: ARRAY"},
		{record + "\n  r.missing.x", record + "\n  r[\"missing\"][\"x\"]", "index operator not supported: NIL"},
		{"\n  nope.x", "\n  nope[\"x\"]", "identifier not found: nope"},
	} {
		got, _ := runWith(t, c.field)
		wantErrorAt(t, got, c.msg, "2:3")
		got, _ = runWith(t, c.index)
		wantErrorAt(t, got, c.msg, "2:3")
	}
}

// r.f costs exactly what r["f"] costs, in steps and in bytes.
func TestFieldCostsWhatIndexCosts(t *testing.T) {
	sameCost(t, record+`r.up`, record+`r["up"]`)
	sameCost(t, record+`r.in.deep[0]`, record+`r["in"]["deep"][0]`)
}
