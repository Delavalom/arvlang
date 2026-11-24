package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestCountLine(t *testing.T) {
	cases := []struct {
		line string
		want []string
	}{
		{"\t// I7: a replay performs no live effect", []string{"I7"}},
		{"x := 1 // I2 and I6, and I2 again", []string{"I2", "I6"}},
		{"// I70 and AI7 and I9 are not invariants", nil},
		{`s := "I1" // no invariant here`, nil},
		{`s := "I1"`, nil},
		{"// (I8)", []string{"I8"}},
	}
	for _, c := range cases {
		if got := CountLine(c.line); !reflect.DeepEqual(got, c.want) {
			t.Errorf("CountLine(%q) = %v, want %v", c.line, got, c.want)
		}
	}
}

func TestCountAndMissing(t *testing.T) {
	counts := map[string]int{}
	src := "// I1\n// I1 I3\nfunc f() {}\n// I8\n"
	if err := Count(strings.NewReader(src), counts); err != nil {
		t.Fatal(err)
	}
	if counts["I1"] != 2 || counts["I3"] != 1 || counts["I8"] != 1 {
		t.Fatalf("counts = %v", counts)
	}
	want := []string{"I2", "I4", "I5", "I6", "I7"}
	if got := Missing(counts); !reflect.DeepEqual(got, want) {
		t.Fatalf("Missing = %v, want %v", got, want)
	}
}

// The repository itself must name every invariant (the CI check).
func TestRepositoryNamesEveryInvariant(t *testing.T) {
	byPkg, err := Walk("../..")
	if err != nil {
		t.Fatal(err)
	}
	if m := Missing(Totals(byPkg)); len(m) > 0 {
		t.Fatalf("invariants with no test: %v", m)
	}
}
