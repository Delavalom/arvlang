package main

import (
	"bytes"
	"strings"
	"testing"
)

// The acceptance test of 15-phase8-tooling.md §8.2 (recording and
// replaying it waits for Phase 6; see 16-phase82.md): k8s_apply is denied
// without the grant and allowed with it, in the root and in a child the
// root hands k8s("staging"); prod stays out of reach.
func TestHostExample(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run(nil, &out, &errOut); code != 1 {
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
	if code := run([]string{"staging"}, &out, &errOut); code != 0 {
		t.Fatalf("staging: exit %d; stderr %q", code, errOut.String())
	}
	want := "web.yaml\napi.yaml\nprod: capability\ncluster: staging=[web.yaml api.yaml]\n"
	if out.String() != want {
		t.Fatalf("staging: stdout %q, want %q", out.String(), want)
	}
}
