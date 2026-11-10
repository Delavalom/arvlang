// Package runtime holds the per-task state the interpreter enforces:
// capabilities, cancellation, scopes and (later) budgets and the effect log.
// It must not import the value or evaluator packages.
package runtime

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
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
		canon, err := Canonical(scope)
		if err != nil {
			return Capability{}, fmt.Errorf("%s: %w", kind, err)
		}
		scope = canon
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
// It fails on a relative path, on more than maxSymlinkHops symlinks (loops)
// and on lstat/readlink errors other than "does not exist".
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
	for rest != "" {
		var comp string
		comp, rest, _ = strings.Cut(rest, sep)
		switch comp {
		case "", ".":
			continue
		case "..":
			resolved = filepath.Dir(resolved)
			continue
		}
		next := filepath.Join(resolved, comp)
		fi, err := os.Lstat(next)
		if err != nil {
			// Missing (or under a non-directory): nothing to resolve here, but
			// keep checking later components, since a ".." may climb back to
			// an existing directory that holds symlinks.
			if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
				resolved = next
				continue
			}
			return "", fmt.Errorf("canonical %q: %w", path, err)
		}
		if fi.Mode()&fs.ModeSymlink == 0 {
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
