package check

import (
	"fmt"
	"math/rand"
	"reflect"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
)

// leastNaive is least as first built: every need against every other.
// It is the oracle for least's index.
func leastNaive(needs []Need) []Need {
	var out []Need
	for i, n := range needs {
		drop := false
		nc := runtime.Capability{Kind: n.Kind, Scope: n.Scope}
		for j, m := range needs {
			if j == i || m.Kind != n.Kind || (n.Kind != runtime.Net && n.Kind != runtime.Env) {
				continue
			}
			mc := runtime.Capability{Kind: m.Kind, Scope: m.Scope}
			if mc.Covers(nc) && (j < i || !nc.Covers(mc)) {
				drop = true
				break
			}
		}
		if !drop {
			out = append(out, n)
		}
	}
	return out
}

// least compares each need with the few needs that can cover it, not
// with every other one: arv check reads untrusted scripts, and a quadratic
// pass let a 430 KB file stall it (and run --manifest) for 17 s.
func TestLeastIsNotQuadratic(t *testing.T) {
	old := covers
	defer func() { covers = old }()
	calls := 0
	covers = func(c, r runtime.Capability) bool { calls++; return old(c, r) }
	const n = 4000
	var needs []Need
	for i := 0; i < n; i++ {
		needs = append(needs,
			Need{Kind: runtime.Net, Scope: fmt.Sprintf("h%d.example", i)},
			Need{Kind: runtime.Net, Scope: fmt.Sprintf("10.0.%d.%d:80", i/256, i%256)},
			Need{Kind: runtime.Env, Scope: fmt.Sprintf("V%d", i)},
			Need{Kind: runtime.Net, Scope: fmt.Sprintf("h%d.other:443", i)}) // covered by nothing
	}
	needs = append(needs, Need{Kind: runtime.Net, Scope: "*.example"}, Need{Kind: runtime.Net, Scope: "10.0.0.0/16"},
		Need{Kind: runtime.Env, Scope: "*"})
	got := least(distinct(needs))
	if len(got) != n+3 || got[0].Scope != "*.example" || got[1].Scope != "10.0.0.0/16" || got[n+2].Scope != "*" {
		t.Errorf("least kept %d needs, want *.example, 10.0.0.0/16, the %d h*.other and env *", len(got), n)
	}
	if calls > 4*len(needs) {
		t.Errorf("least made %d Covers calls for %d needs, want at most %d", calls, len(needs), 4*len(needs))
	}
}

// least keeps exactly what comparing every pair keeps.
func TestLeastMatchesEveryPairComparison(t *testing.T) {
	pool := []string{"*", "*:443", "*:80", "example.com", "example.com:443", "a.example.com", "a.example.com:443",
		"*.example.com", "*.example.com:443", "*.a.example.com", "b.a.example.com:80", "127.0.0.1", "127.0.0.1:80",
		"127.0.0.0/8", "127.0.0.0/24", "10.0.0.0/8", "0.0.0.0/0", "8.8.8.8:53", "8.8.8.8", "::1", "[::1]:80",
		"::/0", "::ffff:0:0/96", "::ffff:0:0/100", "::/80", "::ffff:7f00:1", "fd00::/8", "fd00::1", "[fd00::1]:443", "fd00::/16", "::/127", "[::]:80", "0.0.0.0:80", "1.2.3:80"}
	for i, s := range pool { // as the runtime normalises them
		n, err := literalScope(runtime.Net, s)
		if err != nil {
			t.Fatalf("%s: %v", s, err)
		}
		pool[i] = n
	}
	envs := []string{"*", "HOME", "PATH", "A"}
	r := rand.New(rand.NewSource(81))
	for i := 0; i < 2000; i++ {
		var needs []Need
		for k := r.Intn(10); k >= 0; k-- {
			if r.Intn(4) == 0 {
				needs = append(needs, Need{Kind: runtime.Env, Scope: envs[r.Intn(len(envs))]})
			} else {
				needs = append(needs, Need{Kind: runtime.Net, Scope: pool[r.Intn(len(pool))]})
			}
		}
		if got, want := least(needs), leastNaive(needs); !reflect.DeepEqual(got, want) {
			t.Fatalf("needs %v: least = %v, every pair = %v", needs, got, want)
		}
	}
}
