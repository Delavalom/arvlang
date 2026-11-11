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
	c, _, err := newPrepaid(kind, scope, nil, nil)
	return c, err
}

// newPrepaid is New, charging what canonicalising costs to charge (unless
// nil) as canonicalFor does with known, and returning, uncharged, the
// excess. A failed charge is returned as it is.
func newPrepaid(kind Kind, scope string, charge func(int64) error, known func(string) bool) (Capability, int64, error) {
	c, err := Literal(kind, scope)
	if err != nil {
		return Capability{}, 0, err
	}
	if kind != FSRead && kind != FSWrite {
		return c, 0, nil
	}
	var spent error
	ch := charge
	if charge != nil {
		ch = func(n int64) error { spent = charge(n); return spent }
	}
	canon, excess, err := canonicalFor(c.Scope, ch, known)
	if spent != nil {
		return Capability{}, 0, spent
	}
	if err != nil {
		return Capability{}, excess, fmt.Errorf("%s: %w", kind, err)
	}
	c.Scope = canon
	return c, excess, nil
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
		// r lies below c: compared in place, since c.Scope+"/" copied
		// the grant's scope (up to MaxScopeLen bytes) on every check.
		n := len(c.Scope)
		return len(r.Scope) > n && r.Scope[n] == filepath.Separator && r.Scope[:n] == c.Scope
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
// otherwise, nor why resolution failed. AdmitCharged keeps it that way
// under a budget: what a denied request is charged depends on its text
// alone. An allowed one is charged what following its symlinks cost, so
// the budget can say how long the targets of the symlinks on a path that
// resolves into a grant are, a path whose shape already says much of that.
func (s CapSet) Admit(r Capability) (Capability, error) {
	return s.AdmitCharged(r, nil)
}

// AdmitCharged is Admit, charging what canonicalising r allocates to
// charge (unless nil; see CanonicalCharged). If a charge fails, it stops
// and returns that error as it is. What it charges for a request it
// denies depends on the request's text alone: the cost of following
// symlinks past what the text pays for is charged only once r is found
// covered, or failing inside a grant (see canonicalFor). So a task
// that fills its budget to an exact level cannot tell, from "budget
// exceeded" against a denial, what lies outside its grants.
func (s CapSet) AdmitCharged(r Capability, charge func(int64) error) (Capability, error) {
	var spent error
	ch := charge
	if charge != nil {
		ch = func(n int64) error { spent = charge(n); return spent }
	}
	known := func(p string) bool { return s.Allows(Capability{Kind: r.Kind, Scope: p}) }
	c, excess, err := newPrepaid(r.Kind, r.Scope, ch, known)
	if spent != nil {
		return Capability{}, spent
	}
	var ce *CanonicalError
	if errors.As(err, &ce) && ce.Resolved != "" && s.Allows(Capability{Kind: r.Kind, Scope: ce.Resolved}) {
		if excess > 0 && ch != nil && ch(excess) != nil {
			return Capability{}, spent
		}
		return Capability{}, ce
	}
	if err != nil || !s.Allows(c) {
		return Capability{}, &DeniedError{Want: r, Held: s.List()}
	}
	if excess > 0 && ch != nil && ch(excess) != nil {
		return Capability{}, spent
	}
	return c, nil
}

// Attenuate returns a set holding exactly reqs, or an error naming the first
// request s does not cover. A child can never hold more than its parent.
// Each request goes through Admit, so it may be literal, and the returned
// set holds the canonical forms.
func (s CapSet) Attenuate(reqs []Capability) (CapSet, error) {
	return s.AttenuateEach(len(reqs), func(i int) Capability { return reqs[i] }, nil, nil)
}

// AttenuateEach is Attenuate over the n requests req(0), ..., req(n-1),
// charging what canonicalising each allocates to charge (unless nil; see
// AdmitCharged, whose error a failed charge returns as it is), and calling
// each (unless nil) with every request's canonical form as soon as
// it has one, before the next request is canonicalised; an error from each
// stops it and is returned. spawn uses it to charge what canonicalising
// builds and to stop once its task is cancelled: a request is up to
// MaxScopeLen bytes, and a program can pass any number of them. It takes
// the requests one at a time, so the caller need not copy them into a
// slice, uncharged, before the first charge. The set keeps one copy of
// each distinct grant, so repeating a request does not grow it, and a
// request repeated as given is canonicalised only once.
func (s CapSet) AttenuateEach(n int, req func(int) Capability, charge func(int64) error, each func(Capability) error) (CapSet, error) {
	var canon []Capability
	seen := make(map[Capability]bool)
	admitted := make(map[Capability]Capability) // request as given -> canonical
	for i := 0; i < n; i++ {
		r := req(i)
		c, ok := admitted[r]
		if !ok {
			var err error
			if c, err = s.AdmitCharged(r, charge); err != nil {
				return CapSet{}, err
			}
			admitted[r] = c
		}
		if each != nil {
			if err := each(c); err != nil {
				return CapSet{}, err
			}
		}
		if !seen[c] {
			seen[c] = true
			canon = append(canon, c)
		}
	}
	return CapSet{caps: canon}, nil
}

// DeniedError is returned when a task asks for authority it does not hold.
type DeniedError struct {
	Task string
	Want Capability
	Held []Capability
	Note string // why, when Want alone does not say (e.g. a dial-time refusal)
}

// maxHeldShown caps how many grants a denial lists. Error values are not
// charged to alloc_mb, and a task can hold many grants of up to
// MaxScopeLen bytes each.
const maxHeldShown = 8

func (e *DeniedError) Error() string {
	shown := e.Held
	if len(shown) > maxHeldShown {
		shown = shown[:maxHeldShown]
	}
	held := make([]string, len(shown), len(shown)+1)
	for i, c := range shown {
		held[i] = c.String()
	}
	if n := len(e.Held) - len(shown); n > 0 {
		held = append(held, fmt.Sprintf("... %d more", n))
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
