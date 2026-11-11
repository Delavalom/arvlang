package farm

import (
	"io"
	"net/http"
	"strconv"
	"testing"
)

// fetch fetches path with c and reads the whole body.
func fetch(c *http.Client, url string) (int, error) {
	resp, err := c.Get(url)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	_, err = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode, err
}

// A dropped request fails exactly once, even on a reused keep-alive
// connection. net/http's client re-sends a GET by itself when a reused
// connection closes before any response, which would make one Go attempt
// at a dropped target cost two latencies (and two requests) while Arv,
// which opens a new connection per http_get, pays one. The farm must not
// let that hidden retry skew the gate.
func TestDropFailsOnceOnAReusedConnection(t *testing.T) {
	f, srv := start(t)
	path := func(b Behaviour) string { return srv.URL + "/t/" + strconv.Itoa(find(t, f, b)) }
	for _, b := range []Behaviour{Drop, Flaky} {
		f.Reset()
		c := &http.Client{Transport: &http.Transport{MaxIdleConnsPerHost: 1}}
		// An answered request leaves its connection idle for the next.
		if code, err := fetch(c, path(OK)); err != nil || code != 200 {
			t.Fatalf("OK target: %d %v", code, err)
		}
		if code, err := fetch(c, path(b)); err == nil {
			t.Errorf("behaviour %d: answered %d on a reused connection, want an error", b, code)
		}
		if s := f.Stats(); s.Total != 2 {
			t.Errorf("behaviour %d: the farm saw %d requests, want 2: the client re-sent the dropped one", b, s.Total)
		}
		c.CloseIdleConnections()
	}
}
