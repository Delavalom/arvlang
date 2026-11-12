//go:build arv_treewalk

package evaluator

// defaultTreeWalk is true in a build with the arv_treewalk tag, so the
// whole test suite can run on the reference engine:
// go test -tags arv_treewalk ./...
const defaultTreeWalk = true
