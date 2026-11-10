// Package runtime holds the per-task state the interpreter enforces:
// capabilities, cancellation, scopes and (later) budgets and the effect log.
// It must not import the value or evaluator packages.
package runtime

import (
	"errors"
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

// New validates and normalises a capability. Filesystem scopes must be
// absolute and are canonicalised (this reads the filesystem: lstat and
// readlink); net scopes are normalised by netScope. Host grants, requests
// checked at use, and spawn requests (in Attenuate) all go through New, so
// they compare like for like.
func New(kind Kind, scope string) (Capability, error) {
	c, err := Literal(kind, scope)
	if err != nil {
		return Capability{}, err
	}
	if kind == FSRead || kind == FSWrite {
		canon, err := Canonical(c.Scope)
		if err != nil {
			return Capability{}, fmt.Errorf("%s: %w", kind, err)
		}
		c.Scope = canon
	}
	return c, nil
}

// Literal validates a capability without touching the OS: filesystem scopes
// must be absolute but are kept as written (not canonicalised, not even
// cleaned); net scopes are normalised by netScope, which is pure. This is
// what a script's fs_read("...") value holds: building it needs no
// authority, so it must not lstat anything (I1). It is not comparable with
// grants until passed through New, which Attenuate does.
func Literal(kind Kind, scope string) (Capability, error) {
	if !kinds[kind] {
		return Capability{}, fmt.Errorf("unknown capability kind %q", kind)
	}
	if scope == "" {
		return Capability{}, fmt.Errorf("%s: empty scope", kind)
	}
	if err := CheckScopeLen(kind, scope); err != nil {
		return Capability{}, err
	}
	if (kind == FSRead || kind == FSWrite) && !filepath.IsAbs(scope) {
		return Capability{}, fmt.Errorf("%s: path %q must be absolute", kind, scope)
	}
	if kind == Net {
		norm, err := netScope(scope)
		if err != nil {
			return Capability{}, fmt.Errorf("%s: %w", kind, err)
		}
		scope = norm
	}
	return Capability{Kind: kind, Scope: scope}, nil
}

// MaxScopeLen caps a capability's scope, in bytes. A longer path fails at
// the OS anyway (PATH_MAX is 4096 on Linux), and a host name has at most
// 253 (maxHostLen). Checking it before anything else keeps a program from
// making the runtime copy, normalise and quote a string of any size, none
// of which is charged to alloc_mb.
const MaxScopeLen = 4096

// CheckScopeLen fails if scope is longer than MaxScopeLen. Its error does
// not quote the scope.
func CheckScopeLen(kind Kind, scope string) error {
	if len(scope) > MaxScopeLen {
		return fmt.Errorf("%s: scope is %d bytes long (max %d)", kind, len(scope), MaxScopeLen)
	}
	return nil
}

// Covers reports whether holding c is enough to perform request r.
// An empty scope covers nothing (New never makes one; this guards a
// Capability built by hand, whose empty fs prefix would otherwise be "/").
func (c Capability) Covers(r Capability) bool {
	if c.Kind != r.Kind || c.Scope == "" || r.Scope == "" {
		return false
	}
	switch c.Kind {
	case Net:
		return netCovers(c.Scope, r.Scope)
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

// Admit canonicalises request r, which may be literal (see Literal), and
// returns the canonical form if some grant in s covers it. spawn
// (Attenuate) and the fs effects (read_file, write_file) both use it, so
// the same request gets the same answer, and the same message, at either.
//
// Canonicalising reads the filesystem (lstat, readlink) for paths s may not
// cover. A denial names r as given, never its resolved target. A request
// that fails to canonicalise is denied the same way, with the underlying
// error dropped, unless it failed at a component some grant in s covers
// (see CanonicalError): then the task may already look there, and Admit
// returns that *CanonicalError (permission denied, a symlink loop, ENOENT
// for missing/..) instead of a denial that would contradict the grant.
// Its message can name the canonical path of the failing component, which
// lies inside a grant.
//
// What allow versus deny still reveals: a request is allowed exactly when
// the kernel's resolution of it lands inside a grant, so a task can test
// such resolutions through paths outside its grants. It can learn whether
// a symlink outside its grants points into one of them, and whether a name
// outside them is a real directory: out/name/../../grant/x is allowed only
// if out/name is a directory. A symlink there, a file, or a missing name
// (".." after a missing component fails, as in the kernel) are all denied
// alike, so it cannot tell those apart, nor where a symlink points
// otherwise, nor why resolution failed.
func (s CapSet) Admit(r Capability) (Capability, error) {
	c, err := New(r.Kind, r.Scope)
	var ce *CanonicalError
	if errors.As(err, &ce) && ce.Resolved != "" && s.Allows(Capability{Kind: r.Kind, Scope: ce.Resolved}) {
		return Capability{}, ce
	}
	if err != nil || !s.Allows(c) {
		return Capability{}, &DeniedError{Want: r, Held: s.List()}
	}
	return c, nil
}

// Attenuate returns a set holding exactly reqs, or an error naming the first
// request s does not cover. A child can never hold more than its parent.
// Each request goes through Admit, so it may be literal, and the returned
// set holds the canonical forms.
func (s CapSet) Attenuate(reqs []Capability) (CapSet, error) {
	canon := make([]Capability, len(reqs))
	for i, r := range reqs {
		c, err := s.Admit(r)
		if err != nil {
			return CapSet{}, err
		}
		canon[i] = c
	}
	return NewCapSet(canon...), nil
}

// DeniedError is returned when a task asks for authority it does not hold.
type DeniedError struct {
	Task string
	Want Capability
	Held []Capability
	Note string // why, when Want alone does not say (e.g. a dial-time refusal)
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
	if e.Note != "" {
		where += ": " + e.Note
	}
	return fmt.Sprintf("capability denied: %s%s (holds: [%s])", e.Want, where, strings.Join(held, ", "))
}

// Parse reads the CLI form "kind=scope", e.g. "net=api.github.com".
// Relative fs paths are resolved against base (see Resolve). An empty scope
// is an error, so "fs_read=$UNSET" cannot grant base itself.
func Parse(s, base string) (Capability, error) {
	kind, scope, ok := strings.Cut(s, "=")
	if !ok {
		return Capability{}, fmt.Errorf("capability %q: want kind=scope", s)
	}
	k := Kind(kind)
	if (k == FSRead || k == FSWrite) && scope != "" {
		scope = Resolve(base, scope)
	}
	return New(k, scope)
}

// Resolve makes p absolute against base without cleaning it. "." and ".."
// are left for Canonical, which applies them after following symlinks, as
// the kernel does; filepath.Abs or Join would apply them lexically first and
// reach a different file when a symlink precedes the "..". Every relative fs
// path (grants and requests) goes through here, so both follow one rule.
func Resolve(base, p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return base + string(filepath.Separator) + p
}
