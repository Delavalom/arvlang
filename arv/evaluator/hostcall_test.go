package evaluator

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// What Fn returns or fails with becomes an Arv value or error.
func TestHostEffectErrorsAreArvErrors(t *testing.T) {
	k := k8sKinds(t)
	staging := runtime.Capability{Kind: "k8s", Scope: "staging"}
	run := func(f *fakeK8s, src string) value.Object {
		return runHost(t, k, map[string]Effect{"apply": f.effect()}, src, staging)
	}
	e := wantKind(t, run(&fakeK8s{fail: errors.New("boom")}, `apply("staging", "m")`), runtime.KindEffect)
	if e.Message != "apply: boom" || e.Pos != "1:1" {
		t.Fatalf("error %q at %q", e.Message, e.Pos)
	}
	e = wantKind(t, run(&fakeK8s{ret: &value.Error{Message: "bad manifest", Kind: runtime.KindBudget}}, `apply("staging", "m")`), runtime.KindEffect)
	if e.Message != "apply: bad manifest" {
		t.Fatalf("error %q", e.Message)
	}
	denied := &runtime.DeniedError{Want: runtime.Capability{Kind: "k8s", Scope: "x"}}
	wantKind(t, run(&fakeK8s{fail: denied}, `apply("staging", "m")`), runtime.KindCapability)
	e = wantKind(t, run(&fakeK8s{fail: errors.New(strings.Repeat("x", 1<<20))}, `apply("staging", "m")`), runtime.KindEffect)
	if len(e.Message) > maxHostErr+3 {
		t.Fatalf("a 1 MiB host error became a %d-byte Arv error", len(e.Message))
	}
	f := &fakeK8s{}
	e = wantKind(t, run(f, `apply("staging")`), runtime.KindRuntime)
	if !strings.Contains(e.Message, "want 2 arguments") || f.count() != 0 {
		t.Fatalf("error %q after %d calls", e.Message, f.count())
	}
	e = wantKind(t, run(&fakeK8s{ret: &value.Builtin{}}, `apply("staging", "m")`), runtime.KindRuntime)
	if !strings.Contains(e.Message, "not data") {
		t.Fatalf("error %q", e.Message)
	}
	got := run(&fakeK8s{fail: errors.New("boom")}, `match apply("staging", "m") { ok(v) => v, err(e) => e["kind"] }`)
	if got.Inspect() != "effect" {
		t.Fatalf("match saw %s", got.Inspect())
	}
	wantKind(t, run(&fakeK8s{}, `apply(k8s("staging"), "m")`), runtime.KindRuntime)
}

// hostGate is the seam the Phase 6 effect gate takes over (see 16-phase82.md):
// it sees each admitted call, by name and arguments, and no denied one.
func TestHostEffectGoesThroughTheGate(t *testing.T) {
	old := hostGate
	defer func() { hostGate = old }()
	var gated []string
	hostGate = func(t *runtime.Task, name string, args []value.Object, do func() value.Object) value.Object {
		gated = append(gated, t.ID+" "+name+" "+args[0].Inspect())
		return do()
	}
	k := k8sKinds(t)
	f := &fakeK8s{}
	staging := runtime.Capability{Kind: "k8s", Scope: "staging"}
	runHost(t, k, map[string]Effect{"apply": f.effect()}, `apply("prod", "m")`, staging)
	got := runHost(t, k, map[string]Effect{"apply": f.effect()}, `apply("staging", "m")["applied"]`, staging)
	if got.Inspect() != "m" || len(gated) != 1 || gated[0] != "root apply staging" {
		t.Fatalf("got %s, gate saw %q", got.Inspect(), gated)
	}
}

// A Scope's error is cut like Fn's, at a rune boundary, and a scope longer
// than runtime.MaxScopeLen is refused before the grants are searched.
func TestHostEffectScopeErrors(t *testing.T) {
	k := k8sKinds(t)
	staging := runtime.Capability{Kind: "k8s", Scope: "staging"}
	f := &fakeK8s{}
	scopeIs := func(s string, err error) Effect {
		e := f.effect()
		e.Scope = func([]value.Object) (string, error) { return s, err }
		return e
	}
	run := func(e Effect) value.Object {
		return runHost(t, k, map[string]Effect{"apply": e}, `apply("staging", "m")`, staging)
	}
	e := wantKind(t, run(scopeIs("", errors.New(strings.Repeat("x", 1<<20)))), runtime.KindRuntime)
	if len(e.Message) > maxHostErr+3 {
		t.Fatalf("a 1 MiB Scope error became a %d-byte Arv error", len(e.Message))
	}
	e = wantKind(t, run(scopeIs(strings.Repeat("s", runtime.MaxScopeLen+1), nil)), runtime.KindRuntime)
	if !strings.Contains(e.Message, "scope is") || strings.Contains(e.Message, "sss") {
		t.Fatalf("error %q", e.Message)
	}
	for _, msg := range []string{strings.Repeat("é", 600), "x" + strings.Repeat("é", 600), strings.Repeat("€", 400)} {
		e = wantKind(t, run(scopeIs("", errors.New(msg))), runtime.KindRuntime)
		if !utf8.ValidString(e.Message) || len(e.Message) > maxHostErr+3 {
			t.Fatalf("a Scope error cut to %d bytes, valid UTF-8 %v", len(e.Message), utf8.ValidString(e.Message))
		}
		fail := &fakeK8s{fail: errors.New(msg)}
		e = wantKind(t, runHost(t, k, map[string]Effect{"apply": fail.effect()}, `apply("staging", "m")`, staging), runtime.KindEffect)
		if !utf8.ValidString(e.Message) {
			t.Fatalf("an Fn error cut to invalid UTF-8: ...%q", e.Message[len(e.Message)-8:])
		}
	}
	if f.count() != 0 {
		t.Fatalf("Fn ran %d times", f.count())
	}
}
