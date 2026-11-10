package runtime

import "testing"

// The run's evaluator depth is one pool for all its tasks: reserved by
// any level of the budget tree, refused past MaxRunEvalDepth, returned
// with a negative reservation.
func TestRunDepthPool(t *testing.T) {
	root := Root(Limits{})
	task := root.Child(Limits{Steps: 10}).Task()
	if !root.Depth(MaxRunEvalDepth - 10) {
		t.Fatal("reserving under the limit failed")
	}
	if task.Depth(11) {
		t.Fatal("a task reserved past the run's limit")
	}
	if !task.Depth(10) {
		t.Fatal("a refused reservation was not given back")
	}
	root.Depth(-10)
	if !task.Depth(10) {
		t.Fatal("returned levels were not reusable")
	}
	if Root(Limits{}).Depth(1) != true {
		t.Fatal("each run has its own pool")
	}
}
