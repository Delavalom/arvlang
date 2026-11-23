package evaluator

import (
	"bufio"
	"bytes"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/value"
)

var tokenRE = regexp.MustCompile("^[!#$%&'*+\\-.^_`|~0-9A-Za-z]+$")

// smuggling are the headers http refuses to set, lower case.
var smuggling = []string{"host", "content-length", "connection", "keep-alive", "proxy-connection",
	"proxy-authenticate", "proxy-authorization", "te", "trailer", "transfer-encoding", "upgrade"}

// FuzzHTTPRequestHeaders: requestSpec accepts a request exactly when its
// method is allowed, its header name is a token that is not forbidden and
// its value has no control byte but tab. What it accepts writes as an
// HTTP/1.1 request that reads back with exactly that one header (no line
// split into two) and that method.
func FuzzHTTPRequestHeaders(f *testing.F) {
	for _, s := range [][3]string{
		{"X-A", "1", "POST"}, {"X-A", "1\r\nHost: evil", "GET"}, {"Host", "x", "GET"}, {"x-a\n", "1", "PUT"},
		{"Authorization", "Bearer \t t ", "DELETE"}, {"TE", "trailers", "GET"}, {"X", "é\xff", "get"},
		{"X", "a\x00", "HEAD"}, {"content-LENGTH", "0", "PATCH"}, {"", "", "OPTIONS"}, {"X:Y", "z", "TRACE"},
	} {
		f.Add(s[0], s[1], s[2])
	}
	f.Fuzz(func(t *testing.T, name, val, method string) {
		req := value.NewHash(map[string]value.Object{
			"url":     &value.String{Value: "http://example.com/p"},
			"method":  &value.String{Value: method},
			"headers": strs(map[string]string{name: val}),
			"body":    &value.String{Value: "b"},
		})
		spec, errObj := requestSpec([]value.Object{req})
		ctl := strings.ContainsFunc(val, func(r rune) bool { return r < 0x20 && r != '\t' || r == 0x7f })
		want := slices.Contains(methods, method) && tokenRE.MatchString(name) &&
			!slices.Contains(smuggling, strings.ToLower(name)) && !ctl &&
			len(name)+len(val)+4 <= maxReqHeaderBytes
		if errObj != nil {
			if want || !strings.HasPrefix(errObj.Message, "http: ") || len(errObj.Message) > 2048 {
				t.Fatalf("requestSpec(%q: %q, %q) = %s", name, val, method, errObj.Message)
			}
			return
		}
		if !want {
			t.Fatalf("requestSpec(%q: %q, %q) accepted it", name, val, method)
		}
		r, err := http.NewRequest(spec.method, spec.url, strings.NewReader(*spec.body))
		if err != nil {
			t.Fatal(err)
		}
		for _, h := range spec.headers {
			r.Header[h[0]] = []string{h[1]}
		}
		var wire bytes.Buffer
		if err := r.Write(&wire); err != nil {
			t.Fatalf("writing %q: %q: %v", name, val, err)
		}
		back, err := http.ReadRequest(bufio.NewReader(&wire))
		if err != nil {
			t.Fatalf("reading back %q: %q: %v\n%q", name, val, err, wire.String())
		}
		back.Header.Del("User-Agent")
		if back.Header.Get("Content-Length") != "1" {
			t.Fatalf("%q: %q: Content-Length %q", name, val, back.Header.Get("Content-Length"))
		}
		back.Header.Del("Content-Length")
		canon := http.CanonicalHeaderKey(name)
		if strings.EqualFold(name, "User-Agent") {
			return
		}
		if back.Method != method || len(back.Header) != 1 || back.Header.Get(canon) != strings.Trim(val, " \t") ||
			back.Host != "example.com" {
			t.Fatalf("%q: %q went out as %s %v (host %q)", name, val, back.Method, back.Header, back.Host)
		}
	})
}
