package evaluator

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// fakeK8s is a host effect, apply(namespace, manifest), needing
// k8s(namespace). It counts the calls that reached Fn.
type fakeK8s struct {
	mu    sync.Mutex
	calls []string
	fail  error        // returned by Fn when set
	ret   value.Object // returned by Fn when set
}

func (f *fakeK8s) effect() Effect {
	return Effect{
		Kind: "k8s",
		Scope: func(args []value.Object) (string, error) {
			if len(args) != 2 {
				return "", errors.New("want 2 arguments (namespace, manifest)")
			}
			ns, ok := args[0].(*value.String)
			if !ok {
				return "", errors.New("namespace must be STRING")
			}
			return ns.Value, nil
		},
		Fn: func(t *runtime.Task, args ...value.Object) (value.Object, error) {
			f.mu.Lock()
			f.calls = append(f.calls, args[0].Inspect()+"/"+args[1].Inspect())
			f.mu.Unlock()
			if f.fail != nil {
				return nil, f.fail
			}
			if f.ret != nil {
				return f.ret, nil
			}
			return value.NewHash(map[string]value.Object{"applied": args[1], "ok": &value.Boolean{Value: true}}), nil
		},
	}
}

func (f *fakeK8s) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func k8sKinds(t *testing.T) *runtime.Kinds {
	t.Helper()
	k, err := runtime.NewKinds(runtime.KindSpec{Kind: "k8s"})
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// runHost runs src as the root task holding caps, with effects bound in
// its global environment as arv.Run binds them.
func runHost(t *testing.T, kinds *runtime.Kinds, effects map[string]Effect, src string, caps ...runtime.Capability) value.Object {
	t.Helper()
	host, err := HostBindings(kinds, effects)
	if err != nil {
		t.Fatal(err)
	}
	env := value.NewEnvironment()
	for name, b := range host {
		env.Set(name, b)
	}
	root := runtime.NewRoot(context.Background(), runtime.NewCapSetKinds(kinds, caps...), io.Discard)
	return New(root).Run(parse(t, src), env)
}

// There is no way to bind an effect no grant checks: an effect must name
// a declared host kind and say how a call's scope is found, and its name
// may not hide a builtin, a keyword or a kind's constructor.
func TestHostBindingsRefusesUncheckedEffects(t *testing.T) {
	k := k8sKinds(t)
	ok := (&fakeK8s{}).effect()
	with := func(f func(*Effect)) Effect { e := ok; f(&e); return e }
	for _, tt := range []struct {
		name, effect string
		e            Effect
		want         string
	}{
		{"no kind", "apply", with(func(e *Effect) { e.Kind = "" }), "no capability kind"},
		{"built-in kind", "apply", with(func(e *Effect) { e.Kind = runtime.Net }), "built in"},
		{"undeclared kind", "apply", with(func(e *Effect) { e.Kind = "queue" }), "not declared"},
		{"no scope", "apply", with(func(e *Effect) { e.Scope = nil }), "no Scope"},
		{"no fn", "apply", with(func(e *Effect) { e.Fn = nil }), "no Fn"},
		{"empty name", "", ok, "empty name"},
		{"bad name", "k8s-apply", ok, "letter"},
		{"digit first", "1apply", ok, "letter"},
		{"keyword", "spawn", ok, "keyword"},
		{"builtin", "http_get", ok, "builtin"},
		{"puts", "puts", ok, "builtin"},
		{"kind constructor", "k8s", ok, "custom kind"},
		{"too long", strings.Repeat("a", MaxEffectName+1), ok, "longer than"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := HostBindings(k, map[string]Effect{tt.effect: tt.e})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("HostBindings err = %v, want one mentioning %q", err, tt.want)
			}
		})
	}
	for _, bad := range []runtime.Kind{"len", "match"} {
		kk, err := runtime.NewKinds(runtime.KindSpec{Kind: bad})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := HostBindings(kk, nil); err == nil {
			t.Errorf("custom kind %q hides a builtin or keyword, and was accepted", bad)
		}
	}
	host, err := HostBindings(k, map[string]Effect{"apply": ok})
	if err != nil || host["apply"] == nil || host["k8s"] == nil || len(host) != 2 {
		t.Fatalf("HostBindings = %v, %v; want apply and the k8s constructor", host, err)
	}
}

// The calling task's grants are checked before Fn: a call without a
// covering grant never reaches the host.
func TestHostEffectNeedsItsGrant(t *testing.T) {
	k := k8sKinds(t)
	f := &fakeK8s{}
	eff := map[string]Effect{"apply": f.effect()}
	staging := runtime.Capability{Kind: "k8s", Scope: "staging"}

	wantDenied(t, runHost(t, k, eff, `apply("staging", "web.yaml")`), `k8s("staging")`)
	wantDenied(t, runHost(t, k, eff, `apply("prod", "web.yaml")`, staging), `k8s("prod")`)
	if f.count() != 0 {
		t.Fatalf("Fn ran %d times without a grant", f.count())
	}
	got := runHost(t, k, eff, `let r = apply("staging", "web.yaml"); [r["applied"], r["ok"] == true]`, staging)
	if got.Inspect() != "[web.yaml, true]" || f.count() != 1 {
		t.Fatalf("got %s after %d calls, want [web.yaml, true] after 1", got.Inspect(), f.count())
	}
}

// A child holds a host grant only if spawn passed it one its parent
// covers (I2), and the kind's constructor names it.
func TestHostEffectAttenuates(t *testing.T) {
	k := k8sKinds(t)
	f := &fakeK8s{}
	eff := map[string]Effect{"apply": f.effect()}
	staging := runtime.Capability{Kind: "k8s", Scope: "staging"}
	const deploy = "let deploy = fn(ns) { apply(ns, \"m.yaml\")[\"applied\"] };\n"

	got := runHost(t, k, eff, deploy+`await spawn deploy("staging") with [k8s("staging")]`, staging)
	if got.Inspect() != "m.yaml" {
		t.Fatalf("child with the grant: got %s", got.Inspect())
	}
	wantDenied(t, runHost(t, k, eff, deploy+`await spawn deploy("staging")`, staging), `k8s("staging")`)
	wantDenied(t, runHost(t, k, eff, deploy+`await spawn deploy("prod") with [k8s("prod")]`, staging), `k8s("prod")`)
	nested := deploy + "let outer = fn() { await spawn deploy(\"staging\") with [k8s(\"staging\")] };\n"
	wantDenied(t, runHost(t, k, eff, nested+`await spawn outer() with []`, staging), `k8s("staging")`)
	if f.count() != 1 {
		t.Fatalf("Fn ran %d times, want once", f.count())
	}
	if got := runHost(t, k, eff, `k8s("a/b")`); got.Inspect() != `k8s("a/b")` {
		t.Fatalf("constructor: got %s", got.Inspect())
	}
	wantKind(t, runHost(t, k, eff, `k8s("")`), runtime.KindRuntime)
	wantKind(t, runHost(t, k, eff, `k8s(1)`), runtime.KindRuntime)
}
