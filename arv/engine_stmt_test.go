package arv

import (
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
)

// Phase 8.4 differential on whole programs: where engineProgram wraps one
// random expression (ergo_gen_test.go), sgen writes statements. It makes
// functions that call earlier ones, closures over locals, let in blocks,
// return out of loops and ifs, for over arrays, match, hashes, index,
// .field, scope and an awaited spawn, with the Phase 5E expressions at
// the leaves. Names can be out of scope and values the wrong type: those
// errors must agree too.

type sgen struct {
	e     *egen
	names []string // values in scope
	fns   []string // one-argument functions in scope
	fresh int
}

func (g *sgen) next() int { return g.e.next() }

func (g *sgen) pick(xs []string) string { return xs[g.next()%len(xs)] }

func (g *sgen) name(prefix string) string {
	g.fresh++
	return prefix + strconv.Itoa(g.fresh)
}

// with runs f with name in scope, then takes it out.
func (g *sgen) with(name string, f func() string) string {
	g.names = append(g.names, name)
	s := f()
	g.names = g.names[:len(g.names)-1]
	return s
}

var sOps = []string{"+", "-", "*", "/", "==", "!=", "<", ">", "<=", ">=", "&&", "||"}

// expr writes an expression of at most depth levels of sgen's own forms.
func (g *sgen) expr(depth int) string {
	c := g.next()
	if depth <= 0 || c%7 == 0 {
		switch c % 3 {
		case 0:
			if len(g.names) > 0 {
				return g.pick(g.names)
			}
		case 1:
			return strconv.Itoa(g.next()%5 - 1)
		}
		g.e.nodes = 0
		return g.e.gen(2, false).write(true)
	}
	d := depth - 1
	switch (c / 7) % 14 {
	case 0:
		return g.pick(g.fns) + "(" + g.expr(d) + ")"
	case 1:
		return "[" + g.expr(d) + ", " + g.expr(d) + ", " + g.expr(d) + "]"
	case 2:
		return "(" + g.expr(d) + ")[" + g.expr(d) + "]"
	case 3:
		return `{"k": ` + g.expr(d) + `, 1: ` + g.expr(d) + `, true: ` + g.expr(d) + `}`
	case 4:
		return "(" + g.expr(d) + ")." + g.pick([]string{"k", "n", "sub", "up"})
	case 5:
		ok := g.with("v", func() string { return g.expr(d) })
		return "(match " + g.expr(d) + " { ok(v) => " + ok + ", err(e) => [e.kind, e.message, e.pos] })"
	case 6:
		arr := g.expr(d)
		return "(for x in " + arr + " { " + g.with("x", func() string { return g.block(d) }) + " })"
	case 7:
		arg := g.expr(d)
		return "fn(z) { " + g.with("z", func() string { return g.block(d) }) + " }(" + arg + ")"
	case 8:
		return "(if (" + g.expr(d) + ") { " + g.block(d) + " } else { " + g.block(d) + " })"
	case 9, 10:
		return "(" + g.expr(d) + " " + g.pick(sOps) + " " + g.expr(d) + ")"
	case 11:
		if len(g.names) > 0 {
			return `"<${` + g.pick(g.names) + `}|${` + g.pick(g.names) + `.n}>"`
		}
		return `"<${a}>"`
	case 12:
		return "(await spawn " + g.pick(g.fns) + "(" + g.expr(d) + "))"
	default:
		return "scope { " + g.block(d) + " }"
	}
}

// block writes one to three statements; the last is an expression.
func (g *sgen) block(depth int) string {
	var parts []string
	n := len(g.names)
	for i := g.next() % 3; i > 0; i-- {
		switch g.next() % 4 {
		case 0:
			v := g.name("v")
			parts = append(parts, "let "+v+" = "+g.expr(depth)+";")
			g.names = append(g.names, v)
		case 1:
			parts = append(parts, "if ("+g.expr(depth)+") { return "+g.expr(depth)+" };")
		case 2:
			parts = append(parts, "puts("+g.expr(depth)+");")
		default:
			parts = append(parts, g.expr(depth)+";")
		}
	}
	parts = append(parts, g.expr(depth))
	g.names = g.names[:n]
	return strings.Join(parts, " ")
}

// sPrelude adds functions to ePrelude's values: rec recurses (bounded),
// add takes two arguments.
const sPrelude = ePrelude + `let id = fn(x) { x };
let add = fn(x, y) { x + y };
let rec = fn(n) { if (n <= 0 || n > 30) { 0 } else { n + rec(n - 1) } };
`

// decodeProgram is the program the bytes choose: top-level functions
// and values, each function calling only earlier ones, then a puts.
func decodeProgram(data []byte) string {
	g := &sgen{e: &egen{b: data}, names: []string{"a", "b", "s", "xs", "h"}, fns: []string{"id", "rec", "tick"}}
	var b strings.Builder
	b.WriteString(sPrelude)
	for i := 1 + g.next()%4; i > 0; i-- {
		if g.next()%2 == 0 {
			f := g.name("f")
			body := g.with("p", func() string { return g.block(3) })
			fmt.Fprintf(&b, "let %s = fn(p) { %s };\n", f, body)
			g.fns = append(g.fns, f)
		} else {
			v := g.name("v")
			fmt.Fprintf(&b, "let %s = %s;\n", v, g.expr(3))
			g.names = append(g.names, v)
		}
	}
	fmt.Fprintf(&b, "puts(fn() { %s }())", g.block(3))
	return b.String()
}

// stmtCap bounds every run: the generated calls form a DAG and every
// loop is over a literal, but nesting can still multiply.
var stmtCap = runtime.Limits{Steps: 200000, AllocBytes: 8 << 20}

// checkEnginesCapped is checkEngines under stmtCap: the same outcome on
// both engines under the cap and under tight limits, and, when the cap
// does not stop it, the same steps and bytes.
func checkEnginesCapped(t *testing.T, src string) {
	t.Helper()
	with := func(steps, bytes int64) runtime.Limits { return runtime.Limits{Steps: steps, AllocBytes: bytes} }
	for _, l := range []runtime.Limits{stmtCap, with(150, stmtCap.AllocBytes), with(stmtCap.Steps, 1500)} {
		w, c := runOnEngine(src, true, l), runOnEngine(src, false, l)
		if w != c {
			t.Fatalf("limits %+v:\ntree-walker %+v\ncompiled    %+v\n%s", l, w, c, src)
		}
	}
	if runOnEngine(src, true, stmtCap).kind == runtime.KindBudget {
		return
	}
	for _, k := range []struct {
		name  string
		limit func(int64) runtime.Limits
		hi    int64
	}{
		{"steps", func(n int64) runtime.Limits { return with(n, stmtCap.AllocBytes) }, stmtCap.Steps},
		{"bytes", func(n int64) runtime.Limits { return with(stmtCap.Steps, n) }, stmtCap.AllocBytes},
	} {
		fw, fc := fitUnder(src, true, k.limit, k.hi), fitUnder(src, false, k.limit, k.hi)
		if fw != fc {
			t.Fatalf("%s: tree-walker %d, compiled %d\n%s", k.name, fw, fc, src)
		}
	}
}

// fitUnder is the smallest n <= hi under which src stops with no budget
// error on one engine.
func fitUnder(src string, walk bool, limit func(int64) runtime.Limits, hi int64) int64 {
	lo := int64(1)
	for lo < hi {
		mid := (lo + hi) / 2
		if runOnEngine(src, walk, limit(mid)).kind == runtime.KindBudget {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	return lo
}

// The engines agree on random whole programs: 150 seeds, fixed so a
// failure reproduces (the seed is in the subtest name).
func TestEnginesAgreeOnStatementsProperty(t *testing.T) {
	cases := 150
	if testing.Short() {
		cases = 20
	}
	for seed := 0; seed < cases; seed++ {
		r := rand.New(rand.NewSource(int64(5000 + seed)))
		data := make([]byte, 120)
		r.Read(data)
		t.Run(fmt.Sprint("seed", seed), func(t *testing.T) { checkEnginesCapped(t, decodeProgram(data)) })
	}
}

// FuzzEnginesAgreeStatements is the property with the fuzzer's bytes.
func FuzzEnginesAgreeStatements(f *testing.F) {
	for seed := 0; seed < 10; seed++ {
		r := rand.New(rand.NewSource(int64(seed)))
		data := make([]byte, 100)
		r.Read(data)
		f.Add(data)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 300 {
			return
		}
		checkEnginesCapped(t, decodeProgram(data))
	})
}
