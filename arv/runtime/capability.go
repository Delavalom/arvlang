// Package runtime holds the per-task state the interpreter enforces:
// capabilities, cancellation, scopes and (later) budgets and the effect log.
// It must not import the value or evaluator packages.
package runtime

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Kind names a class of authority.
type Kind string

const (
	Net     Kind = "net"      // outbound network, scoped to a host
	FSRead  Kind = "fs_read"  // read files under a path
	FSWrite Kind = "fs_write" // write files under a path
	Env     Kind = "env"      // read one environment variable
)

var kinds = map[Kind]bool{Net: true, FSRead: true, FSWrite: true, Env: true}

// Capability is one grant, e.g. net("api.github.com") or fs_read("/data").
type Capability struct {
	Kind  Kind
	Scope string
}

func (c Capability) String() string { return fmt.Sprintf("%s(%q)", c.Kind, c.Scope) }

// New validates and normalises a capability. Filesystem scopes must be absolute.
func New(kind Kind, scope string) (Capability, error) {
	if !kinds[kind] {
		return Capability{}, fmt.Errorf("unknown capability kind %q", kind)
	}
	if scope == "" {
		return Capability{}, fmt.Errorf("%s: empty scope", kind)
	}
	if kind == FSRead || kind == FSWrite {
		if !filepath.IsAbs(scope) {
			return Capability{}, fmt.Errorf("%s: path %q must be absolute", kind, scope)
		}
		scope = Canonical(scope)
	}
	if kind == Net {
		scope = strings.ToLower(scope)
	}
	return Capability{Kind: kind, Scope: scope}, nil
}

// Covers reports whether holding c is enough to perform request r.
func (c Capability) Covers(r Capability) bool {
	if c.Kind != r.Kind {
		return false
	}
	switch c.Kind {
	case Net:
		if c.Scope == "*" || c.Scope == r.Scope {
			return true
		}
		if strings.HasPrefix(c.Scope, "*.") {
			return strings.HasSuffix(r.Scope, c.Scope[1:]) // "*.x.com" covers "a.x.com", not "x.com"
		}
		return false
	case FSRead, FSWrite:
		if c.Scope == "/" || c.Scope == r.Scope {
			return true
		}
		return strings.HasPrefix(r.Scope, c.Scope+string(filepath.Separator))
	case Env:
		return c.Scope == "*" || c.Scope == r.Scope
	}
	return false
}

// CapSet is an immutable set of capabilities held by one task.
type CapSet struct{ caps []Capability }

func NewCapSet(caps ...Capability) CapSet {
	return CapSet{caps: append([]Capability(nil), caps...)}
}

// List returns a copy of the grants.
func (s CapSet) List() []Capability { return append([]Capability(nil), s.caps...) }

// Allows reports whether some grant in s covers r.
func (s CapSet) Allows(r Capability) bool {
	for _, c := range s.caps {
		if c.Covers(r) {
			return true
		}
	}
	return false
}

// Attenuate returns a set holding exactly reqs, or an error naming the first
// request s does not cover. A child can never hold more than its parent.
func (s CapSet) Attenuate(reqs []Capability) (CapSet, error) {
	for _, r := range reqs {
		if !s.Allows(r) {
			return CapSet{}, &DeniedError{Want: r, Held: s.List()}
		}
	}
	return NewCapSet(reqs...), nil
}

// DeniedError is returned when a task asks for authority it does not hold.
type DeniedError struct {
	Task string
	Want Capability
	Held []Capability
}

func (e *DeniedError) Error() string {
	held := make([]string, len(e.Held))
	for i, c := range e.Held {
		held[i] = c.String()
	}
	where := ""
	if e.Task != "" {
		where = " in task " + e.Task
	}
	return fmt.Sprintf("capability denied: %s%s (holds: [%s])", e.Want, where, strings.Join(held, ", "))
}

// Parse reads the CLI form "kind=scope", e.g. "net=api.github.com".
// Relative fs paths are resolved against base.
func Parse(s, base string) (Capability, error) {
	kind, scope, ok := strings.Cut(s, "=")
	if !ok {
		return Capability{}, fmt.Errorf("capability %q: want kind=scope", s)
	}
	k := Kind(kind)
	if (k == FSRead || k == FSWrite) && !filepath.IsAbs(scope) {
		scope = filepath.Join(base, scope)
	}
	return New(k, scope)
}

// Canonical cleans an absolute path and resolves symlinks in the longest
// prefix that exists, so a grant and a request for the same file compare
// equal (on macOS /var is a symlink to /private/var) and a symlink cannot
// smuggle a request outside its grant.
func Canonical(path string) string {
	path = filepath.Clean(path)
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	dir, base := filepath.Split(path)
	if dir == "" || dir == path || base == "" {
		return path
	}
	return filepath.Join(Canonical(dir), base)
}
