package check

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv"
	"github.com/delavalom/arvlang/arv/runtime"
)

// arv check and the runtime agree on each call: when check reports the
// sites s1..sn for it, running it with the grants for s1..sk-1 is denied
// naming exactly sk (a path made absolute against the working directory,
// as the runtime does), so the runtime asks for each site in turn, the
// dial-time one last; when check says the call is never allowed, the
// runtime fails it some other way. The grants held never cover the whole
// call, so nothing reaches the network or a file.
func TestNeedsMatchTheRuntimesRequests(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	spawn := func(c string) string { return `let f = fn() { 1 }; await spawn f() with [` + c + `]` }
	for _, src := range []string{
		`http_get("https://API.GitHub.com./zen")`,
		`http_get("http://127.0.0.1:8080/")`,
		`http_get("http://[::1]/")`,
		`http_get("http://0.0.0.0:9/")`,
		`http_get("http://[::]:9/")`,
		`http_get("http://[::ffff:0.0.0.0]:9/")`,
		`http_get("http://user@example.com/")`,
		`http_get("http://example.com:0/")`,
		`http_get("http://exa mple.com/")`,
		`http_get("http://a_b.example/")`,
		`http_get("ftp://example.com/")`,
		`http_get("http:///p")`,
		`http_get("http://10.0.0.1:99999/")`,
		`read_file("rel/x.txt")`,
		`read_file("/nonexistent-p81/../x")`,
		`read_file("")`,
		`write_file("out/f.json", "t")`,
		`getenv("HOME")`,
		`getenv("")`,
		spawn(`net("API.example.com")`),
		spawn(`net("*.example.com")`),
		spawn(`net("10.0.0.0/8")`),
		spawn(`net("h.example:443")`),
		spawn(`net("h.example:0")`),
		spawn(`net("*")`),
		spawn(`fs_read("rel")`),
		spawn(`fs_write("/tmp")`),
		spawn(`env("A")`),
	} {
		r, err := Source(src)
		if err != nil {
			t.Fatalf("%s: %v", src, err)
		}
		if len(r.Notes) == 1 && len(r.Sites) == 0 && strings.Contains(r.Notes[0].Msg, "never allowed") {
			_, runErr := arv.Run(src, arv.Options{})
			var ae *arv.Error
			if !errors.As(runErr, &ae) || ae.Kind == runtime.KindCapability {
				t.Errorf("%s: check says %s, run failed with %v", src, r.Notes[0], runErr)
			}
			continue
		}
		if len(r.Notes) != 0 || len(r.Sites) == 0 {
			t.Errorf("%s: report %+v, want sites or one never-allowed note", src, r)
			continue
		}
		var held []runtime.Capability
		for _, n := range r.Sites {
			scope := n.Scope
			if n.Kind == runtime.FSRead || n.Kind == runtime.FSWrite {
				scope = runtime.Resolve(cwd, scope)
			}
			c := runtime.Capability{Kind: n.Kind, Scope: scope}
			_, runErr := arv.Run(src, arv.Options{Caps: held})
			var ae *arv.Error
			want := "capability denied: " + c.String()
			if !errors.As(runErr, &ae) || ae.Kind != runtime.KindCapability || !strings.Contains(ae.Msg, want) {
				t.Errorf("%s holding %v: check needs %s, run failed with %v", src, held, n, runErr)
			}
			held = append(held, c)
		}
	}
}
