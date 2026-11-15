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

// checkDesugared fails unless the ergonomic and hand-written forms of n
// give the same value or error, the same output, and are charged the same
// steps and the same bytes.
func checkDesugared(t *testing.T, n *enode) {
	t.Helper()
	e, w := eProgram(n, true), eProgram(n, false)
	oe, ow := eRun(e, runtime.Limits{}), eRun(w, runtime.Limits{})
	if strings.HasPrefix(oe.err, "go: ") || strings.HasPrefix(ow.err, "go: ") {
		t.Fatalf("a form does not run:\n%s\n%+v\n%s\n%+v", e, oe, w, ow)
	}
	if oe != ow {
		t.Fatalf("outcomes differ:\n%s\n%+v\n%s\n%+v", e, oe, w, ow)
	}
	for _, c := range []struct {
		name  string
		limit func(int64) runtime.Limits
	}{{"steps", eSteps}, {"bytes", eBytes}} {
		fe, okE := eFit(e, c.limit)
		fw, okW := eFit(w, c.limit)
		if !okE || !okW || fe != fw {
			t.Fatalf("%s: %d (%v) for\n%s\n%d (%v) for\n%s", c.name, fe, okE, e, fw, okW, w)
		}
	}
}

// decode is the expression the bytes choose.
func decode(data []byte) *enode { return (&egen{b: data}).gen(5, false) }

// The desugaring property over random expressions: 300 seeds, fixed so a
// failure reproduces (the seed is in the subtest name).
func TestDesugaringEquivalenceProperty(t *testing.T) {
	cases := 300
	if testing.Short() {
		cases = 40
	}
	for seed := 0; seed < cases; seed++ {
		r := rand.New(rand.NewSource(int64(seed)))
		data := make([]byte, 80)
		r.Read(data)
		t.Run(fmt.Sprint("seed", seed), func(t *testing.T) { checkDesugared(t, decode(data)) })
	}
}

// Hand-picked shapes the random walk may take long to reach: short
// circuit over a failing or printing right side, interpolations of every
// atom, escapes and nested fields.
func TestDesugaringEquivalenceExamples(t *testing.T) {
	for _, n := range []*enode{
		{kind: "and", kids: []*enode{{kind: "atom", text: "false"}, {kind: "atom", text: "(1 / 0)"}}},
		{kind: "or", kids: []*enode{{kind: "atom", text: "true"}, {kind: "tick", kids: []*enode{{kind: "atom", text: "a"}}}}},
		{kind: "or", kids: []*enode{{kind: "atom", text: "1"}, {kind: "spawn", kids: []*enode{{kind: "atom", text: "b"}}}}},
		{kind: "interp", texts: []string{`\${n}`, " of ", "!"}, kids: []*enode{{kind: "atom", text: "xs"}, {kind: "field", text: "missing"}}},
		{kind: "interp", texts: []string{"", ""}, kids: []*enode{{kind: "atom", text: "s"}}},
		{kind: "interp", texts: []string{"", "", ""}, kids: []*enode{{kind: "atom", text: "(1 / 0)"}, {kind: "atom", text: "tick(a)"}}},
		{kind: "field", text: "k", kids: []*enode{{kind: "field", text: "sub"}}},
	} {
		t.Run(n.write(true), func(t *testing.T) { checkDesugared(t, n) })
	}
}

// FuzzDesugarEquivalence is the property with the fuzzer's bytes.
func FuzzDesugarEquivalence(f *testing.F) {
	for seed := 0; seed < 20; seed++ {
		r := rand.New(rand.NewSource(int64(seed)))
		data := make([]byte, 40)
		r.Read(data)
		f.Add(data)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 200 {
			return
		}
		checkDesugared(t, decode(data))
	})
}

// <= and >= are Go's on integers, and fail as < and > fail on anything
// else (the operator named in the message is the one written).
func TestCompareMatchesGo(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	for i := 0; i < 200; i++ {
		x, y := r.Int63n(7)-3, r.Int63n(7)-3
		if i%2 == 0 {
			x, y = r.Int63()-r.Int63(), r.Int63()-r.Int63()
		}
		src := fmt.Sprintf("[%d <= %d, %d >= %d]", x, y, x, y)
		if o := eRun(src, runtime.Limits{}); o.value != fmt.Sprintf("[%v, %v]", x <= y, x >= y) {
			t.Fatalf("%s = %+v", src, o)
		}
	}
	for _, v := range []string{`"a"`, "true", "[1]", "{}", "fn() { 1 }"} {
		for _, pair := range [][2]string{{v, v}, {"1", v}, {v, "1"}} {
			for _, op := range []string{"<=", ">="} {
				got := eRun(pair[0]+" "+op+" "+pair[1], runtime.Limits{})
				want := eRun(pair[0]+" < "+pair[1], runtime.Limits{})
				if got.err == "" || got.err != strings.Replace(want.err, " < ", " "+op+" ", 1) {
					t.Errorf("%s %s %s: %q, < gives %q", pair[0], op, pair[1], got.err, want.err)
				}
			}
		}
	}
}

// Leak rule for the new forms: a task spawned on the right of && or ||,
// or inside ${...}, is ended and waited for before Run returns, whether the
// run succeeds, fails on its own, or runs out of a budget.
func TestErgonomicFormsLeakNoGoroutine(t *testing.T) {
	const nap = "let nap = fn() { sleep(600000) };\nlet id = fn(x) { x };\n"
	for _, c := range []struct {
		name, src string
		limits    runtime.Limits
		want      string
	}{
		{"short-circuited spawn", nap + "false && (await spawn nap())", runtime.Limits{}, ""},
		{"spawn on the right of &&", nap + "true && (await spawn id(1)) == 1", runtime.Limits{}, ""},
		{"unawaited task on the right of ||, then an error",
			nap + "let boom = fn() { let t = spawn nap(); 1 / 0 };\nfalse || boom()", runtime.Limits{}, "division by zero"},
		{"timeout while the right side of && awaits",
			nap + "true && (await spawn nap())", runtime.Limits{Timeout: 50 * time.Millisecond}, "budget exceeded: timeout (limit 50 ms)"},
		{"interpolation of awaited tasks", nap + `"${await spawn id(1)}-${await spawn id([2])}"`, runtime.Limits{}, ""},
		{"timeout inside ${...}", nap + `"x ${await spawn nap()}"`, runtime.Limits{Timeout: 50 * time.Millisecond}, "budget exceeded: timeout (limit 50 ms)"},
		{"alloc runs out in ${...} while a task sleeps",
			nap + "let d = fn(s, n) { if (n == 0) { s } else { d(s + s, n - 1) } };\nlet x = \"x\";\nlet t = spawn nap();\n\"${d(x, 22)}\"",
			runtime.Limits{AllocBytes: 1 << 20}, "budget exceeded: alloc (limit 1048576 bytes)"},
		{"steps run out on the right of || while a task sleeps",
			nap + "let f = fn(n) { f(n + 1) };\nlet t = spawn nap();\nfalse || f(0)", runtime.Limits{Steps: 5000}, "budget exceeded: steps (limit 5000)"},
	} {
		t.Run(c.name, func(t *testing.T) {
			before := settle(0)
			_, err := Run(c.src, Options{Limits: c.limits})
			var ae *Error
			switch {
			case c.want == "" && err != nil:
				t.Fatalf("err = %v, want success", err)
			case c.want != "" && (!errors.As(err, &ae) || ae.Msg != c.want):
				t.Fatalf("err = %v, want %q", err, c.want)
			}
			if after := settle(before); after > before {
				buf := make([]byte, 1<<20)
				t.Fatalf("%d goroutines before, %d after:\n%s", before, after, buf[:goruntime.Stack(buf, true)])
			}
		})
	}
}
