package runtime

import (
	"errors"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
)

// hostRules are coverage rules a host might declare, each reflexive and
// transitive as KindSpec requires (nil is exact match).
var hostRules = []struct {
	name string
	fn   func(g, r string) bool
}{
	{"exact", nil},
	{"subtree", func(g, r string) bool { return r == g || strings.HasPrefix(r, g+"/") }},
	{"domain", func(g, r string) bool { return r == g || strings.HasSuffix(r, "."+g) }},
	{"any", func(g, r string) bool { return true }},
}

// validKindName is the rule NewKinds documents, written independently.
func validKindName(k string) bool {
	if k == "" || len(k) > MaxKindLen || IsBuiltin(Kind(k)) {
		return false
	}
	for i, c := range []byte(k) {
		if !('a' <= c && c <= 'z' || i > 0 && ('0' <= c && c <= '9' || c == '_')) {
			return false
		}
	}
	return true
}

func validHostScope(s string) bool { return s != "" && len(s) <= MaxScopeLen }

// FuzzHostKindCovers: scope matching for host kinds (Phase 8.2).
//
//   - NewKinds accepts exactly the names its doc allows.
//   - Allows matches an independent oracle: some grant of the same kind,
//     both scopes non-empty, that the kind's rule (or equality) covers.
//     Grants of another host kind or a built-in kind never count, and the
//     rule is never called with an empty scope or another kind's grant.
//   - Admit agrees with Allows, keeps the scope as written, and names the
//     request as given when it denies.
//   - Attenuate agrees with Admit; the child keeps the registry, holds
//     exactly the request, and allows nothing the parent does not (I2).
//   - Without the registry, nothing of a host kind is allowed; host
//     grants change nothing for a built-in request.
func FuzzHostKindCovers(f *testing.F) {
	for i, s := range [][5]string{
		{"k8s", "staging", "prod", "staging", "staging"},
		{"team", "infra", "ops", "infra/db", "infra/db/eu"},
		{"team", "infra", "", "infrastructure", "infra/"},
		{"dns", "example.com", "a.example.com", "b.a.example.com", "xexample.com"},
		{"Bad-Kind", "a", "b", "a", "a"},
		{"net", "*", "x", "x", "x"},
		{"k_9", "*", "*", "*", "**"},
		{"q", "a/", "a//b", "a//b/c", "a/"},
		{"q", strings.Repeat("s", MaxScopeLen), "s", strings.Repeat("s", MaxScopeLen+1), "s"},
	} {
		f.Add(s[0], uint8(i), s[1], s[2], s[3], s[4])
	}
	f.Fuzz(func(t *testing.T, name string, mode uint8, g1, g2, req, probe string) {
		k, err := NewKinds(KindSpec{Kind: Kind(name)})
		if (err == nil) != validKindName(name) {
			t.Fatalf("NewKinds(%q) err = %v, want valid = %v", name, err, validKindName(name))
		}
		if err != nil {
			name = "fz"
		}
		kind := Kind(name)
		rule := hostRules[int(mode)%len(hostRules)]
		var calls atomic.Int64
		var spec KindSpec
		spec.Kind = kind
		if rule.fn != nil {
			spec.Covers = func(g, r string) bool {
				calls.Add(1)
				if g == "" || r == "" {
					t.Errorf("%s: Covers(%q, %q) called with an empty scope", rule.name, g, r)
				}
				return rule.fn(g, r)
			}
		}
		other := Kind("other")
		if kind == other {
			other = "other2"
		}
		k, err = NewKinds(spec, KindSpec{Kind: other, Covers: func(g, r string) bool { return true }})
		if err != nil {
			t.Fatal(err)
		}
		cover := func(g, r string) bool {
			if g == "" || r == "" {
				return false
			}
			if rule.fn == nil {
				return g == r
			}
			return rule.fn(g, r)
		}

		var host []string
		caps := []Capability{{Env, "*"}}
		for _, g := range []string{g1, g2} {
			c, err := k.Literal(kind, g)
			if (err == nil) != validHostScope(g) {
				t.Fatalf("Literal(%s, %d bytes) err = %v", kind, len(g), err)
			}
			if err != nil {
				continue
			}
			if c != (Capability{kind, g}) {
				t.Fatalf("Literal changed %q to %s", g, c)
			}
			host = append(host, g)
			caps = append(caps, c)
		}
		if validHostScope(g1) {
			caps = append(caps, Capability{other, g1})
		}
		s := NewCapSetKinds(k, caps...)
		allows := func(scope string) bool {
			for _, g := range host {
				if cover(g, scope) {
					return true
				}
			}
			return false
		}

		r := Capability{kind, req}
		if got, want := s.Allows(r), allows(req); got != want {
			t.Fatalf("%s: grants %q: Allows(%q) = %v, want %v", rule.name, host, req, got, want)
		}
		c, err := s.Admit(r)
		wantAdmit := validHostScope(req) && allows(req)
		if (err == nil) != wantAdmit {
			t.Fatalf("%s: grants %q: Admit(%q) err = %v, want admitted = %v", rule.name, host, req, err, wantAdmit)
		}
		var denied *DeniedError
		if err != nil && (!errors.As(err, &denied) || denied.Want != r) {
			t.Fatalf("Admit(%q) err = %#v, want a denial naming the request", req, err)
		}
		if err == nil && c != r {
			t.Fatalf("Admit(%q) = %s, want it as written", req, c)
		}
		if s.Allows(Capability{other, req}) != (validHostScope(g1) && g1 != "" && req != "") {
			// The other kind's Covers says yes to everything: only its
			// own grant, other(g1), may count, never this kind's.
			t.Fatalf("Allows(%s(%q)) wrong with grants %v", other, req, caps)
		}

		if NewCapSet(caps...).Allows(r) {
			t.Fatalf("a set without the registry allowed %s", r)
		}
		envReq := Capability{Env, req}
		if s.Allows(envReq) != NewCapSet(Capability{Env, "*"}).Allows(envReq) {
			t.Fatalf("host grants changed the answer for %s", envReq)
		}

		child, attErr := s.Attenuate([]Capability{r})
		if (attErr == nil) != (err == nil) {
			t.Fatalf("Admit(%q) err = %v, but Attenuate err = %v", req, err, attErr)
		}
		if attErr != nil {
			return
		}
		if child.Kinds() != k || !slices.Equal(child.List(), []Capability{r}) {
			t.Fatalf("child = %v (registry kept: %v), want exactly %s", child.List(), child.Kinds() == k, r)
		}
		for _, p := range []string{probe, req, g1, g2, req + "/" + probe, probe + "." + req} {
			pc := Capability{kind, p}
			if child.Allows(pc) && !s.Allows(pc) {
				t.Fatalf("I2: %s: parent %q, child %q allows %q", rule.name, host, req, p)
			}
			if child.Allows(pc) != cover(req, p) {
				t.Fatalf("child %q: Allows(%q) = %v", req, p, child.Allows(pc))
			}
			if child.Allows(Capability{other, p}) || child.Allows(Capability{Env, p}) {
				t.Fatalf("child %q allows %q of a kind it was not handed", req, p)
			}
		}
		if rule.fn == nil && calls.Load() != 0 {
			t.Fatal("exact match called a Covers")
		}
	})
}
