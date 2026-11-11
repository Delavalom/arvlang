package evaluator

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// maxURLLen caps http_get's URL, in bytes, as most servers do (nginx
// and Apache refuse a request line over about 8 KiB). Parsing a URL,
// building a request and an error naming it each copy it, uncharged.
const maxURLLen = 8 << 10

// clipLen is how much of an argument an error message quotes.
const clipLen = 256

// quoteClipped quotes s for an error message. Past clipLen bytes it
// quotes only the start (cut at a rune boundary) and gives the length, so
// an error, which is never charged to alloc_mb, stays small.
func quoteClipped(s string) string {
	if len(s) <= clipLen {
		return strconv.Quote(s)
	}
	return fmt.Sprintf("%q... (%d bytes)", s[:clipCut(s)], len(s))
}

// clipped is s, or past clipLen bytes its start and its length, for error
// text that is not ours to quote (net/http's): it may quote a header the
// server sent, whole. The result is a copy, so it keeps no long text alive.
func clipped(s string) string {
	if len(s) <= clipLen {
		return s
	}
	return fmt.Sprintf("%s... (%d bytes)", s[:clipCut(s)], len(s))
}

// clipCut is where to cut s, at most clipLen bytes in, at a rune boundary.
func clipCut(s string) int {
	i := clipLen
	for i > 0 && !utf8.RuneStart(s[i]) {
		i--
	}
	return i
}

// maxRedirects matches net/http's default limit.
const maxRedirects = 10

// clientFor returns an HTTP client that re-checks the task's net capability
// (host and port, see netRequest) on every redirect, so a granted host
// cannot bounce the task to another host or port, and checks every address
// it connects to (transportFor). The caller must CloseIdleConnections.
// clientTimeout bounds one http_get, headers and body; a variable only so
// a test can stall a body without waiting 30 s.
var clientTimeout = 30 * time.Second

func clientFor(t *runtime.Task) *http.Client {
	return &http.Client{
		Transport: locationCap{transportFor(t)},
		Timeout:   clientTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return errors.New("too many redirects")
			}
			// The Location header comes from the server: hold it to the
			// cap on the program's own URL before following it.
			if n := len(req.URL.String()); n > maxURLLen {
				return fmt.Errorf("redirect URL is %d bytes long (max %d)", n, maxURLLen)
			}
			want, err := netRequest(req.URL)
			if err != nil {
				return err
			}
			return t.Require(want)
		},
	}
}

// locationCap refuses a redirect whose Location header is over maxURLLen,
// before client.Do parses it. CheckRedirect runs only after the parse, and
// a failed parse made net/http quote the whole Location twice in its
// error (up to 8 bytes per byte of it), uncharged. Only the statuses
// net/http follows are checked: client.Do returns any other 3xx (300, 304,
// ...) as it is, without parsing its Location.
type locationCap struct{ *http.Transport }

func (l locationCap) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := l.Transport.RoundTrip(req)
	if err != nil || !followed(resp.StatusCode) {
		return resp, err
	}
	if n := len(resp.Header.Get("Location")); n > maxURLLen {
		resp.Body.Close()
		return nil, fmt.Errorf("redirect Location is %d bytes long (max %d)", n, maxURLLen)
	}
	return resp, nil
}

// followed reports whether net/http follows a response with this status
// (and so parses its Location).
func followed(code int) bool {
	switch code {
	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther,
		http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
		return true
	}
	return false
}

// httpGet charges the body as it reads it, then its record's two pairs
// (allocSize), as a ChargingFunction.
func httpGet(t *runtime.Task, charge func(int64) error, args ...value.Object) value.Object {
	raw, errObj := oneString("http_get", args)
	if errObj != nil {
		return errObj
	}
	if len(raw) > maxURLLen { // before url.Parse and the errors copy it
		return newError("http_get: URL is %d bytes long (max %d)", len(raw), maxURLLen)
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return newError("http_get: invalid url %s", quoteClipped(raw))
	}
	if u.Hostname() == "" {
		return newError("http_get: URL has no host")
	}
	want, err := netRequest(u)
	if err != nil {
		return newError("http_get: %s", err)
	}
	if err := t.Require(want); err != nil {
		return errorOf(err, runtime.KindCapability)
	}
	req, err := http.NewRequestWithContext(t.Ctx, http.MethodGet, raw, nil)
	if err != nil {
		return newError("http_get: %s", err)
	}
	client := clientFor(t)
	defer client.CloseIdleConnections()
	resp, err := client.Do(req)
	if err != nil {
		var denied *runtime.DeniedError
		if errors.As(err, &denied) {
			return errorOf(denied, runtime.KindCapability)
		}
		// The world failed (refused, reset, too many redirects), unless
		// the task was cancelled first.
		kind := runtime.KindEffect
		if t.Err() != nil {
			kind = runtime.KindCancelled
		}
		var uerr *url.Error
		if errors.As(err, &uerr) { // it quotes a URL, maybe a redirect's: clip it
			return newErrorKind(kind, "http_get: %s %s: %s", uerr.Op, quoteClipped(uerr.URL), clipped(uerr.Err.Error()))
		}
		return newErrorKind(kind, "http_get: %s", clipped(err.Error()))
	}
	defer resp.Body.Close()
	body, err := readCapped(resp.Body, resp.ContentLength, charge)
	if errors.Is(err, errTooBig) {
		return newErrorKind(runtime.KindEffect, "http_get: %s", err)
	}
	if err != nil {
		return readError(t, "http_get: reading body", err)
	}
	if err := charge(2 * hashPairSize); err != nil {
		return errorOf(err, runtime.KindBudget)
	}
	return value.NewHash(map[string]value.Object{
		"status": &value.Integer{Value: int64(resp.StatusCode)},
		"body":   &value.String{Value: body},
	})
}
