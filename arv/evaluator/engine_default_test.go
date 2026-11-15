package evaluator

import (
	"testing"

	"github.com/delavalom/arvlang/arv/value"
)

// The default engine is compiled code, and the tree-walker only in a
// build with the arv_treewalk tag (review round 1). Every other engine
// test picks its engine, so without this one a default flipped by mistake
// would pass the whole suite and lose Phase 8.4's speed in production.
func TestDefaultEngine(t *testing.T) {
	if defaultTreeWalk != wantTreeWalk || treeWalk.Load() != wantTreeWalk {
		t.Fatalf("default tree-walker %v, now %v; this build wants %v", defaultTreeWalk, treeWalk.Load(), wantTreeWalk)
	}
	got, _ := runWith(t, `fn(x) { x }`)
	fn, ok := got.(*value.Function)
	if !ok {
		t.Fatalf("got %s", got.Inspect())
	}
	if _, compiled := fn.Code.(*code); compiled == wantTreeWalk {
		t.Errorf("a run with the default engine made a function with compiled body %v; tree-walker wanted %v", compiled, wantTreeWalk)
	}
}
