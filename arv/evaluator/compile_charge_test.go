package evaluator

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/delavalom/arvlang/arv/ast"
	"github.com/delavalom/arvlang/arv/lexer"
	"github.com/delavalom/arvlang/arv/parser"
	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// runCharged runs src under l as arv.Run does, its code charge included
// (runLimited leaves it out); it returns the result and the output.
func runCharged(t *testing.T, src string, l runtime.Limits) (value.Object, string) {
	t.Helper()
	p := parser.New(lexer.New(src))
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}
	var out bytes.Buffer
	root := runtime.NewRoot(context.Background(), runtime.NewCapSet(), &out)
	return New(root).RunLimited(prog, value.NewEnvironment(), l), out.String()
}

// fitCharged is fit with the code charge included.
func fitCharged(t *testing.T, src string, limit func(int64) runtime.Limits) int64 {
	t.Helper()
	lo, hi := int64(1), int64(1<<24)
	for lo < hi {
		mid := (lo + hi) / 2
		if r, _ := runCharged(t, src, limit(mid)); isError(r) {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	return lo
}

var chargePrograms = []string{
	`1 + 2`,
	`let f = fn(a, b) { if (a < b) { [a, b, {"k": a + b * 2}] } else { f(b, a) } }; f(3, 1)`,
	`let h = {"n": 1}; let s = "x${h.n}y"; h.n > 0 && len(s) == 3 || false`,
	`let r = scope { let t = spawn fn(x) { x * 2 }(21); await t }; for x in [1, 2, 3] { r + x }`,
	`match scope { error("no") } { ok(v) => v, err(e) => e.message }`,
	`let g = fn() { return 5; 6 }; g()`,
}

// A7: a program's code costs codeNodeSize bytes per node of its AST, on
// both engines, on top of what evaluating it costs; its steps do not
// change.
func TestCodeIsChargedPerNodeOnBothEngines(t *testing.T) {
	for _, src := range chargePrograms {
		want := codeNodeSize * int64(ast.Size(parsed(t, src)))
		for _, walk := range []bool{false, true} {
			onEngine(walk, func() {
				charged, plain := fitCharged(t, src, bytesLimit), fit(t, src, bytesLimit)
				if plain == 1 {
					plain = 0 // fit's floor: a program that allocates nothing
				}
				if charged-plain != want {
					t.Errorf("walk=%v: %s: %d bytes with its code, %d without: want %d more", walk, src, charged, plain, want)
				}
				if a, b := fitCharged(t, src, stepsLimit), fit(t, src, stepsLimit); a != b {
					t.Errorf("walk=%v: %s: %d steps with the charge, %d without", walk, src, a, b)
				}
			})
		}
	}
}

// A program whose code does not fit its alloc_mb fails before its first
// step, with the budget's error, on both engines alike: nothing printed.
func TestCodeThatDoesNotFitFailsFirst(t *testing.T) {
	const src = `puts("ran"); 1 + 2`
	code := codeNodeSize * int64(ast.Size(parsed(t, src)))
	var got [2]string
	for i, walk := range []bool{false, true} {
		onEngine(walk, func() {
			r, out := runCharged(t, src, runtime.Limits{AllocBytes: code - 1})
			e, ok := r.(*value.Error)
			want := fmt.Sprintf("budget exceeded: alloc (limit %d bytes)", code-1)
			if !ok || e.Message != want || e.ErrKind() != runtime.KindBudget || out != "" {
				t.Errorf("walk=%v: %v, printed %q; want %q and nothing printed", walk, r, out, want)
			} else {
				got[i] = e.Message + " at " + e.Pos
			}
			if r, out := runCharged(t, src, runtime.Limits{AllocBytes: code}); isError(r) || out != "ran\n" {
				t.Errorf("walk=%v: with %d bytes: %v, printed %q", walk, code, r, out)
			}
		})
	}
	if got[0] != got[1] {
		t.Errorf("engines differ: %q, %q", got[0], got[1])
	}
}

// A program not made by the parser (Nodes 0) is not charged, and nor is
// a node that is not a program.
func TestHandBuiltProgramsAreNotCharged(t *testing.T) {
	prog := parsed(t, `[1, 2]`)
	nodes := prog.Nodes
	prog.Nodes = 0
	root := runtime.NewRoot(context.Background(), runtime.NewCapSet(), &bytes.Buffer{})
	if r := New(root).RunLimited(prog, value.NewEnvironment(), runtime.Limits{AllocBytes: 2 * arrayElemSize}); isError(r) {
		t.Errorf("Nodes 0: %v", r)
	}
	root = runtime.NewRoot(context.Background(), runtime.NewCapSet(), &bytes.Buffer{})
	stmt := prog.Statements[0]
	if r := New(root).RunLimited(stmt, value.NewEnvironment(), runtime.Limits{AllocBytes: 2 * arrayElemSize}); isError(r) {
		t.Errorf("a statement: %v", r)
	}
	if nodes == 0 {
		t.Error("the parser set no Nodes")
	}
}
