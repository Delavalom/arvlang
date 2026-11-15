package evaluator

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/lexer"
	"github.com/delavalom/arvlang/arv/parser"
	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// A hash literal that writes a key twice keeps one pair for it, and is
// charged for the pairs it keeps on both engines, not for the keys it
// writes (Phase 8.4, S2; mutation check: B6).
func TestEnginesChargeHashPairsKept(t *testing.T) {
	one := fittedBytes(t, `{1: 2}`)
	if twice := fittedBytes(t, `{1: 1, 1: 2}`); twice != one {
		t.Fatalf("{1: 1, 1: 2} is charged %d bytes, {1: 2} %d: a repeated key is charged once", twice, one)
	}
	fittedBytes(t, `{"a": 1, "a": 2, "a": 3, "b": 4}`)
}

// fittedBytes is the bytes src is charged, the same on both engines; it
// fails when they differ.
func fittedBytes(t *testing.T, src string) int64 {
	t.Helper()
	var w, c int64
	onEngine(true, func() { w = fit(t, src, bytesLimit) })
	onEngine(false, func() { c = fit(t, src, bytesLimit) })
	if w != c {
		t.Fatalf("%s: tree-walker %d bytes, compiled %d", src, w, c)
	}
	return w
}

// tickSource is n lines of if, &&, ||, <=, >=, fields and lets, one form
// per line, so the line a check falls on shows how many times the task
// ticked before it. Interpolation is left out: it stringifies through
// str, which looks at the context itself (a cancelled run stops at the
// first "${", whatever the tick count).
func tickSource(n int) string {
	forms := []string{
		`if (true) { 1 } else { 2 };`,
		`if (false) { 1 };`,
		`if (1 <= 2 && 2 >= 1) { 3 };`,
		`true && false;`,
		`false || true;`,
		`{"a": 1}.a;`,
		`let s = "v";`,
	}
	var b strings.Builder
	for i := 0; i < n; i++ {
		b.WriteString(forms[i%len(forms)])
		b.WriteByte('\n')
	}
	return b.String()
}

// runCancelled runs src on one engine in a task whose context is already
// cancelled: the run stops at the task's first check, every checkEvery
// ticks, and the error's position is the node that check fell on.
func runCancelled(t *testing.T, walk bool, src string) string {
	t.Helper()
	p := parser.New(lexer.New(src))
	program := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var got string
	onEngine(walk, func() {
		var out bytes.Buffer
		v := New(runtime.NewRoot(ctx, runtime.NewCapSet(), &out)).Run(program, value.NewEnvironment())
		e, ok := v.(*value.Error)
		if !ok || e.ErrKind() != runtime.KindCancelled {
			t.Fatalf("walk=%v: a cancelled run gave %s", walk, v.Inspect())
		}
		got = fmt.Sprintf("%s at %s: %s", e.ErrKind(), e.Pos, e.Message)
	})
	return got
}

// Both engines tick the task (Task.Tick, which counts toward its logged
// checks) once per node, so a check falls on the same node: a log is the
// same whichever engine wrote it (Phase 8.4, I7, S2; mutation check: L1,
// an extra tick in a compiled if). The program is long enough for the
// first check, and each line is a different node, so one tick more or
// less per if moves the check to another line.
func TestEnginesTickAlike(t *testing.T) {
	src := tickSource(400)
	w, c := runCancelled(t, true, src), runCancelled(t, false, src)
	if w != c {
		t.Fatalf("cancelled run:\ntree-walker %s\ncompiled    %s", w, c)
	}
	if strings.Contains(w, " at : ") {
		t.Fatalf("the cancel has no position: %s", w)
	}
}
