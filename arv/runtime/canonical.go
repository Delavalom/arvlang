package runtime

import (
	"bytes"
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
// This is check-then-use: between Canonical and the open, another process
// that can write inside the grant could swap a directory component for a
// symlink. The fs effects therefore open the canonical path beneath the
// covering grant with os.Root (evaluator.openIn), which refuses any symlink
// that leads out of the grant, so the swap cannot redirect the open.
func Canonical(path string) (string, error) {
	return CanonicalCharged(path, nil)
}

// StatCost is about what an lstat or stat allocates beyond copies of its
// path (a FileInfo, an error, rounding): 208 bytes on darwin/arm64.
const StatCost = 256

// CanonicalCharged is Canonical, calling charge (unless nil) with the
// bytes each step is about to allocate, and stopping with charge's error,
// as it is, as soon as one fails. Each existing component costs an lstat
// of the whole resolved prefix (StatCost and a C copy of the prefix, plus
// a Go copy unless the prefix is still a prefix of path, each charged
// with its size-class rounding, heapSize), so a deep path
// costs O(depth x length): about 620 KB for a 996-byte path 455 levels
// deep, and about 1.4 MB for 4 KB of "/d/.." pairs under a 900-byte
// prefix. The fs effects and spawn charge it to alloc_mb (review round 10).
func CanonicalCharged(path string, charge func(int64) error) (string, error) {
	if !filepath.IsAbs(path) {
		return "", &CanonicalError{Path: path, Err: errors.New("path is not absolute")}
	}
	if charge == nil {
		charge = func(int64) error { return nil }
	}
	if err := charge(int64(len(path)) + 1); err != nil { // buf
		return "", err
	}
	const sep = string(filepath.Separator)
	vol := filepath.VolumeName(path)
	// buf is the resolved path so far, always clean and absolute; root is
	// the length of its volume and root separator. Components are appended
	// in place, so the walk is linear in the path's length: it used
	// filepath.Join for each, a new copy of the whole prefix every time.
	buf := make([]byte, 0, len(path)+1)
	buf = append(buf, vol+sep...)
	root := len(buf)
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
			buf = parentOf(buf, root)
			continue
		}
		mark := len(buf)
		if need := mark + 1 + len(comp); need > cap(buf) {
			// Past a symlink the path can outgrow buf: append grows it
			// to under 1.25 times what it needs plus 192 bytes, or to
			// twice a capacity under 256.
			if err := charge(heapSize(need + need/4 + 512)); err != nil {
				return "", err
			}
		}
		if mark > root {
			buf = append(buf, filepath.Separator)
		}
		buf = append(buf, comp...)
		if missing != nil {
			continue // nothing exists below a missing component
		}
		// next is buf as a string: a slice of path while buf still
		// spells a prefix of it (no symlink followed, no "." or "//"),
		// which the comparison checks without copying.
		cost := StatCost + heapSize(len(buf)+1) // the FileInfo and the C copy
		samePrefix := len(buf) <= len(path) && string(buf) == path[:len(buf)]
		if !samePrefix {
			cost += heapSize(len(buf))
		}
		if err := charge(cost); err != nil {
			return "", err
		}
		var next string
		if samePrefix {
			next = path[:len(buf)]
		} else {
			next = string(buf)
		}
		fi, err := os.Lstat(next)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
				missing, missingAt = err, next
				continue
			}
			return "", &CanonicalError{Path: path, Resolved: next, Err: err}
		}
		if fi.Mode()&fs.ModeSymlink == 0 {
			if !fi.IsDir() {
				missing, missingAt = &fs.PathError{Op: "lstat", Path: next + sep, Err: syscall.ENOTDIR}, next
			}
			continue
		}
		if hops++; hops > maxSymlinkHops {
			return "", &CanonicalError{Path: path, Resolved: next, Err: syscall.ELOOP}
		}
		// Readlink copies next into a C string and reads into a buffer
		// of 128 bytes, doubled (with another C copy) until the target
		// fits; the larger buffers, the target and the walk of the target
		// in place of the link, joined to what is left, are charged once
		// the target's length is known. A target is at most PATH_MAX, and
		// there are at most maxSymlinkHops of them.
		if err := charge(StatCost + heapSize(len(next)+1) + readlinkBuf); err != nil {
			return "", err
		}
		target, err := os.Readlink(next)
		if err != nil {
			return "", &CanonicalError{Path: path, Resolved: next, Err: err}
		}
		lcost := heapSize(len(target)) + heapSize(len(target)+1+len(rest))
		for size := 2 * readlinkBuf; size/2 <= int64(len(target)); size *= 2 {
			lcost += size + heapSize(len(next)+1)
		}
		if err := charge(lcost); err != nil {
			return "", err
		}
		buf = buf[:mark] // the link's directory
		if filepath.IsAbs(target) {
			v := filepath.VolumeName(target)
			buf = append(buf[:0], v+sep...)
			root = len(buf)
			target = target[len(v):]
		}
		// Re-walk the target in place of the link, then the remaining components.
		rest = target + sep + rest
	}
	if err := charge(int64(len(buf))); err != nil {
		return "", err
	}
	return string(buf), nil
}

// heapSize bounds what allocating n bytes takes from the heap. Go rounds
// an allocation up to a size class (898 bytes take 1024): past 128 bytes
// a class is at most 19% above the one below it (3457 bytes take 4096),
// below that at most 16 bytes, and past 32 KiB it rounds to 8 KiB pages.
func heapSize(n int) int64 {
	return int64(n) + int64(n)/4 + 16
}

// readlinkBuf is the size of os.Readlink's first buffer.
const readlinkBuf int64 = 128

// parentOf drops buf's last component, as filepath.Dir does for a clean
// absolute path, but in place. root is the length of buf's volume and
// root separator, which it never drops.
func parentOf(buf []byte, root int) []byte {
	i := bytes.LastIndexByte(buf[root:], filepath.Separator)
	if i < 0 {
		return buf[:root]
	}
	return buf[:root+i]
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
