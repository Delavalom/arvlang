package e2e

import (
	"io"
	"net/http"
	"strconv"
	"sync"
	"testing"
	"time"
)

// /flaky?k=K&fail=N answers 500 to the first N requests for key K (per
// test id) and 200 "up" after that, and records when each arrived, so a
// test can count a restart's attempts and time its backoff on the server.

var (
	flakyMu sync.Mutex
	flakyAt = map[string][]time.Time{} // id+"/"+k -> arrival times
)

func addP4Routes(mux *http.ServeMux) {
	mux.HandleFunc("/flaky", func(w http.ResponseWriter, r *http.Request) {
		record(r)
		q := r.URL.Query()
		fail, err := strconv.Atoi(q.Get("fail"))
		if err != nil {
			http.Error(w, "bad fail", http.StatusBadRequest)
			return
		}
		key := q.Get("id") + "/" + q.Get("k")
		flakyMu.Lock()
		flakyAt[key] = append(flakyAt[key], time.Now())
		n := len(flakyAt[key])
		flakyMu.Unlock()
		if n <= fail {
			http.Error(w, "down", http.StatusInternalServerError)
			return
		}
		io.WriteString(w, "up")
	})
}

// flakyTimes returns when /flaky received this test's requests for key k.
func flakyTimes(t *testing.T, k string) []time.Time {
	flakyMu.Lock()
	defer flakyMu.Unlock()
	return append([]time.Time(nil), flakyAt[testID(t)+"/"+k]...)
}
