package evaluator

import (
	"net/http"
	"strings"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

func init() {
	builtins["http"] = &value.Builtin{Charging: httpRequest, Effect: "http"}
}

// Request limits. A response's header block is capped at maxHeaderBytes;
// a request's at the same size, and its body at a response's (maxBody).
const (
	maxReqHeaders     = 64
	maxReqHeaderBytes = maxHeaderBytes
)

// methods are the methods http sends. CONNECT and TRACE are left out.
var methods = []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"}

// forbiddenHeaders are the headers a program cannot set: Host and
// Content-Length are the transport's (from the URL and the body), and the
// hop-by-hop headers describe one connection, which the transport owns.
// Setting any of them could smuggle a second request or reach another
// host than the one the net grant was checked for.
var forbiddenHeaders = map[string]bool{
	"Host": true, "Content-Length": true, "Connection": true, "Keep-Alive": true,
	"Proxy-Connection": true, "Proxy-Authenticate": true, "Proxy-Authorization": true,
	"Te": true, "Trailer": true, "Transfer-Encoding": true, "Upgrade": true,
}

// httpSpec is one request: what http(req) and http_get(url) send.
type httpSpec struct {
	method  string
	url     string
	headers [][2]string // canonical name and value, in the request's order
	body    *string     // nil: no body
}

// size is what sending the request copies, charged before it is sent: the
// body and each header line (name, ": ", value, CRLF).
func (s *httpSpec) size() int64 {
	var n int64
	if s.body != nil {
		n = int64(len(*s.body))
	}
	for _, h := range s.headers {
		n += headerLineSize(h[0], h[1])
	}
	return n
}

func headerLineSize(name, val string) int64 { return int64(len(name) + len(val) + 4) }

// http(req) sends the request a hash describes: url (required), method
// (GET when absent), headers (a hash of strings) and body (a string). It
// returns what http_get returns, {status, body}, and is checked as
// http_get is: the net grant for the URL's host and port, every address
// dialled and every redirect. The request is checked before anything is
// required or sent, and its body and headers are charged before it is
// sent. The effect log records the request as the program wrote it,
// headers and body included.
func httpRequest(t *runtime.Task, charge func(int64) error, args ...value.Object) value.Object {
	spec, errObj := requestSpec(args)
	if errObj != nil {
		return errObj
	}
	return doRequest(t, charge, "http", spec)
}

// requestSpec checks http's argument and turns it into a request. Keys are
// read in the hash's order, so the first bad one is always the same.
func requestSpec(args []value.Object) (httpSpec, *value.Error) {
	var spec httpSpec
	if len(args) != 1 {
		return spec, newError("http: want 1 argument, got %d", len(args))
	}
	h, ok := args[0].(*value.Hash)
	if !ok {
		return spec, newError("http: argument must be HASH, got %s", args[0].Type())
	}
	fields := map[string]value.Object{}
	for _, hk := range h.Keys() {
		p := h.Pairs[hk]
		k, ok := p.Key.(*value.String)
		if !ok {
			return spec, newError("http: request keys must be STRING, got %s", p.Key.Type())
		}
		switch k.Value {
		case "url", "method", "headers", "body":
			fields[k.Value] = p.Value
		default:
			return spec, newError("http: unknown key %s in the request (want url, method, headers, body)", quoteClipped(k.Value))
		}
	}
	u, ok := fields["url"]
	if !ok {
		return spec, newError("http: the request has no url")
	}
	us, ok := u.(*value.String)
	if !ok {
		return spec, newError("http: url must be STRING, got %s", u.Type())
	}
	spec.url = us.Value
	spec.method = http.MethodGet
	if m, ok := fields["method"]; ok {
		ms, ok := m.(*value.String)
		if !ok {
			return spec, newError("http: method must be STRING, got %s", m.Type())
		}
		if !knownMethod(ms.Value) {
			return spec, newError("http: method %s is not one of %s", quoteClipped(ms.Value), strings.Join(methods, ", "))
		}
		spec.method = ms.Value
	}
	if hv, ok := fields["headers"]; ok {
		var errObj *value.Error
		if spec.headers, errObj = requestHeaders(hv); errObj != nil {
			return spec, errObj
		}
	}
	if b, ok := fields["body"]; ok {
		bs, ok := b.(*value.String)
		if !ok {
			return spec, newError("http: body must be STRING, got %s", b.Type())
		}
		if len(bs.Value) > maxBody {
			return spec, newError("http: body is %d bytes long (max %d)", len(bs.Value), maxBody)
		}
		spec.body = &bs.Value
	}
	return spec, nil
}

func knownMethod(m string) bool {
	for _, k := range methods {
		if m == k {
			return true
		}
	}
	return false
}

// requestHeaders checks a request's headers: string names that are HTTP
// tokens, not forbidden and not given twice (in any case), string values
// without CR, LF or another control byte, at most maxReqHeaders of them in
// at most maxReqHeaderBytes.
func requestHeaders(v value.Object) ([][2]string, *value.Error) {
	h, ok := v.(*value.Hash)
	if !ok {
		return nil, newError("http: headers must be HASH, got %s", v.Type())
	}
	if len(h.Pairs) > maxReqHeaders {
		return nil, newError("http: %d headers (max %d)", len(h.Pairs), maxReqHeaders)
	}
	var out [][2]string
	seen := map[string]bool{}
	var size int64
	for _, hk := range h.Keys() {
		p := h.Pairs[hk]
		k, ok := p.Key.(*value.String)
		if !ok {
			return nil, newError("http: header names must be STRING, got %s", p.Key.Type())
		}
		name := k.Value
		if !validHeaderName(name) {
			return nil, newError("http: invalid header name %s", quoteClipped(name))
		}
		canon := http.CanonicalHeaderKey(name)
		if forbiddenHeaders[canon] {
			return nil, newError("http: header %s cannot be set", quoteClipped(name))
		}
		if seen[canon] {
			return nil, newError("http: header %s given twice", quoteClipped(name))
		}
		seen[canon] = true
		s, ok := p.Value.(*value.String)
		if !ok {
			return nil, newError("http: header %s must be STRING, got %s", quoteClipped(name), p.Value.Type())
		}
		if !validHeaderValue(s.Value) {
			return nil, newError("http: header %s: value has CR, LF or another control byte", quoteClipped(name))
		}
		if size += headerLineSize(canon, s.Value); size > maxReqHeaderBytes {
			return nil, newError("http: headers are %d bytes (max %d)", size, maxReqHeaderBytes)
		}
		out = append(out, [2]string{canon, s.Value})
	}
	return out, nil
}

// validHeaderName reports whether name is an HTTP token (RFC 9110 5.6.2).
func validHeaderName(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case strings.IndexByte("!#$%&'*+-.^_`|~", c) >= 0:
		default:
			return false
		}
	}
	return true
}

// validHeaderValue reports whether v has no control byte but tab: no CR
// or LF to end the header line early, no NUL, no DEL.
func validHeaderValue(v string) bool {
	for i := 0; i < len(v); i++ {
		if c := v[i]; c < 0x20 && c != '\t' || c == 0x7f {
			return false
		}
	}
	return true
}
