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
// Every failure is a *CanonicalError recording how far resolution got.
//
// Limit: this is check-then-use. Between Canonical and the open, another
// process that can write inside the grant could swap a directory component
// for a symlink and redirect the open. Callers open the canonical path with
// O_NOFOLLOW, which closes this for the final component only. Closing it for
// directories needs openat-style resolution (Go 1.24's os.Root).
func Canonical(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", &CanonicalError{Path: path, Err: errors.New("path is not absolute")}
	}
	const sep = string(filepath.Separator)
	vol := filepath.VolumeName(path)
	resolved := vol + sep // always a resolved, existing-or-missing absolute path
	rest := path[len(vol):]
	hops := 0
	var missing error    // why the first missing (or non-directory) component cannot be descended
	var missingAt string // that component, resolved
	for rest != "" {
		var comp string
		comp, rest, _ = strings.Cut(rest, sep)
		switch comp {
		case "", ".":
			continue
		case "..":
			if missing != nil {
				return "", &CanonicalError{Path: path, Resolved: missingAt, Err: missing}
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
				missing, missingAt = err, next
				resolved = next
				continue
			}
			return "", &CanonicalError{Path: path, Resolved: next, Err: err}
		}
		if fi.Mode()&fs.ModeSymlink == 0 {
			if !fi.IsDir() {
				missing, missingAt = &fs.PathError{Op: "lstat", Path: next + sep, Err: syscall.ENOTDIR}, next
			}
			resolved = next
			continue
		}
		if hops++; hops > maxSymlinkHops {
			return "", &CanonicalError{Path: path, Resolved: next, Err: syscall.ELOOP}
		}
		target, err := os.Readlink(next)
		if err != nil {
			return "", &CanonicalError{Path: path, Resolved: next, Err: err}
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

// CanonicalError is a failure of Canonical. Resolved is the canonical path
// of the component it failed at: the one it could not lstat or readlink,
// the symlink that exceeded maxSymlinkHops, or the missing (or
// non-directory) component a ".." tried to climb out of. Admit uses it to
// tell a failure inside a grant from one outside. It is empty when no
// component was reached (a relative path).
type CanonicalError struct {
	Path     string // the path as given
	Resolved string // the canonical path of the failing component, or ""
	Err      error  // the OS error
}

func (e *CanonicalError) Error() string { return fmt.Sprintf("canonical %q: %v", e.Path, e.Err) }
func (e *CanonicalError) Unwrap() error { return e.Err }
