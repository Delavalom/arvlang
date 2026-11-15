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

// The pool keeps LiveTaskDepth levels for every task the live-task cap
// allows, plus the root, and never less than MaxRunEvalDepth.
func TestRunDepthPoolScalesWithLiveTasks(t *testing.T) {
	for _, tt := range []struct {
		live int
		want int64
	}{
		{0, MaxRunEvalDepth},
		{DefaultLiveTasks, MaxRunEvalDepth},
		{8000, 8001 * LiveTaskDepth},
	} {
		b := Root(Limits{LiveTasks: tt.live})
		if got := b.DepthLimit(); got != tt.want {
			t.Fatalf("LiveTasks %d: pool %d, want %d", tt.live, got, tt.want)
		}
		if !b.Child(Limits{}).Task().Depth(tt.want) || b.Depth(1) {
			t.Fatalf("LiveTasks %d: the pool does not hold exactly %d levels", tt.live, tt.want)
		}
	}
}
