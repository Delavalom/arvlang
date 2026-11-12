package arv

import (
	"errors"
	goruntime "runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// hostRules are reflexive, transitive coverage rules a host might give a
// kind (nil is exact match).
var hostRules = []func(g, r string) bool{
	nil,
	func(g, r string) bool { return r == g || strings.HasPrefix(r, g+"/") },
	func(g, r string) bool { return r == g || strings.HasSuffix(r, "."+g) },
}

// FuzzRunHostScope: scope matching for a host kind, end to end through
// Run. The program gets its scopes from an effect (arg), so any bytes
// reach it unescaped. A call to the team effect reaches Fn exactly when
// the calling task holds a covering grant: the root holds team(g); a
// child is handed team(c), which spawn admits only if team(g) covers it.
// Every call that reached Fn from the child is also covered by the
// root's grant (I2), every failure is an Arv error, and no goroutine
// outlives Run.
func FuzzRunHostScope(f *testing.F) {
	for _, s := range [][3]string{
		{"infra", "infra/db", "infra/db/eu"},
		{"infra", "infra", "infra"},
		{"infra", "infrastructure", "infrastructure"},
		{"infra/db", "infra", "infra/db"},
		{"example.com", "a.example.com", "b.a.example.com"},
		{"", "x", "x"},
		{"x", "", "x"},
		{"x", "x", ""},
		{"a\x00b", "a\x00b/c", "a\x00b/c"},
		{"é", "é/\"", "é/\"/\\n"},
		{"x", "x", strings.Repeat("x", runtime.MaxScopeLen+1)},
	} {
		for m := range hostRules {
			f.Add(uint8(m), s[0], s[1], s[2])
		}
	}
	f.Fuzz(func(t *testing.T, mode uint8, g, c, r string) {
		rule := hostRules[int(mode)%len(hostRules)]
		cover := func(g, r string) bool {
			if g == "" || r == "" || len(g) > runtime.MaxScopeLen || len(r) > runtime.MaxScopeLen {
				return false
			}
			if rule == nil {
				return g == r
			}
			return rule(g, r)
		}
		var n atomic.Int64
		inputs := []string{c, r}
		opts := Options{
			Kinds: []runtime.KindSpec{{Kind: "team", Covers: rule}},
			Caps:  []runtime.Capability{{Kind: "fzin", Scope: "in"}},
			Effects: map[string]Effect{
				"arg": {Kind: "fzin", Scope: func([]value.Object) (string, error) { return "in", nil },
					Fn: func(_ *runtime.Task, a ...value.Object) (value.Object, error) {
						return &value.String{Value: inputs[a[0].(*value.Integer).Value]}, nil
					}},
				"call": {Kind: "team", Scope: nsScope,
					Fn: func(*runtime.Task, ...value.Object) (value.Object, error) {
						n.Add(1)
						return &value.String{Value: "called"}, nil
					}},
			},
		}
		if g != "" && len(g) <= runtime.MaxScopeLen {
			opts.Caps = append(opts.Caps, runtime.Capability{Kind: "team", Scope: g})
		}
		run := func(src string, want bool) {
			t.Helper()
			n.Store(0)
			before := goruntime.NumGoroutine()
			got, err := Run(src, opts)
			noLeak(t, before, src)
			called := n.Load()
			if called > 1 || (called == 1) != want {
				t.Fatalf("g=%q c=%q r=%q: %s: Fn called %d times, want called = %v (err %v)", g, c, r, src, called, want, err)
			}
			if want != (err == nil) {
				t.Fatalf("g=%q c=%q r=%q: %s: err = %v, want success = %v", g, c, r, src, err, want)
			}
			var ae *Error
			if err != nil && !errors.As(err, &ae) {
				t.Fatalf("%s: err = %#v, want an Arv error", src, err)
			}
			if err == nil && got.Inspect() != "called" {
				t.Fatalf("%s = %v", src, got.Inspect())
			}
		}
		run(`call(arg(1))`, cover(g, r))
		viaChild := cover(g, c) && cover(c, r)
		if viaChild && !cover(g, r) {
			t.Fatalf("I2: g=%q c=%q r=%q: the child may call what the root may not", g, c, r)
		}
		run(`let f = fn(s) { call(s) }; await spawn f(arg(1)) with [team(arg(0))]`, viaChild)
		// A grandchild handed the same grant again.
		run(`let f = fn(s) { call(s) }; let h = fn(s, cc) { await spawn f(s) with [team(cc)] }; let c = arg(0); await spawn h(arg(1), c) with [team(c)]`, viaChild)
	})
}
