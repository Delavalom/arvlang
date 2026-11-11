package check

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"testing"

	rt "github.com/delavalom/arvlang/arv/runtime"
)

func missString(ms []Miss) string {
	s := ""
	for _, m := range ms {
		s += fmt.Sprintf("[%s err=%v] ", m.Need.String(), m.Err)
	}
	return s
}

// Outside's index gives CapSet.Admit's answer against every grant, for
// net, env and paths through symlinks, a symlink loop, a file and a
// missing directory followed by "..".
func TestOutsideMatchesAdmit(t *testing.T) {
	dir := t.TempDir()
	for _, d := range []string{"a/b", "c"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "a/f"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	for link, target := range map[string]string{"link": "a", "loop": "loop", "up": "..", "deep": "a/b"} {
		if err := os.Symlink(target, filepath.Join(dir, link)); err != nil {
			t.Fatal(err)
		}
	}
	var grants []rt.Capability
	for _, kind := range []rt.Kind{rt.FSRead, rt.FSWrite} {
		for _, p := range []string{".", "a", "a/b", "link", "deep", "c", "missing", "a/f", "/", dir + "/a/b/x"} {
			c, err := rt.Parse(string(kind)+"="+p, dir)
			if err != nil {
				t.Fatalf("%s=%s: %v", kind, p, err)
			}
			grants = append(grants, c)
		}
	}
	nets := []string{"*", "*:443", "example.com", "example.com:443", "a.example.com:80", "*.example.com",
		"*.a.example.com:443", "127.0.0.1", "127.0.0.1:80", "127.0.0.0/8", "10.0.0.0/8", "0.0.0.0/0", "::1",
		"[::1]:80", "::/0", "fd00::/8", "fd00::/16", "[fd00::1]:443", "::ffff:0:0/96", "8.8.8.8"}
	for _, s := range nets {
		c, err := rt.Parse("net="+s, dir)
		if err != nil {
			t.Fatalf("net=%s: %v", s, err)
		}
		grants = append(grants, c)
	}
	for _, s := range []string{"*", "HOME", "PATH"} {
		grants = append(grants, rt.Capability{Kind: rt.Env, Scope: s})
	}
	var sites []Need
	for _, kind := range []rt.Kind{rt.FSRead, rt.FSWrite} {
		for _, p := range []string{"a/x", "a/b/y", "link/b", "missing/../a", "a/f/../b", "loop/x", "a/../a/b",
			"/etc/hosts", "missing/x", "link/../c", "up/x", "deep/../f", ".", "c/../a/b/z", "a/b/x", "/"} {
			sites = append(sites, Need{Kind: kind, Scope: p})
		}
	}
	for _, s := range append(nets, "b.a.example.com:443", "example.org", "10.1.2.3:80", "127.0.0.1:81",
		"10.1.0.0/16", "[fd00::2]:443", "fd00:1::/32", "8.8.4.4:53", "[::ffff:10.0.0.1]:80", "0.0.0.0:80", "[::]:80") {
		n, err := literalScope(rt.Net, s)
		if err != nil {
			t.Fatalf("%s: %v", s, err)
		}
		sites = append(sites, Need{Kind: rt.Net, Scope: n})
	}
	for _, s := range []string{"*", "HOME", "USER"} {
		sites = append(sites, Need{Kind: rt.Env, Scope: s})
	}
	r := rand.New(rand.NewSource(812))
	for i := 0; i < 3000; i++ {
		var g []rt.Capability
		for k := r.Intn(8); k > 0; k-- {
			g = append(g, grants[r.Intn(len(grants))])
		}
		rep := &Report{}
		for k := 1 + r.Intn(10); k > 0; k-- {
			rep.Sites = append(rep.Sites, sites[r.Intn(len(sites))])
		}
		got, want := missString(Outside(rep, g, dir)), missString(outsideNaive(rep, g, dir))
		if got != want {
			t.Fatalf("grants %v, sites %v:\nOutside = %s\nAdmit   = %s", g, rep.Sites, got, want)
		}
	}
}
