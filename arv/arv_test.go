package arv

import (
	"bytes"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
)

func TestRunReturnsValueAndOutput(t *testing.T) {
	var out bytes.Buffer
	got, err := Run(`puts("start"); let add = fn(a, b) { a + b }; add(2, 3)`, Options{Out: &out})
	if err != nil {
		t.Fatal(err)
	}
	if got.Inspect() != "5" || out.String() != "start\n" {
		t.Fatalf("got %s, out %q", got.Inspect(), out.String())
	}
}

func TestRunSurfacesErrors(t *testing.T) {
	if _, err := Run(`let = 1`, Options{}); err == nil || !strings.HasPrefix(err.Error(), "parse error") {
		t.Fatalf("want parse error, got %v", err)
	}
	if _, err := Run(`getenv("HOME")`, Options{}); err == nil || !strings.Contains(err.Error(), "capability denied") {
		t.Fatalf("want capability denied, got %v", err)
	}
	home, _ := runtime.New(runtime.Env, "HOME")
	if _, err := Run(`getenv("HOME")`, Options{Caps: []runtime.Capability{home}}); err != nil {
		t.Fatalf("granted env: %v", err)
	}
}
