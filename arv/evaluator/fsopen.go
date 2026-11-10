package evaluator

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/delavalom/arvlang/arv/runtime"
)

// beforeOpen, when set (only by tests), runs after the capability check and
// before the open: the window in which another process could swap a
// directory inside the grant for a symlink.
var beforeOpen func(path string)

// beforeStep, when set (only by tests), runs once a path component has
// been checked and before it is opened (see step and openLeafIn).
var beforeStep func(path string)

// errChanged reports a component between the outer root and the grant that
// is no longer the directory (or file) the check saw: a symlink, or a
// different directory, swapped in after the check.
var errChanged = errors.New("path changed during the open (a component was replaced)")

// fsTarget is an fs request after the check (fsRequest): the canonical
// path, the scope of the grant that covers it, and outer, the directory
// the open starts from (confiningRoot): grant itself, or a directory above
// it that some other fs grant of the task names.
type fsTarget struct{ outer, grant, path string }

// confiningRoot returns where an open beneath grant must start: the
// outermost ancestor of grant, spelled as in grant, that is the same
// directory as some fs grant in held (of either kind), or else grant.
// held is the task's lineage (Task.LineageCaps): an attenuated child's
// grant nested inside a grant only an ancestor holds is confined as if the
// child held both (review round 3).
//
// A grant nested inside another lies where a writer inside the outer one
// can swap a directory on the way to it for a symlink, and os.OpenRoot
// follows symlinks in the root's own path. CapSet.Grant already picks the
// outermost cover of the request's kind; this also catches a cover of the
// other kind (fs_write=/g with fs_read=/g/sub) and one spelled differently
// (/R/g and /R/G/sub on a case-insensitive filesystem), by comparing
// identities, not strings. Only the outer root's own path is trusted.
func confiningRoot(held []runtime.Capability, grant string) string {
	var dirs []os.FileInfo
	for _, c := range held {
		if (c.Kind != runtime.FSRead && c.Kind != runtime.FSWrite) || c.Scope == grant {
			continue
		}
		if fi, err := os.Stat(c.Scope); err == nil && fi.IsDir() {
			dirs = append(dirs, fi)
		}
	}
	if len(dirs) == 0 {
		return grant
	}
	var above []string // grant's ancestors, innermost first
	for d := filepath.Dir(grant); ; d = filepath.Dir(d) {
		above = append(above, d)
		if filepath.Dir(d) == d {
			break
		}
	}
	for i := len(above) - 1; i >= 0; i-- {
		fi, err := os.Stat(above[i])
		if err != nil {
			continue
		}
		for _, d := range dirs {
			if os.SameFile(fi, d) {
				return above[i]
			}
		}
	}
	return grant
}

// openIn opens tg.path, which is canonical and covered by tg.grant
// (fsRequest returns both), so that the open cannot leave the grant even
// if the tree changes after the check.
//
// When tg.outer is the grant, a directory grant is opened as an os.Root
// and the path is opened beneath it: os.Root follows a symlink only while
// it stays inside the root and refuses one that escapes ("path escapes
// from parent"), so a directory swapped for a symlink to outside the grant
// is refused, while one swapped for a symlink to elsewhere inside it
// reaches a file the task may open anyway. When the path is the grant
// itself (a file grant), openLeaf opens it directly with O_NOFOLLOW.
//
// When tg.outer is above the grant, the open starts at an os.Root on outer
// and steps down to the grant one component at a time, refusing any
// component that is not the directory checked (step). The grant's own
// path was canonical when checked, so a symlink or a different directory
// on it now is a swap.
//
// Errors name the path, as a plain open would.
func openIn(tg fsTarget, flag int, perm uint32) (*os.File, error) {
	if beforeOpen != nil {
		beforeOpen(tg.path)
	}
	if tg.outer == tg.grant && tg.path == tg.grant {
		return openLeaf(tg.path, flag, perm)
	}
	rel, ok := relIn(tg.grant, tg.path)
	down, ok2 := relIn(tg.outer, tg.grant)
	if (!ok && tg.path != tg.grant) || (!ok2 && tg.outer != tg.grant) {
		return nil, &fs.PathError{Op: "open", Path: tg.path, Err: errors.New("path is not inside its grant")}
	}
	root, err := os.OpenRoot(tg.outer)
	if err != nil {
		return nil, renamePathError(err, tg.path)
	}
	defer func() { root.Close() }()
	var comps []string
	if down != "" {
		comps = strings.Split(down, string(filepath.Separator))
	}
	leaf := ""
	if tg.path == tg.grant { // a file grant below outer: step to its directory
		comps, leaf = comps[:len(comps)-1], comps[len(comps)-1]
	}
	at := tg.outer
	for _, c := range comps {
		at = filepath.Join(at, c)
		next, err := step(root, at, c)
		if err != nil {
			return nil, renamePathError(err, tg.path)
		}
		root.Close()
		root = next
	}
	var f *os.File
	if leaf != "" {
		f, err = openLeafIn(root, tg.path, leaf, flag, perm)
	} else {
		f, err = root.OpenFile(rel, flag|rootOpenFlags, fs.FileMode(perm))
	}
	if err != nil {
		return nil, renamePathError(err, tg.path)
	}
	return f, nil
}

// relIn returns path relative to dir, if path lies strictly inside dir.
func relIn(dir, path string) (string, bool) {
	rel, ok := strings.CutPrefix(path, dir)
	if dir != string(filepath.Separator) {
		rel, ok = strings.CutPrefix(rel, string(filepath.Separator))
	}
	return rel, ok && rel != "" && dir != path
}

// step opens the directory name in cur, refusing it unless it is the very
// directory lstat saw: not a symlink, and not swapped between the lstat
// and the open (os.SameFile). at is its full path, for beforeStep.
func step(cur *os.Root, at, name string) (*os.Root, error) {
	fi, err := cur.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !fi.IsDir() {
		return nil, &fs.PathError{Op: "open", Path: at, Err: errChanged}
	}
	if beforeStep != nil {
		beforeStep(at)
	}
	next, err := cur.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	if got, err := next.Stat("."); err != nil || !os.SameFile(fi, got) {
		next.Close()
		return nil, &fs.PathError{Op: "open", Path: at, Err: errChanged}
	}
	return next, nil
}

// openLeafIn opens a file grant's leaf in its directory dir, as openLeaf's
// O_NOFOLLOW would: a symlink there is refused, and so is a leaf swapped
// between the lstat and the open. An existing leaf is opened without
// O_CREATE, a missing one with O_EXCL, so the open never creates a file
// where a swapped-in symlink points. A missing leaf opened without
// O_CREATE (a read) is reported missing without an open: os.Root would
// follow a symlink planted there after the lstat, and with nothing to
// compare it to, the open could not tell (review round 3).
func openLeafIn(dir *os.Root, path, leaf string, flag int, perm uint32) (*os.File, error) {
	fi, err := dir.Lstat(leaf)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if flag&os.O_CREATE == 0 {
			return nil, err
		}
		fi, flag = nil, flag|os.O_EXCL
	case err != nil:
		return nil, err
	case fi.Mode()&fs.ModeSymlink != 0:
		return nil, &fs.PathError{Op: "open", Path: path, Err: errChanged}
	default:
		flag &^= os.O_CREATE
	}
	if beforeStep != nil {
		beforeStep(path)
	}
	f, err := dir.OpenFile(leaf, flag|rootOpenFlags, fs.FileMode(perm))
	if err != nil {
		return nil, err
	}
	if fi != nil {
		if got, err := f.Stat(); err != nil || !os.SameFile(fi, got) {
			f.Close()
			return nil, &fs.PathError{Op: "open", Path: path, Err: errChanged}
		}
	}
	return f, nil
}

// renamePathError makes an os.Root error read like a plain open of path.
func renamePathError(err error, path string) error {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return &fs.PathError{Op: "open", Path: path, Err: pe.Err}
	}
	return err
}

// openRegularIn opens tg.path (beneath its grant, see openIn) for reading
// only if it is a regular file. It opens with O_NONBLOCK on unix so a FIFO
// with no writer cannot block the open (and the task, which cancellation
// cannot interrupt mid-syscall), then fstats the result and rejects
// anything not regular.
func openRegularIn(tg fsTarget) (*os.File, error) {
	f, err := openIn(tg, os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	return checkRegular(f)
}

// openWritableIn opens tg.path (beneath its grant, see openIn) for
// writing, creating it (mode perm) if missing, only if it is a regular
// file, and truncates it. With O_NONBLOCK, a FIFO with no reader fails at
// once (ENXIO) instead of blocking until a reader appears, deaf to
// cancellation; one with a reader opens and is refused by the fstat check.
// O_TRUNC would act on whatever was opened, so the file is truncated only
// once it is known to be regular.
//
// Known limit: a non-regular file is opened before it is refused, so a FIFO
// reader is woken with EOF and a device's open side effects still happen.
// This needs an fs_write grant that covers that file, so the task already
// holds authority over it.
func openWritableIn(tg fsTarget, perm uint32) (*os.File, error) {
	f, err := openIn(tg, os.O_WRONLY|os.O_CREATE, perm)
	if err != nil {
		return nil, err
	}
	if f, err = checkRegular(f); err != nil {
		return nil, err
	}
	if err := f.Truncate(0); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}
