package main

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/bench/farm"
)

func TestServeWritesFilesAndServes(t *testing.T) {
	dir := t.TempDir()
	targets, expect := filepath.Join(dir, "targets.txt"), filepath.Join(dir, "expect.json")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pr, pw := io.Pipe()
	code := make(chan int, 1)
	go func() {
		code <- run(ctx, []string{"-n", "10", "-latency", "1ms", "-targets", targets, "-expect", expect}, pw, io.Discard)
		pw.Close()
	}()
	base, err := bufio.NewReader(pr).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	base = strings.TrimSpace(base)
	if !strings.HasPrefix(base, "http://127.0.0.1:") {
		t.Fatalf("base URL %q", base)
	}
	resp, err := http.Get(base + "/stats")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	list, _ := os.ReadFile(targets)
	if lines := strings.Fields(string(list)); len(lines) != 10 || lines[3] != base+"/t/3" {
		t.Errorf("targets.txt: %q", list)
	}
	data, _ := os.ReadFile(expect)
	if rs, err := farm.ParseReport(data); err != nil || len(rs) != 10 {
		t.Errorf("expect.json: %d entries, %v", len(rs), err)
	}
	cancel()
	if c := <-code; c != 0 {
		t.Errorf("exit %d after cancel, want 0", c)
	}
}

// The farm is local only.
func TestRefusesBadFlags(t *testing.T) {
	for _, args := range [][]string{
		{"-addr", "0.0.0.0:0"},
		{"-addr", "localhost:0"},
		{"-addr", "192.168.1.1:0"},
		{"-latency", "fast"},
		{"-n", "0"},
		{"-fail", "2"},
	} {
		var stderr bytes.Buffer
		if c := run(context.Background(), args, io.Discard, &stderr); c != 2 {
			t.Errorf("%v: exit %d, want 2 (%s)", args, c, stderr.String())
		}
	}
}

func TestCompare(t *testing.T) {
	dir := t.TempDir()
	write := func(name, text string) string {
		p := filepath.Join(dir, name)
		os.WriteFile(p, []byte(text), 0o644)
		return p
	}
	a := write("a.json", `[{"url":"u","status":200,"up":true,"latency_ms":5}]`)
	b := write("b.json", `[{"latency_ms":80,"status":200,"up":true,"url":"u"}]`)
	c := write("c.json", `[{"url":"u","status":0,"up":false,"latency_ms":5}]`)
	for _, tc := range []struct {
		args []string
		code int
		out  string
	}{
		{[]string{"compare", a, b}, 0, "same: 1 entries\n"},
		{[]string{"compare", a, c}, 1, "reports differ at entry 0"},
		{[]string{"compare", a, filepath.Join(dir, "none.json")}, 2, ""},
		{[]string{"compare", a}, 2, ""},
	} {
		var out bytes.Buffer
		if got := run(context.Background(), tc.args, &out, io.Discard); got != tc.code || !strings.Contains(out.String(), tc.out) {
			t.Errorf("%v: exit %d, %q; want %d, %q", tc.args[1:], got, out.String(), tc.code, tc.out)
		}
	}
}
