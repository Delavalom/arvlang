package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

// The acceptance test of 15-phase8-tooling.md §8.2 (its recording and
// replay are TestHostExampleRecordsAndReplays): k8s_apply is denied
// without the grant and allowed with it, in the root and in a child the
// root hands k8s("staging"); prod stays out of reach.
func TestHostExample(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run(nil, logs{}, &out, &errOut); code != 1 {
		t.Fatalf("no grant: exit %d, want 1; stdout %q", code, out.String())
	}
	if want := `error: capability denied: k8s("staging") in task root (holds: [])`; !strings.Contains(errOut.String(), want) {
		t.Fatalf("no grant: stderr %q, want %q", errOut.String(), want)
	}
	if out.Len() != 0 {
		t.Fatalf("no grant: stdout %q, want nothing", out.String())
	}
	out.Reset()
	errOut.Reset()
	if code := run([]string{"staging"}, logs{}, &out, &errOut); code != 0 {
		t.Fatalf("staging: exit %d; stderr %q", code, errOut.String())
	}
	want := "web.yaml\napi.yaml\nprod: capability\ncluster: staging=[web.yaml api.yaml]\n"
	if out.String() != want {
		t.Fatalf("staging: stdout %q, want %q", out.String(), want)
	}
}

// The rest of §8.2's acceptance test: the run is recorded, and its replay
// prints the same lines without calling k8s_apply (the cluster stays
// empty), with no -allow, its grants taken from the log.
func TestHostExampleRecordsAndReplays(t *testing.T) {
	log := filepath.Join(t.TempDir(), "run.jsonl")
	var out, errOut bytes.Buffer
	if code := run([]string{"staging"}, logs{record: log}, &out, &errOut); code != 0 {
		t.Fatalf("record: exit %d; stderr %q", code, errOut.String())
	}
	lines := "web.yaml\napi.yaml\nprod: capability\n"
	if out.String() != lines+"cluster: staging=[web.yaml api.yaml]\n" {
		t.Fatalf("record: stdout %q", out.String())
	}
	out.Reset()
	if code := run(nil, logs{replay: log}, &out, &errOut); code != 0 || out.String() != lines+"cluster: \n" {
		t.Fatalf("replay: exit %d, stdout %q, stderr %q", code, out.String(), errOut.String())
	}
	if code := run([]string{"staging"}, logs{replay: log}, &out, &errOut); code != 1 || !strings.Contains(errOut.String(), "drop -allow") {
		t.Fatalf("replay with -allow: exit %d, stderr %q", code, errOut.String())
	}
}
