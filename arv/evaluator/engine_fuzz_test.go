package evaluator

import (
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/lexer"
	"github.com/delavalom/arvlang/arv/parser"
	"github.com/delavalom/arvlang/arv/runtime"
)

// FuzzEnginesAgreeSource is the Phase 8.4 differential on raw source:
// any program that parses gives the same value or error (message, kind,
// position, task) and the same output on the tree-walker and as compiled
// code, under a cap and under tight limits that stop it part way, and is
// charged the same steps and the same bytes (S1, S2, S4). The seeds are
// the differential corpus, so mutations start from every node type.
//
// Programs that name a nondeterministic builtin (random, now, sleep), a
// timeout or spawn are skipped: their outcomes may differ from run to run
// on one engine (tasks print and spend a shared budget concurrently).
func FuzzEnginesAgreeSource(f *testing.F) {
	for _, src := range engineCorpus {
		f.Add(src)
	}
	f.Add(`let r = {"a": {"b": 2}}; let f = fn(x) { x.a.b >= 2 && x.a.b <= 3 || false }; "v=${f(r)} ${r.a}"`)
	for _, c := range returnCases { // a return in every value position (A1)
		f.Add(c.src)
	}
	f.Add(`let f = fn(n) { let g = fn() { [n, {"k": -(if (n > 1) { return n * 2 } else { n })}] }; [g(), if (n < 3) { return "${g()}" } else { 0 }] }; [f(1), f(2), f(3)]`)
	f.Fuzz(func(t *testing.T, src string) {
		if len(src) > 2000 || !deterministicSource(src) {
			return
		}
		p := parser.New(lexer.New(src))
		p.ParseProgram()
		if len(p.Errors()) > 0 {
			return
		}
		sameOnBothEnginesCapped(t, src)
	})
}

// fuzzCap bounds every fuzzed run: steps end loops and recursion, bytes
// end doubling strings.
var fuzzCap = runtime.Limits{Steps: 20000, AllocBytes: 1 << 20}

func deterministicSource(src string) bool {
	for _, w := range []string{"spawn", "random", "now", "sleep", "timeout_ms"} {
		if strings.Contains(src, w) {
			return false
		}
	}
	return true
}

// sameOnBothEnginesCapped is sameOnBothEngines for programs that may not
// end: every limit includes the cap, and the exact steps and bytes are
// fitted below it.
func sameOnBothEnginesCapped(t *testing.T, src string) {
	t.Helper()
	limits := []runtime.Limits{fuzzCap, {Steps: 7, AllocBytes: fuzzCap.AllocBytes}, {Steps: 300, AllocBytes: fuzzCap.AllocBytes},
		{Steps: fuzzCap.Steps, AllocBytes: 40}, {Steps: fuzzCap.Steps, AllocBytes: 600}}
	for _, l := range limits {
		w, c := runEngine(t, true, src, l), runEngine(t, false, src, l)
		if w != c {
			t.Fatalf("limits %+v:\ntree-walker %+v\ncompiled    %+v\n%s", l, w, c, src)
		}
	}
	if runEngine(t, true, src, fuzzCap).kind == runtime.KindBudget {
		return
	}
	for _, k := range []struct {
		name  string
		limit func(int64) runtime.Limits
		hi    int64
	}{
		{"steps", func(n int64) runtime.Limits { return runtime.Limits{Steps: n, AllocBytes: fuzzCap.AllocBytes} }, fuzzCap.Steps},
		{"bytes", func(n int64) runtime.Limits { return runtime.Limits{Steps: fuzzCap.Steps, AllocBytes: n} }, fuzzCap.AllocBytes},
	} {
		w, c := fitBelow(t, true, src, k.limit, k.hi), fitBelow(t, false, src, k.limit, k.hi)
		if w != c {
			t.Fatalf("%s: tree-walker %d, compiled %d\n%s", k.name, w, c, src)
		}
	}
}

// fitBelow is the smallest n <= hi under which src, on one engine, stops
// with no budget error; src must do so under hi.
func fitBelow(t *testing.T, walk bool, src string, limit func(int64) runtime.Limits, hi int64) int64 {
	t.Helper()
	lo := int64(1)
	for lo < hi {
		mid := (lo + hi) / 2
		if runEngine(t, walk, src, limit(mid)).kind == runtime.KindBudget {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	return lo
}
