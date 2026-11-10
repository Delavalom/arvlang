package value

import (
	"runtime/debug"
	"strings"
	"testing"
)

// A value can nest without limit (`let a = [a];` repeated), and Inspect
// used to recurse once per level: a fatal Go stack overflow. It now stops
// at MaxInspectDepth levels and prints "..." for the rest.
func TestInspectDeepValues(t *testing.T) {
	defer debug.SetMaxStack(debug.SetMaxStack(16 << 20))
	var arr Object = &Array{}
	var hash Object = &Hash{Pairs: map[HashKey]HashPair{}}
	for i := 0; i < 1_000_000; i++ {
		arr = &Array{Elements: []Object{arr}}
		hash = NewHash(map[string]Object{"k": hash})
	}
	got := arr.Inspect()
	if want := strings.Repeat("[", MaxInspectDepth) + "..." + strings.Repeat("]", MaxInspectDepth); got != want {
		t.Errorf("deep array = %.40q... (%d bytes), want %d levels then ...", got, len(got), MaxInspectDepth)
	}
	got = hash.Inspect()
	if want := strings.Repeat("{k: ", MaxInspectDepth) + "..." + strings.Repeat("}", MaxInspectDepth); got != want {
		t.Errorf("deep hash = %.40q... (%d bytes), want %d levels then ...", got, len(got), MaxInspectDepth)
	}
	shallow := &Array{Elements: []Object{&Integer{Value: 1}, &Array{Elements: []Object{&String{Value: "a"}}}}}
	if got := shallow.Inspect(); got != "[1, [a]]" {
		t.Errorf("shallow = %s", got)
	}
}
