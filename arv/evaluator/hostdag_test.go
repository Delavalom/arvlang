package evaluator

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// dag is n arrays, each holding the next one twice: n+1 nodes, 2^n paths.
func dag(n int, leaf value.Object) *value.Array {
	a := &value.Array{Elements: []value.Object{leaf}}
	for i := 0; i < n; i++ {
		a = &value.Array{Elements: []value.Object{a, a}}
	}
	return a
}

// A result that shares sub-values (a program's own DAG, echoed back by
// Fn) is walked and charged once per node, not once per path: 2^60 paths
// take no time, cost what the 61 nodes hold, and keep their sharing.
func TestHostValueWalksSharedNodesOnce(t *testing.T) {
	start := time.Now()
	got, size, err := hostValue(dag(60, &value.Integer{Value: 1}))
	if err != nil {
		t.Fatal(err)
	}
	if want := int64(arrayElemSize + 60*2*arrayElemSize); size != want {
		t.Fatalf("size %d, want %d", size, want)
	}
	if d := time.Since(start); d > 10*time.Second {
		t.Fatalf("took %v", d)
	}
	// A shared node that needs a copy (it holds a host boolean) is copied
	// once, and both references see the one copy.
	got, _, err = hostValue(dag(60, &value.Boolean{Value: true}))
	if err != nil {
		t.Fatal(err)
	}
	top := got.(*value.Array)
	if top.Elements[0] != top.Elements[1] {
		t.Fatal("a shared node was copied twice")
	}
	// A shared string is charged once.
	s := &value.String{Value: strings.Repeat("x", 1000)}
	_, size, _ = hostValue(&value.Array{Elements: []value.Object{s, s, s}})
	if want := int64(3*arrayElemSize + 1000); size != want {
		t.Fatalf("size %d, want %d", size, want)
	}
}

// Remembering a node does not let a result nest deeper than maxHostDepth:
// a chain seen first near the top and again far down is still too deep.
func TestHostValueSharedNodeKeepsDepthLimit(t *testing.T) {
	chain := value.Object(&value.Integer{Value: 1})
	for i := 0; i < maxHostDepth-10; i++ {
		chain = &value.Array{Elements: []value.Object{chain}}
	}
	deep := chain
	for i := 0; i < 20; i++ {
		deep = &value.Array{Elements: []value.Object{deep}}
	}
	in := &value.Array{Elements: []value.Object{chain, deep}}
	if _, _, err := hostValue(in); err == nil || !strings.Contains(err.Error(), "deeper than") {
		t.Fatalf("err = %v, want one about depth", err)
	}
}

// hostResult stops walking a large result once the task is cancelled,
// and charges it as it goes: a spent budget stops the walk early.
func TestHostResultChecksAsItWalks(t *testing.T) {
	big := &value.Array{}
	for i := 0; i < 100000; i++ {
		big.Elements = append(big.Elements, &value.Array{Elements: []value.Object{&value.Integer{Value: int64(i)}}})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	task := runtime.NewRoot(ctx, runtime.NewCapSet(), io.Discard)
	var charged int64
	charge := func(n int64) error { charged += n; return nil }
	got := hostResult(task, "get", big, nil, charge)
	if e, ok := got.(*value.Error); !ok || e.Kind != runtime.KindCancelled {
		t.Fatalf("cancelled task: got %s, want a cancelled error", brief(got))
	}
	if charged >= int64(len(big.Elements))*2*arrayElemSize {
		t.Fatalf("charged %d: the walk did not stop", charged)
	}
	task = runtime.NewRoot(context.Background(), runtime.NewCapSet(), io.Discard)
	calls := 0
	charge = func(n int64) error {
		calls++
		return &runtime.ExceededError{Kind: "alloc", Limit: 1}
	}
	got = hostResult(task, "get", big, nil, charge)
	if e, ok := got.(*value.Error); !ok || e.Kind != runtime.KindBudget || calls != 1 {
		t.Fatalf("spent budget: got %s after %d charges", brief(got), calls)
	}
}

// brief describes v without printing a large value.
func brief(v value.Object) string {
	if e, ok := v.(*value.Error); ok {
		return e.Kind + " error: " + e.Message
	}
	return string(v.Type())
}
