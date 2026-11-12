package arv

import (
	"bytes"
	"errors"
	"fmt"
	"math/rand"
	"testing"

	"github.com/delavalom/arvlang/arv/evaluator"
	"github.com/delavalom/arvlang/arv/runtime"
)

// engineProgram puts the random expression n (ergo_gen_test.go) inside a
// function, a let, a return, a loop, a match and a spawned task, so the
// node types the generator does not make run around it.
func engineProgram(n *enode) string {
	return ePrelude +
		"let f = fn(i) { let r = " + n.write(true) + "; if (i == 2) { return r } else { [i, r] } };\n" +
		"let rs = for i in [1, 2] { match f(i) { ok(v) => v, err(e) => [e.kind, e.message, e.pos] } };\n" +
		"puts(rs);\nscope { await spawn f(3) }"
}

// engineOutcome is eOutcome with the error's position, which the same
// source must give on both engines.
type engineOutcome struct {
	eOutcome
	pos string
}

// runOnEngine runs src through Run on one engine (walk: the tree-walker).
func runOnEngine(src string, walk bool, l runtime.Limits) engineOutcome {
	old := evaluator.SetTreeWalk(walk)
	defer evaluator.SetTreeWalk(old)
	var out bytes.Buffer
	v, err := Run(src, Options{Out: &out, Limits: l})
	o := engineOutcome{eOutcome: eOutcome{out: out.String()}}
	var ae *Error
	switch {
	case errors.As(err, &ae):
		o.err, o.kind, o.pos = ae.Msg, ae.Kind, ae.Pos
	case err != nil:
		o.err = "go: " + err.Error()
	default:
		o.value = v.Inspect()
	}
	return o
}

// fitOnEngine is eFit on one engine.
func fitOnEngine(src string, walk bool, limit func(int64) runtime.Limits) (int64, bool) {
	old := evaluator.SetTreeWalk(walk)
	defer evaluator.SetTreeWalk(old)
	return eFit(src, limit)
}

// checkEngines fails unless src gives the same outcome on both engines,
// with no limit and under a tight steps and bytes limit, and is charged
// the same steps and the same bytes.
func checkEngines(t *testing.T, src string) {
	t.Helper()
	for _, l := range []runtime.Limits{{}, {Steps: 150}, {AllocBytes: 1500}} {
		w, c := runOnEngine(src, true, l), runOnEngine(src, false, l)
		if w != c {
			t.Fatalf("limits %+v:\ntree-walker %+v\ncompiled    %+v\n%s", l, w, c, src)
		}
	}
	for _, k := range []struct {
		name  string
		limit func(int64) runtime.Limits
	}{{"steps", eSteps}, {"bytes", eBytes}} {
		fw, okW := fitOnEngine(src, true, k.limit)
		fc, okC := fitOnEngine(src, false, k.limit)
		if fw != fc || okW != okC {
			t.Fatalf("%s: tree-walker %d (%v), compiled %d (%v)\n%s", k.name, fw, okW, fc, okC, src)
		}
	}
}

// The engines agree on random programs: 200 seeds, fixed so a failure
// reproduces (the seed is in the subtest name).
func TestEnginesAgreeProperty(t *testing.T) {
	cases := 200
	if testing.Short() {
		cases = 30
	}
	for seed := 0; seed < cases; seed++ {
		r := rand.New(rand.NewSource(int64(1000 + seed)))
		data := make([]byte, 80)
		r.Read(data)
		t.Run(fmt.Sprint("seed", seed), func(t *testing.T) { checkEngines(t, engineProgram(decode(data))) })
	}
}

// FuzzEnginesAgree is the property with the fuzzer's bytes.
func FuzzEnginesAgree(f *testing.F) {
	for seed := 0; seed < 10; seed++ {
		r := rand.New(rand.NewSource(int64(seed)))
		data := make([]byte, 40)
		r.Read(data)
		f.Add(data)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 200 {
			return
		}
		checkEngines(t, engineProgram(decode(data)))
	})
}

// The speed programs (p84.1) agree too, under no limit.
func TestEnginesAgreeOnSpeedPrograms(t *testing.T) {
	for _, p := range speedPrograms {
		if w, c := runOnEngine(p.src, true, runtime.Limits{}), runOnEngine(p.src, false, runtime.Limits{}); w != c {
			t.Errorf("%s: tree-walker %+v, compiled %+v", p.name, w, c)
		}
	}
}
