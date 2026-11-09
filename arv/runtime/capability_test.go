package runtime

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func mustCap(t *testing.T, k Kind, scope string) Capability {
	t.Helper()
	c, err := New(k, scope)
	if err != nil {
		t.Fatalf("New(%s, %q): %v", k, scope, err)
	}
	return c
}

func TestCovers(t *testing.T) {
	tests := []struct {
		held, want Capability
		ok         bool
	}{
		{mustCap(t, Net, "api.github.com"), mustCap(t, Net, "api.github.com"), true},
		{mustCap(t, Net, "api.github.com"), mustCap(t, Net, "evil.com"), false},
		{mustCap(t, Net, "*.github.com"), mustCap(t, Net, "api.github.com"), true},
		{mustCap(t, Net, "*.github.com"), mustCap(t, Net, "github.com"), false},
		{mustCap(t, Net, "*"), mustCap(t, Net, "anything.io"), true},
		{mustCap(t, FSRead, "/data"), mustCap(t, FSRead, "/data/a.txt"), true},
		{mustCap(t, FSRead, "/data"), mustCap(t, FSRead, "/database/a.txt"), false},
		{mustCap(t, FSRead, "/data"), mustCap(t, FSRead, "/data/../etc/passwd"), false},
		{mustCap(t, FSRead, "/data"), mustCap(t, FSWrite, "/data/a.txt"), false},
		{mustCap(t, Env, "HOME"), mustCap(t, Env, "HOME"), true},
		{mustCap(t, Env, "HOME"), mustCap(t, Env, "AWS_SECRET_ACCESS_KEY"), false},
	}
	for _, tt := range tests {
		if got := tt.held.Covers(tt.want); got != tt.ok {
			t.Errorf("%s covers %s = %v, want %v", tt.held, tt.want, got, tt.ok)
		}
	}
}

func TestNewRejectsBadInput(t *testing.T) {
	for _, c := range []struct {
		k     Kind
		scope string
	}{{"disk", "/"}, {Net, ""}, {FSRead, "relative/path"}} {
		if _, err := New(c.k, c.scope); err == nil {
			t.Errorf("New(%s, %q) succeeded, want error", c.k, c.scope)
		}
	}
}

func TestAttenuate(t *testing.T) {
	parent := NewCapSet(mustCap(t, Net, "*.github.com"), mustCap(t, FSRead, "/data"))

	child, err := parent.Attenuate([]Capability{mustCap(t, Net, "api.github.com")})
	if err != nil {
		t.Fatalf("attenuate to a covered cap: %v", err)
	}
	if child.Allows(mustCap(t, FSRead, "/data/x")) {
		t.Error("child kept fs_read it did not ask for")
	}

	_, err = parent.Attenuate([]Capability{mustCap(t, Net, "evil.com")})
	var denied *DeniedError
	if !errors.As(err, &denied) {
		t.Fatalf("want *DeniedError, got %v", err)
	}
	if !strings.Contains(err.Error(), `net("evil.com")`) {
		t.Errorf("error should name the request, got %q", err)
	}
}

func TestParse(t *testing.T) {
	base := t.TempDir()
	c, err := Parse("fs_read=data", base)
	if want := Canonical(filepath.Join(base, "data")); err != nil || c.Scope != want {
		t.Fatalf("Parse relative fs path = %v, %v; want scope %s", c, err, want)
	}
	if _, err := Parse("net", "/"); err == nil {
		t.Error("Parse without '=' should fail")
	}
}
