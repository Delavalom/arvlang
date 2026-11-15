package runtime

import (
	"strings"
	"testing"
)

// A host declares its own kinds; the built-in ones, a malformed name or a
// repeated one are refused, so a host kind can never change what net,
// fs_read, fs_write or env mean.
func TestNewKindsValidates(t *testing.T) {
	for _, tt := range []struct {
		name  string
		specs []KindSpec
		want  string // "" for success
	}{
		{"none", nil, ""},
		{"one", []KindSpec{{Kind: "k8s"}}, ""},
		{"two", []KindSpec{{Kind: "k8s"}, {Kind: "queue_v2"}}, ""},
		{"empty", []KindSpec{{Kind: ""}}, "empty name"},
		{"built in net", []KindSpec{{Kind: Net}}, `"net" is built in`},
		{"built in fs_read", []KindSpec{{Kind: FSRead}}, `"fs_read" is built in`},
		{"built in env", []KindSpec{{Kind: Env}}, `"env" is built in`},
		{"twice", []KindSpec{{Kind: "k8s"}, {Kind: "k8s"}}, `"k8s" declared twice`},
		{"upper case", []KindSpec{{Kind: "K8s"}}, "lower-case"},
		{"leading digit", []KindSpec{{Kind: "8ks"}}, "lower-case"},
		{"dash", []KindSpec{{Kind: "k-8"}}, "lower-case"},
		{"space", []KindSpec{{Kind: "k 8"}}, "lower-case"},
		{"too long", []KindSpec{{Kind: Kind(strings.Repeat("k", MaxKindLen+1))}}, "longer than"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			k, err := NewKinds(tt.specs...)
			if tt.want == "" {
				if err != nil || k == nil {
					t.Fatalf("NewKinds = %v, %v; want a registry", k, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("NewKinds err = %v, want one mentioning %q", err, tt.want)
			}
		})
	}
}

func TestKindsHas(t *testing.T) {
	k, err := NewKinds(KindSpec{Kind: "k8s"})
	if err != nil {
		t.Fatal(err)
	}
	if !k.Has("k8s") || k.Has("queue") || k.Has(Net) {
		t.Fatal("Has answers for the wrong kinds")
	}
	var none *Kinds
	if none.Has("k8s") {
		t.Fatal("a nil registry declares nothing")
	}
	if !IsBuiltin(Net) || !IsBuiltin(Env) || IsBuiltin("k8s") {
		t.Fatal("IsBuiltin is wrong")
	}
	if got := k.List(); len(got) != 1 || got[0] != "k8s" {
		t.Fatalf("List = %v", got)
	}
}

// A host kind's scope is kept as written: non-empty and at most
// MaxScopeLen bytes. Other kinds go through Literal and New unchanged.
func TestKindsLiteralAndNew(t *testing.T) {
	k, err := NewKinds(KindSpec{Kind: "k8s"})
	if err != nil {
		t.Fatal(err)
	}
	c, err := k.Literal("k8s", "Staging/EU ")
	if err != nil || c != (Capability{Kind: "k8s", Scope: "Staging/EU "}) {
		t.Fatalf("Literal = %v, %v", c, err)
	}
	if _, err := k.Literal("k8s", ""); err == nil || !strings.Contains(err.Error(), "empty scope") {
		t.Fatalf("empty scope: err = %v", err)
	}
	long := strings.Repeat("s", MaxScopeLen+1)
	if _, err := k.Literal("k8s", long); err == nil || strings.Contains(err.Error(), long) {
		t.Fatalf("long scope: err = %v, want an error that does not quote it", err)
	}
	if _, err := k.New("k8s", "prod"); err != nil {
		t.Fatalf("New: %v", err)
	}
	if c, err := k.New(Net, "API.example.com"); err != nil || c.Scope != "api.example.com" {
		t.Fatalf("New(net) = %v, %v; want the built-in normalisation", c, err)
	}
	if _, err := k.Literal("queue", "q"); err == nil || !strings.Contains(err.Error(), "unknown capability kind") {
		t.Fatalf("undeclared kind: err = %v", err)
	}
	var none *Kinds
	if _, err := none.New("k8s", "prod"); err == nil {
		t.Fatal("a nil registry accepted a host kind")
	}
}
