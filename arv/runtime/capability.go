// Package runtime holds the per-task state the interpreter enforces:
// capabilities, cancellation, scopes and (later) budgets and the effect log.
// It must not import the value or evaluator packages.
package runtime

import (
	"errors"
	"fmt"
	"io/fs"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unicode"
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
// readlink); net scopes are normalised by netHost. Host grants, requests
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
// cleaned); net scopes are normalised by netHost, which is pure. This is
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
	if (kind == FSRead || kind == FSWrite) && !filepath.IsAbs(scope) {
		return Capability{}, fmt.Errorf("%s: path %q must be absolute", kind, scope)
	}
	if kind == Net {
		host, err := netHost(scope)
		if err != nil {
			return Capability{}, fmt.Errorf("%s: %w", kind, err)
		}
		scope = host
	}
	return Capability{Kind: kind, Scope: scope}, nil
}

// netHost normalises a net scope to the form url.Hostname() gives for the
// same host, so a grant can match a request: lower case, one trailing dot
// dropped, IP literals in netip's canonical form and IPv6 without brackets.
// It rejects anything that could never match a hostname (a port, a scheme,
// a path, userinfo, whitespace) and wildcards other than "*" alone or a
// leading "*." on a DNS name.
func netHost(s string) (string, error) {
	if s == "*" {
		return s, nil
	}
	if strings.ContainsAny(s, "/@?#") || strings.IndexFunc(s, isSpaceOrControl) >= 0 {
		return "", fmt.Errorf("host %q: want a bare host name (no scheme, path, userinfo or spaces)", s)
	}
	h := strings.ToLower(s)
	if strings.HasPrefix(h, "[") {
		inner, ok := strings.CutSuffix(h[1:], "]")
		if a, err := netip.ParseAddr(inner); ok && err == nil && a.Is6() {
			return a.String(), nil
		}
		return "", fmt.Errorf("host %q: brackets must hold exactly an IPv6 address (no port)", s)
	}
	if strings.Contains(h, ":") {
		if a, err := netip.ParseAddr(h); err == nil && a.Is6() {
			return a.String(), nil
		}
		return "", fmt.Errorf("host %q: a port is not allowed", s)
	}
	h = strings.TrimSuffix(h, ".")
	if h == "" {
		return "", fmt.Errorf("host %q: empty host", s)
	}
	if a, err := netip.ParseAddr(h); err == nil {
		return a.String(), nil
	}
	name, wild := strings.CutPrefix(h, "*.")
	if name == "" || strings.Contains(name, "*") {
		return "", fmt.Errorf("host %q: a wildcard must be \"*\" or a leading \"*.\" on a domain", s)
	}
	if wild && isNumeric(name[strings.LastIndex(name, ".")+1:]) {
		// No TLD is all digits, so this is (part of) an IP literal.
		return "", fmt.Errorf("host %q: wildcards cannot cover IP addresses", s)
	}
	return h, nil
}

func isSpaceOrControl(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }

func isNumeric(s string) bool {
	return s != "" && strings.Trim(s, "0123456789") == ""
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
		if c.Scope == "*" || c.Scope == r.Scope {
			return true
		}
		if strings.HasPrefix(c.Scope, "*.") {
			if _, err := netip.ParseAddr(r.Scope); err == nil {
				return false // DNS wildcards never cover IP literals
			}
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

// Admit canonicalises request r, which may be literal (see Literal), and
// returns the canonical form if some grant in s covers it. spawn
// (Attenuate) and the fs effects (read_file, write_file) both use it, so
// the same request gets the same answer, and the same message, at either.
//
// Canonicalising reads the filesystem (lstat, readlink) for paths s may not
// cover. A denial names r as given, never its resolved target, and a
// request that fails to canonicalise is denied the same way, with the
// underlying error dropped.
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

// maxSymlinkHops caps how many symlinks Canonical follows for one path,
// matching Linux's MAXSYMLINKS. Hitting it means a loop (or an absurd chain).
const maxSymlinkHops = 40

// Canonical returns the path the OS would actually reach for an absolute
// path, with every symlink followed, so a grant and a request for the same
// file compare equal (on macOS /var is a symlink to /private/var) and a
// symlink cannot smuggle a request outside its grant.
//
// The path need not exist. Canonical walks it one component at a time from
// the root: each existing component that is a symlink is replaced by its
// target (relative targets resolve against the link's directory), including
// a dangling one; ".." steps back from the resolved prefix, as the kernel
// does; components that don't exist are appended as they are. So for a
// dangling link <grant>/x -> /elsewhere/new, the result is /elsewhere/new,
// which is where a create would land.
//
// Like the kernel, ".." after a component that does not exist, or that is
// not a directory, fails (ENOENT, ENOTDIR) instead of climbing back. Once a
// component is missing nothing below it is looked up.
//
// It fails on a relative path, on such a "..", on more than maxSymlinkHops
// symlinks (loops) and on lstat/readlink errors other than "does not exist".
//
// Limit: this is check-then-use. Between Canonical and the open, another
// process that can write inside the grant could swap a directory component
// for a symlink and redirect the open. Callers open the canonical path with
// O_NOFOLLOW, which closes this for the final component only. Closing it for
// directories needs openat-style resolution (Go 1.24's os.Root).
func Canonical(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("canonical: path %q is not absolute", path)
	}
	const sep = string(filepath.Separator)
	vol := filepath.VolumeName(path)
	resolved := vol + sep // always a resolved, existing-or-missing absolute path
	rest := path[len(vol):]
	hops := 0
	var missing error // why the first missing (or non-directory) component cannot be descended
	for rest != "" {
		var comp string
		comp, rest, _ = strings.Cut(rest, sep)
		switch comp {
		case "", ".":
			continue
		case "..":
			if missing != nil {
				return "", fmt.Errorf("canonical %q: %w", path, missing)
			}
			resolved = filepath.Dir(resolved)
			continue
		}
		next := filepath.Join(resolved, comp)
		if missing != nil {
			resolved = next // nothing exists below a missing component
			continue
		}
		fi, err := os.Lstat(next)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
				missing = err
				resolved = next
				continue
			}
			return "", fmt.Errorf("canonical %q: %w", path, err)
		}
		if fi.Mode()&fs.ModeSymlink == 0 {
			if !fi.IsDir() {
				missing = &fs.PathError{Op: "lstat", Path: next + sep, Err: syscall.ENOTDIR}
			}
			resolved = next
			continue
		}
		if hops++; hops > maxSymlinkHops {
			return "", fmt.Errorf("canonical %q: too many levels of symbolic links", path)
		}
		target, err := os.Readlink(next)
		if err != nil {
			return "", fmt.Errorf("canonical %q: %w", path, err)
		}
		if filepath.IsAbs(target) {
			v := filepath.VolumeName(target)
			resolved, target = v+sep, target[len(v):]
		}
		// Re-walk the target in place of the link, then the remaining components.
		rest = target + sep + rest
	}
	return resolved, nil
}
