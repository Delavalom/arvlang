package check

import (
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

// http_get to the unspecified address is checked twice at run time: the
// request against the URL's host, then the dial, which lands on loopback,
// against a loopback grant (runtime.Task.RequireDial). The host is an IP
// literal, so arv check works out the second need without resolving
// anything.
func TestUnspecifiedAddressAlsoNeedsLoopback(t *testing.T) {
	for _, tt := range []struct {
		url  string
		want []string
	}{
		{"http://0.0.0.0:9/", []string{"net=0.0.0.0:9", "net=127.0.0.1:9"}},
		{"http://[::]:9/", []string{"net=[::1]:9", "net=[::]:9"}},
		{"http://[::ffff:0.0.0.0]:9/", []string{"net=0.0.0.0:9", "net=127.0.0.1:9"}},
		{"https://0.0.0.0/", []string{"net=0.0.0.0:443", "net=127.0.0.1:443"}},
		// The dial need is the request's own: nothing to add.
		{"http://127.0.0.1:9/", []string{"net=127.0.0.1:9"}},
		{"http://10.1.2.3/", []string{"net=10.1.2.3:80"}},
		{"http://[::1]:9/", []string{"net=[::1]:9"}},
		{"http://8.8.8.8/", []string{"net=8.8.8.8:80"}},
	} {
		needs, notes := checked(t, `http_get("`+tt.url+`");`)
		if !reflect.DeepEqual(needs, tt.want) || notes != nil {
			t.Errorf("%s: needs %q, notes %q; want %q", tt.url, needs, notes, tt.want)
		}
	}
}

// A host that is not an IP literal but that a resolver may read as one
// (127.1, 2130706433, 0x7f.1) gets a note: its dial-time need depends on
// the resolver, like any name's.
func TestNumericNamesGetANote(t *testing.T) {
	for _, host := range []string{"127.1", "2130706433", "0x7f.1", "0x7f000001", "1.2.3"} {
		needs, notes := checked(t, `http_get("http://`+host+`:9/");`)
		if !reflect.DeepEqual(needs, []string{"net=" + host + ":9"}) || len(notes) != 1 ||
			!strings.Contains(notes[0], "may read it as an IP address") {
			t.Errorf("%s: needs %q, notes %q", host, needs, notes)
		}
	}
	for _, host := range []string{"example.com", "a1.example", "x0", "0x.example"} {
		if _, notes := checked(t, `http_get("http://`+host+`/");`); notes != nil {
			t.Errorf("%s: notes %q, want none", host, notes)
		}
	}
}

// A header that grants only net=0.0.0.0:P does not cover the dial.
func TestOutsideSeesTheDialNeed(t *testing.T) {
	r, err := Source(`http_get("http://0.0.0.0:9/");`)
	if err != nil {
		t.Fatal(err)
	}
	grants, err := Grants([]Entry{{1, "net=0.0.0.0:9"}}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, m := range Outside(r, grants, "/") {
		got = append(got, m.String())
	}
	if !reflect.DeepEqual(got, []string{"net=127.0.0.1:9"}) {
		t.Errorf("Outside = %q", got)
	}
}

// Running with exactly the reported grants reaches a local server through
// the unspecified address, and leaving either grant out is denied.
func TestUnspecifiedAddressRunsWithTheReportedGrants(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) }))
	defer srv.Close()
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
	urls := []string{"http://0.0.0.0:" + port + "/"}
	if l, err := net.Listen("tcp6", "[::1]:"+port); err == nil {
		srv6 := &httptest.Server{Listener: l, Config: &http.Server{Handler: srv.Config.Handler}}
		srv6.Start()
		defer srv6.Close()
		urls = append(urls, "http://[::]:"+port+"/")
	} else {
		t.Logf("no IPv6 loopback listener (%v): [::] not run", err)
	}
	dir := t.TempDir()
	for _, u := range urls {
		src := `let r = http_get("` + u + `"); if (r["status"] != 200) { error("status") } else { 0 };`
		rep, err := Source(src)
		if err != nil {
			t.Fatal(err)
		}
		var got []Entry
		for _, n := range rep.Needs {
			got = append(got, Entry{Text: n.String()})
		}
		if err := runWithEntries(src, got, dir); err != nil {
			t.Errorf("%s with %v: %v", u, got, err)
		}
		for drop := range got {
			rest := append(append([]Entry(nil), got[:drop]...), got[drop+1:]...)
			if err := runWithEntries(src, rest, dir); err == nil || !strings.Contains(err.Error(), "capability denied") {
				t.Errorf("%s without %s: %v, want a denial", u, got[drop].Text, err)
			}
		}
	}
}
