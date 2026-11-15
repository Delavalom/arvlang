package arv

import (
	"errors"
	"fmt"
	"math/rand"
	goruntime "runtime"
	"strings"
	"testing"
	"time"

	"github.com/delavalom/arvlang/arv/runtime"
)

// I6 through the language: for random nests of scope options, with the
// run's own limit as the outermost level, a program that never stops on
// its own fails with the smallest limit in the nest, wherever it sits.
// Every other limit is far larger, so what the scopes spend opening
// (evaluating their options) or a late timer cannot change which one
// trips first.

type nestKind struct {
	kind   string
	option string                     // the scope option
	small  func(*rand.Rand) int64     // the limit that must trip
	big    int64                      // every other limit present
	root   func(int64) runtime.Limits // the same limit for the whole run
	want   func(int64) string         // the error for the small limit
	work   string                     // a body that never ends on its own
}

var nestKinds = []nestKind{
	{
		kind: "steps", option: "steps",
		small: func(r *rand.Rand) int64 { return 500 + r.Int63n(3000) },
		big:   10_000_000,
		root:  func(n int64) runtime.Limits { return runtime.Limits{Steps: n} },
		want:  func(n int64) string { return fmt.Sprintf("budget exceeded: steps (limit %d)", n) },
		work:  "spin(0)",
	},
	{
		kind: "alloc", option: "alloc_mb",
		small: func(r *rand.Rand) int64 { return 1 + r.Int63n(3) },
		big:   4096,
		root:  func(n int64) runtime.Limits { return runtime.Limits{AllocBytes: n << 20} },
		want:  func(n int64) string { return fmt.Sprintf("budget exceeded: alloc (limit %d bytes)", n<<20) },
		work:  `grow([])`,
	},
	{
		kind: "timeout", option: "timeout_ms",
		small: func(r *rand.Rand) int64 { return 20 + r.Int63n(80) },
		big:   600_000,
		root:  func(n int64) runtime.Limits { return runtime.Limits{Timeout: time.Duration(n) * time.Millisecond} },
		want:  func(n int64) string { return fmt.Sprintf("budget exceeded: timeout (limit %d ms)", n) },
		work:  "sleep(600000)",
	},
}

const nestPrelude = `let spin = fn(n) { spin(n + 1) };
let grow = fn(acc) { grow(push(acc, "xxxxxxxxxxxxxxxx")) };
`

func TestNestedLimitsOnlyShrinkProperty(t *testing.T) {
	rng := rand.New(rand.NewSource(6))
	for _, k := range nestKinds {
		for round := 0; round < 12; round++ {
			small := k.small(rng)
			levels := 1 + rng.Intn(4)       // the run plus the scopes
			at := rng.Intn(levels + 1)      // where the small limit sits; 0 = the run
			limits := make([]int64, levels) // 0: this scope sets none
			for i := range limits {
				if rng.Intn(3) > 0 {
					limits[i] = k.big
				}
			}
			var opts Options
			switch {
			case at == 0:
				opts.Limits = k.root(small)
			case rng.Intn(2) == 0:
				opts.Limits = k.root(k.big)
			}
			if at > 0 {
				limits[at-1] = small
			}
			src := nestPrelude + nest(k.option, limits, k.work)
			if k.kind != "steps" && rng.Intn(2) == 0 { // inside a task too
				src = nestPrelude + "let body = fn() { " + nest(k.option, limits, k.work) + " };\n" +
					"scope { await spawn body() }"
			}

			before := goruntime.NumGoroutine()
			_, err := Run(src, opts)
			var ae *Error
			if !errors.As(err, &ae) || ae.Msg != k.want(small) || ae.Pos == "" {
				t.Fatalf("%s round %d: err = %v, want %q with a position\nlimits %v (run: %+v)\n%s",
					k.kind, round, err, k.want(small), limits, opts.Limits, src)
			}
			noLeak(t, before, src)
		}
	}
}

// nest wraps body in one scope per entry of limits, outermost first; an
// entry of 0 is a scope without that option.
func nest(option string, limits []int64, body string) string {
	var b strings.Builder
	for _, l := range limits {
		if l == 0 {
			b.WriteString("scope { ")
		} else {
			fmt.Fprintf(&b, "scope (%s: %d) { ", option, l)
		}
	}
	b.WriteString(body)
	b.WriteString(strings.Repeat(" }", len(limits)))
	return b.String()
}
