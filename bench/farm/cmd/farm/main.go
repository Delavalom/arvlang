// Command farm serves the Phase 5 gate's target farm on a loopback
// address, or compares two report.json files.
//
//	farm [-addr 127.0.0.1:0] [-n 200] [-latency 50ms-400ms] [-fail 0.1] [-flaky 0.05] [-seed 1] [-targets FILE] [-expect FILE]
//	farm compare A.json B.json
//
// Serving, it prints its base URL (http://127.0.0.1:PORT) on the first
// line of standard output, then serves until interrupted. -targets writes
// targets.txt and -expect the report a correct checker writes.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/delavalom/arvlang/bench/farm"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "compare" {
		return compare(args[1:], stdout, stderr)
	}
	fs := flag.NewFlagSet("farm", flag.ContinueOnError)
	fs.SetOutput(stderr)
	addr := fs.String("addr", "127.0.0.1:0", "loopback address to listen on")
	n := fs.Int("n", 200, "number of targets")
	latency := fs.String("latency", "50ms-400ms", "latency range MIN-MAX (or one duration)")
	fail := fs.Float64("fail", 0.1, "fraction of targets always down")
	flaky := fs.Float64("flaky", 0.05, "fraction of targets that drop their first request")
	seed := fs.Uint64("seed", 1, "seed for latencies and behaviours")
	targets := fs.String("targets", "", "write the target list here")
	expect := fs.String("expect", "", "write the expected report here")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	lo, hi, err := parseLatency(*latency)
	if err != nil {
		fmt.Fprintln(stderr, "farm:", err)
		return 2
	}
	f, err := farm.New(farm.Config{N: *n, MinLatency: lo, MaxLatency: hi, Fail: *fail, Flaky: *flaky, Seed: *seed})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if err := loopback(*addr); err != nil {
		fmt.Fprintln(stderr, "farm:", err)
		return 2
	}
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		fmt.Fprintln(stderr, "farm:", err)
		return 1
	}
	base := "http://" + ln.Addr().String()
	if err := writeFiles(f, base, *targets, *expect); err != nil {
		ln.Close()
		fmt.Fprintln(stderr, "farm:", err)
		return 1
	}
	srv := &http.Server{Handler: f, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		srv.Close()
	}()
	fmt.Fprintln(stdout, base)
	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		fmt.Fprintln(stderr, "farm:", err)
		return 1
	}
	return 0
}

// loopback refuses any address but a loopback IP: the farm is local only.
func loopback(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("-addr %s: want a loopback IP such as 127.0.0.1", addr)
	}
	return nil
}

// parseLatency reads "50ms-400ms", or one duration for a fixed latency.
func parseLatency(s string) (lo, hi time.Duration, err error) {
	a, b, ok := strings.Cut(s, "-")
	if !ok {
		b = a
	}
	if lo, err = time.ParseDuration(a); err == nil {
		hi, err = time.ParseDuration(b)
	}
	if err != nil {
		return 0, 0, fmt.Errorf("-latency %q: want MIN-MAX such as 50ms-400ms", s)
	}
	return lo, hi, nil
}

func writeFiles(f *farm.Farm, base, targets, expect string) error {
	if targets != "" {
		if err := os.WriteFile(targets, []byte(f.TargetList(base)), 0o644); err != nil {
			return err
		}
	}
	if expect != "" {
		data, err := json.Marshal(f.Expected(base))
		if err != nil {
			return err
		}
		return os.WriteFile(expect, data, 0o644)
	}
	return nil
}

// compare exits 0 if two reports are the same apart from latency.
func compare(args []string, stdout, stderr io.Writer) int {
	if len(args) != 2 {
		fmt.Fprintln(stderr, "usage: farm compare A.json B.json")
		return 2
	}
	var rs [2][]farm.Result
	for i, name := range args {
		data, err := os.ReadFile(name)
		if err == nil {
			rs[i], err = farm.ParseReport(data)
		}
		if err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", name, err)
			return 2
		}
	}
	if err := farm.Compare(rs[0], rs[1]); err != nil {
		fmt.Fprintln(stdout, err)
		return 1
	}
	fmt.Fprintf(stdout, "same: %d entries\n", len(rs[0]))
	return 0
}
