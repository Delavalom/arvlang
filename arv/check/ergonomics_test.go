package check

import (
	"reflect"
	"testing"
)

// The Phase 5E syntax is desugared by the parser into nodes arv check
// already walks: "${x}" is a call of the pure builtin $str, r.f is
// r["f"], && and || are ifs, <= and >= are infix. An interpolation of
// pure values needs nothing and gets no note.
func TestErgonomicSyntaxNeedsNothingOfItsOwn(t *testing.T) {
	needs, notes := checked(t, `let r = {"up": 1, "n": [1]};
puts("${r.up} of ${len(r.n)} >= ${r.up >= 1 && r.up <= 2 || false}", str(r));`)
	if needs != nil || notes != nil {
		t.Errorf("needs = %q, notes = %q", needs, notes)
	}
}

// Effects inside the new syntax are searched like any others. A string
// literal cannot appear inside ${...}, so the name is bound first.
func TestErgonomicSyntaxIsSearched(t *testing.T) {
	for _, src := range []string{
		`let k = "A"; "a ${getenv(k)} b";`,
		`let k = "A"; "${len(getenv(k))}";`,
		`{"x": getenv("A")}.x;`,
		`true && getenv("A") == "";`,
		`false || getenv("A") == "";`,
		`getenv("A") == "" && true;`,
		`len(getenv("A")) <= 1;`,
		`1 >= len(getenv("A"));`,
	} {
		needs, notes := checked(t, src)
		if !reflect.DeepEqual(needs, []string{"env=A"}) || notes != nil {
			t.Errorf("%s: needs = %q, notes = %q", src, needs, notes)
		}
	}
}
