package check

import (
	"math/rand"
	"reflect"
	"strings"
	"testing"
)

// checkGenerated holds a generated program (see gen) to the soundness
// property: arv check reports no note and exactly the leaves' needs, the
// program runs with exactly those grants, and leaving any one out is
// denied.
func checkGenerated(t *testing.T, src string, needs map[string]bool, dir string) {
	t.Helper()
	rep, err := Source(src)
	if err != nil {
		t.Fatalf("%s: %v", src, err)
	}
	if rep.Notes != nil {
		t.Fatalf("%s: notes %v", src, rep.Notes)
	}
	var got []Entry
	reported := map[string]bool{}
	for _, n := range rep.Needs {
		got = append(got, Entry{Text: n.String()})
		reported[n.String()] = true
	}
	if !reflect.DeepEqual(reported, needs) {
		t.Fatalf("%s: check needs %v, the leaves %v", src, got, needs)
	}
	if err := runWithEntries(src, got, dir); err != nil {
		t.Fatalf("%s\nwith the reported grants: %v", src, err)
	}
	for drop := range got {
		rest := append(append([]Entry(nil), got[:drop]...), got[drop+1:]...)
		err := runWithEntries(src, rest, dir)
		if err == nil || !strings.Contains(err.Error(), "capability denied") {
			t.Fatalf("%s\nwithout %s: %v, want a denial", src, got[drop].Text, err)
		}
	}
}

// FuzzSoundGen drives the soundness generator from fuzzed seeds, so the
// property of TestReportedGrantsSufficeAndAreNeeded holds far beyond its
// 300 fixed programs: deeper nesting of spawn ... with, scope options,
// supervise and match around every kind of leaf.
func FuzzSoundGen(f *testing.F) {
	port := localServer(f)
	for _, s := range []int64{0, 1, 81, 8181, -7} {
		f.Add(s, uint8(3))
	}
	f.Fuzz(func(t *testing.T, seed int64, depth uint8) {
		g := &gen{r: rand.New(rand.NewSource(seed)), port: port, needs: map[string]bool{}}
		body, _ := g.expr(1 + int(depth%6))
		checkGenerated(t, g.prelude()+body+";", g.needs, soundFixture(t))
	})
}
