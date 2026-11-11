package check

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv"
	"github.com/delavalom/arvlang/arv/runtime"
)

// arv check and the runtime agree on each call: when check reports a need,
// running the call with no grants is denied naming exactly that
// capability (a path made absolute against the working directory, as the
// runtime does); when check says the call is never allowed, the runtime
// fails it some other way. No grant is held, so nothing reaches the
// network or a file.
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
		_, runErr := arv.Run(src, arv.Options{})
		var ae *arv.Error
		if !errors.As(runErr, &ae) {
			t.Errorf("%s: run = %v, want an Arv error", src, runErr)
			continue
		}
		switch {
		case len(r.Sites) == 1 && len(r.Notes) == 0:
			n := r.Sites[0]
			scope := n.Scope
			if n.Kind == runtime.FSRead || n.Kind == runtime.FSWrite {
				scope = runtime.Resolve(cwd, scope)
			}
			want := "capability denied: " + runtime.Capability{Kind: n.Kind, Scope: scope}.String()
			if ae.Kind != runtime.KindCapability || !strings.Contains(ae.Msg, want) {
				t.Errorf("%s: check needs %s, run failed with %s: %s", src, n, ae.Kind, ae.Msg)
			}
		case len(r.Sites) == 0 && len(r.Notes) == 1 && strings.Contains(r.Notes[0].Msg, "never allowed"):
			if ae.Kind == runtime.KindCapability {
				t.Errorf("%s: check says %s, run was denied: %s", src, r.Notes[0], ae.Msg)
			}
		default:
			t.Errorf("%s: report %+v, want one need or one never-allowed note", src, r)
		}
	}
}
