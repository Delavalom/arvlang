// Command uptime-go is the Go baseline of the Phase 5 gate, written as
// for production: check every URL in targets.txt, at most 20 at a time,
// each within 2 s and with up to 2 retries on transport errors; write
// out/report.json; exit 1 if more than 5% of the targets are down.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"os"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"
)

const (
	maxInFlight  = 20
	checkTimeout = 2 * time.Second
	retries      = 2
	backoff      = 100 * time.Millisecond
)

type result struct {
	URL       string `json:"url"`
	Status    int    `json:"status"` // 0 when no response came
	Up        bool   `json:"up"`
	LatencyMS int64  `json:"latency_ms"` // of the last attempt
}

func main() {
	if err := run(context.Background(), "targets.txt", "out/report.json", os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, targetsPath, reportPath string, stdout io.Writer) error {
	data, err := os.ReadFile(targetsPath)
	if err != nil {
		return err
	}
	urls := strings.Fields(string(data))
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConnsPerHost = maxInFlight
	client := &http.Client{Transport: transport}

	results := make([]result, len(urls))
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(maxInFlight) // the semaphore: g.Go blocks while 20 checks run
	for i, url := range urls {
		g.Go(func() error {
			results[i] = check(ctx, client, url)
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return err
	}

	report, err := json.Marshal(results)
	if err != nil {
		return err
	}
	if err := os.WriteFile(reportPath, report, 0o644); err != nil {
		return err
	}
	down := 0
	for _, r := range results {
		if !r.Up {
			down++
		}
	}
	summary, err := json.Marshal(map[string]int{"down": down, "total": len(results)})
	if err != nil {
		return err
	}
	fmt.Fprintln(stdout, string(summary))
	if down*20 > len(results) {
		return errors.New("more than 5% of the targets are down")
	}
	return nil
}

// check fetches url, retrying transport errors (not HTTP error statuses)
// up to retries times with jittered exponential backoff, all within
// checkTimeout.
func check(ctx context.Context, client *http.Client, url string) result {
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	r := result{URL: url}
	for attempt := 0; ; attempt++ {
		start := time.Now()
		status, err := get(ctx, client, url)
		r.LatencyMS = time.Since(start).Milliseconds()
		if err == nil {
			r.Status, r.Up = status, status >= 200 && status < 300
			return r
		}
		if attempt == retries {
			return r
		}
		wait := backoff << attempt
		wait += rand.N(wait/10 + 1)
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return r
		}
	}
}

func get(ctx context.Context, client *http.Client, url string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		return 0, err
	}
	return resp.StatusCode, nil
}
