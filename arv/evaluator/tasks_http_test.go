package evaluator

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/delavalom/arvlang/arv/runtime"
)

// The acceptance row of 10-phase3-budgets.md as designed: 50 tasks each
// make a request that takes 100 ms, under tasks: 5. The server counts the
// requests in flight, so the limit is checked where the effect lands.
func TestTasksBackpressureAtTheServer(t *testing.T) {
	var mu sync.Mutex
	n, peak, total := 0, 0, 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		total++
		if n++; n > peak {
			peak = n
		}
		mu.Unlock()
		time.Sleep(100 * time.Millisecond)
		mu.Lock()
		n--
		mu.Unlock()
		fmt.Fprint(w, "ok")
	}))
	defer srv.Close()

	src := fmt.Sprintf(`let work = fn() { http_get(%q)["status"] };
		scope (tasks: 5) { for i in %s { spawn work() with [net("127.0.0.1")] } }; "done"`, srv.URL, zeros(50))
	netCap := grant(t, runtime.Net, "127.0.0.1")
	start := time.Now()
	done := make(chan string, 1)
	go func() {
		got, _ := runWith(t, src, netCap)
		done <- got.Inspect()
	}()
	select {
	case got := <-done:
		if got != "done" {
			t.Fatalf("got %s", got)
		}
	case <-time.After(time.Minute):
		t.Fatal("still running after a minute")
	}
	mu.Lock()
	defer mu.Unlock()
	if total != 50 || peak > 5 {
		t.Fatalf("%d requests with peak %d in flight; want 50 with at most 5", total, peak)
	}
	if d := time.Since(start); d < time.Second {
		t.Fatalf("50 x 100 ms at 5 at a time took %s, want at least 1 s", d)
	}
}
