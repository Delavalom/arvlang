package evaluator

import (
	"context"
	"io"
	"sync/atomic"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// arvKeywords is the lexer's keyword table, written out again so the
// oracle below does not share code with HostBindings.
var arvKeywords = map[string]bool{"fn": true, "let": true, "true": true, "false": true, "if": true,
	"else": true, "return": true, "spawn": true, "with": true, "await": true, "scope": true,
	"for": true, "in": true, "match": true}

// bindable reports whether a host may bind name: an identifier of at most
// MaxEffectName bytes that is neither a keyword nor a builtin.
func bindable(name string) bool {
	if name == "" || len(name) > MaxEffectName || arvKeywords[name] || builtins[name] != nil {
		return false
	}
	for i, c := range []byte(name) {
		letter := 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || c == '_'
		if !letter && (i == 0 || c < '0' || c > '9') {
			return false
		}
	}
	return true
}

// FuzzHostBindings: there is no way to register an unchecked host effect.
// HostBindings succeeds exactly when the effect names a declared host
// kind and has a Scope and an Fn, and every name it binds is bindable and
// distinct. What it returns calls Fn only for a task holding a grant that
// covers the call's scope (exact match here), and never for another kind.
func FuzzHostBindings(f *testing.F) {
	for _, s := range [][4]string{
		{"k8s", "k8s_apply", "k8s", "staging"},
		{"k8s", "k8s_apply", "", "staging"},
		{"k8s", "k8s_apply", "net", "staging"},
		{"k8s", "k8s", "k8s", "staging"},
		{"k8s", "puts", "k8s", "staging"},
		{"k8s", "spawn", "k8s", "staging"},
		{"len", "apply", "len", "a"},
		{"fn", "apply", "fn", "a"},
		{"k8s", "Apply_2", "queue", "a"},
		{"k8s", "_", "k8s", ""},
		{"k8s", "net", "k8s", "a"},
	} {
		f.Add(s[0], s[1], s[2], s[3], true, true)
	}
	f.Add("k8s", "k8s_apply", "k8s", "a", false, true)
	f.Add("k8s", "k8s_apply", "k8s", "a", true, false)
	f.Fuzz(func(t *testing.T, kind, name, ekind, scope string, hasScope, hasFn bool) {
		kinds, err := runtime.NewKinds(runtime.KindSpec{Kind: runtime.Kind(kind)})
		if err != nil {
			return // NewKinds' own rules: FuzzHostKindCovers
		}
		var calls atomic.Int64
		e := Effect{Kind: runtime.Kind(ekind)}
		if hasScope {
			e.Scope = func(a []value.Object) (string, error) { return scope, nil }
		}
		if hasFn {
			e.Fn = func(*runtime.Task, ...value.Object) (value.Object, error) {
				calls.Add(1)
				return &value.String{Value: "done"}, nil
			}
		}
		out, err := HostBindings(kinds, map[string]Effect{name: e})
		want := bindable(kind) && bindable(name) && name != kind && ekind == kind && hasScope && hasFn
		if (err == nil) != want {
			t.Fatalf("HostBindings(kind %q, effect %q of kind %q, Scope %v, Fn %v) err = %v, want success = %v",
				kind, name, ekind, hasScope, hasFn, err, want)
		}
		if err != nil {
			if out != nil {
				t.Fatalf("HostBindings failed (%v) but returned bindings %v", err, out)
			}
			return
		}
		if len(out) != 2 || out[kind] == nil || out[kind].Fn == nil || out[name] == nil || out[name].Charging == nil {
			t.Fatalf("bindings = %v, want a constructor %q and a charging effect %q", out, kind, name)
		}
		call := func(caps ...runtime.Capability) value.Object {
			root := runtime.NewRoot(context.Background(), runtime.NewCapSetKinds(kinds, caps...), io.Discard)
			return out[name].Charging(root, nil)
		}
		for _, tt := range []struct {
			caps []runtime.Capability
			ok   bool
		}{
			{nil, false},
			{[]runtime.Capability{{Kind: runtime.Env, Scope: "*"}, {Kind: runtime.Net, Scope: "*"}}, false},
			{[]runtime.Capability{{Kind: runtime.Kind(kind), Scope: scope + "x"}}, false},
			{[]runtime.Capability{{Kind: runtime.Kind(kind), Scope: scope}}, scope != "" && len(scope) <= runtime.MaxScopeLen},
		} {
			calls.Store(0)
			v := call(tt.caps...)
			ev, isErr := v.(*value.Error)
			if tt.ok != (calls.Load() == 1) || tt.ok == isErr {
				t.Fatalf("holding %v, scope %q: Fn called %d times, result %v", tt.caps, scope, calls.Load(), v.Inspect())
			}
			if isErr && len(scope) <= runtime.MaxScopeLen && ev.ErrKind() != runtime.KindCapability {
				t.Fatalf("holding %v, scope %q: error of kind %q, want a denial", tt.caps, scope, ev.ErrKind())
			}
		}
		c := out[kind].Fn(nil, &value.String{Value: scope})
		if cv, ok := c.(*value.Capability); ok {
			if cv.Cap != (runtime.Capability{Kind: runtime.Kind(kind), Scope: scope}) {
				t.Fatalf("constructor(%q) = %s", scope, cv.Cap)
			}
		} else if _, ok := c.(*value.Error); !ok || scope != "" && len(scope) <= runtime.MaxScopeLen {
			t.Fatalf("constructor(%q) = %v", scope, c.Inspect())
		}
	})
}
