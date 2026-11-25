package check

import (
	"reflect"
	"testing"
)

// http(req) is a net effect. Its host is known when req is a hash literal,
// or a name bound once to one, whose url key is a known string.
func TestHTTPNeeds(t *testing.T) {
	for _, tt := range []struct {
		src          string
		needs, notes []string
	}{
		{`http({"url": "https://api.example.com/x", "method": "POST", "body": "b"});`,
			[]string{"net=api.example.com:443"}, nil},
		{`let u = "http://127.0.0.1:8080/"; http({"method": "GET", "url": u + "x"});`,
			[]string{"net=127.0.0.1:8080"}, nil},
		{`let req = {"url": "https://a.example/", "headers": {"Authorization": "x"}}; http(req);`,
			[]string{"net=a.example:443"}, nil},
		// 0.0.0.0 lands on loopback, which the dial check also asks for.
		{`http({"url": "http://0.0.0.0:9/"});`,
			[]string{"net=0.0.0.0:9", "net=127.0.0.1:9"}, nil},
		{`let get = fn(u) { http({"url": u}) };`,
			nil, []string{"http at 1:19: host not statically known"}},
		{`let get = fn(r) { http(r) };`,
			nil, []string{"http at 1:19: host not statically known"}},
		{`let k = fn() { "url" }; http({k(): "https://x.example/"});`,
			nil, []string{"http at 1:25: host not statically known"}},
		// Two keys that may both be url: which one wins is not the check's.
		{`http({"url": "https://a.example/", "url": "https://b.example/"});`,
			nil, []string{"http at 1:1: host not statically known"}},
		{`http({"url": 5});`,
			nil, []string{"http at 1:1: host not statically known"}},
		{`http({"method": "GET"});`,
			nil, []string{"http at 1:1: never allowed: the request has no url"}},
		{`http({"url": "ftp://x/"});`,
			nil, []string{"http at 1:1: never allowed: invalid url"}},
		{`http();`,
			nil, []string{"http at 1:1: never allowed: 0 arguments (it takes 1)"}},
		{`let f = http;`,
			nil, []string{"http at 1:9: used as a value: what it needs is not statically known"}},
	} {
		needs, notes := checked(t, tt.src)
		if !reflect.DeepEqual(needs, tt.needs) || !reflect.DeepEqual(notes, tt.notes) {
			t.Errorf("%s\nneeds = %q, want %q\nnotes = %q, want %q", tt.src, needs, tt.needs, notes, tt.notes)
		}
	}
}

// parse_json needs no grant, and says nothing.
func TestParseJSONIsPure(t *testing.T) {
	needs, notes := checked(t, `let v = parse_json("[1]"); let f = parse_json; f("2");`)
	if needs != nil || notes != nil {
		t.Errorf("needs = %q, notes = %q", needs, notes)
	}
}
