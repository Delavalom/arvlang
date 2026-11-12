package evaluator

import (
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
)

// I1 for host effects: a capability value passed to an effect is data,
// not authority. A call whose arguments include k8s("prod"), at the top
// level or nested, is denied unless the calling task's grants cover
// k8s("prod"), and Fn never runs (mutation M07).
func TestHostEffectCapabilityValueIsNotAGrant(t *testing.T) {
	k := k8sKinds(t)
	f := &fakeK8s{}
	eff := map[string]Effect{"apply": f.effect()}
	prod := runtime.Capability{Kind: "k8s", Scope: "prod"}
	staging := runtime.Capability{Kind: "k8s", Scope: "staging"}

	for _, tt := range []struct {
		name string
		src  string
		caps []runtime.Capability
	}{
		{"no grant", `apply("prod", k8s("prod"))`, nil},
		{"other grant", `apply("prod", k8s("prod"))`, []runtime.Capability{staging}},
		{"bound first", `let c = k8s("prod"); apply("prod", c)`, nil},
		{"in a list", `apply("prod", [k8s("prod")])`, nil},
		{"in a hash", `apply("prod", {"cap": k8s("prod")})`, nil},
		// The root holds k8s("prod"), but the child is spawned without
		// `with`, so it holds nothing; the value it is handed is no grant.
		{"spawn argument", `let f = fn(c) { apply("prod", c) }; await spawn f(k8s("prod"))`, []runtime.Capability{prod}},
		{"spawn with nothing", `let f = fn(c) { apply("prod", c) }; await spawn f(k8s("prod")) with []`, []runtime.Capability{prod}},
		{"spawn with another grant", `let f = fn(c) { apply("prod", c) }; await spawn f(k8s("prod")) with [k8s("staging")]`, []runtime.Capability{prod, staging}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			before := f.count()
			wantDenied(t, runHost(t, k, eff, tt.src, tt.caps...), `k8s("prod")`)
			if n := f.count() - before; n != 0 {
				t.Fatalf("Fn ran %d times for a task without the grant", n)
			}
		})
	}
	// The same call goes through once the task does hold the grant: what
	// was denied above was the missing grant, not the argument.
	got := runHost(t, k, eff, `apply("prod", "m")["applied"]`, prod)
	if got.Inspect() != "m" || f.count() != 1 {
		t.Fatalf("with the grant: got %s after %d calls", got.Inspect(), f.count())
	}
}
