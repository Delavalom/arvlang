package arv

import (
	"strconv"
	"strings"
	"testing"
)

// hundred is the Arv array literal [0, 1, ..., 99].
var hundred = func() string {
	parts := make([]string, 100)
	for i := range parts {
		parts[i] = strconv.Itoa(i)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}()

// speedPrograms are the CPU-bound programs Phase 8.4 measures, one per
// kind of work the interpreter does: calls, loops, hashes, closures,
// match, string building, and the Phase 5E forms (.field, interpolation,
// &&, ||, <=, >=) in "ergonomic". Each takes 2 to 12 ms on the base
// (Apple M1 Pro) and needs no grant.
var speedPrograms = []struct{ name, src string }{
	{"calls", `let fib = fn(n) { if (n < 2) { n } else { fib(n - 1) + fib(n - 2) } }; fib(20)`},
	{"loops", `let a = ` + hundred + `; let s = for x in a { for y in a { x * y + 1 } }; len(s)`},
	{"hashes", `let a = ` + hundred + `; let h = {"a": 1, "b": 2}; let r = for x in a { for y in a { h["a"] + h["b"] + {"k": y}["k"] } }; len(r)`},
	{"ergonomic", `let a = ` + hundred + `; let h = {"a": 1, "b": 2}; let r = for x in a { for y in a { if (x <= y && y >= 50 || x == 3) { "${h.a}:${y}" } else { h.b } } }; len(r)`},
	{"closures", `let a = ` + hundred + `; let add = fn(n) { fn(m) { n + m } }; let r = for x in a { let f = add(x); for y in a { f(y) } }; len(r)`},
	{"match", `let a = ` + hundred + `; let r = for x in a { for y in a { match (if (y < 50) { error("e") } else { y }) { ok(v) => v, err(e) => e.kind } } }; len(r)`},
	{"strings", `let a = ` + hundred + `; let r = for x in a { let s = ""; for y in a { s + "ab" } }; len(r)`},
}

// BenchmarkSpeed runs each speed program through Run, parse included.
func BenchmarkSpeed(b *testing.B) {
	for _, p := range speedPrograms {
		b.Run(p.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := Run(p.src, Options{}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// TestSpeedProgramsRun keeps the benchmark programs valid: each one runs
// without an error, so a benchmark never measures an early failure.
func TestSpeedProgramsRun(t *testing.T) {
	for _, p := range speedPrograms {
		if _, err := Run(p.src, Options{}); err != nil {
			t.Errorf("%s: %v", p.name, err)
		}
	}
}
