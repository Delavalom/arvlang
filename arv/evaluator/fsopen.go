package evaluator

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// beforeOpen, when set (only by tests), runs after the capability check and
// before the open: the window in which another process could swap a
// directory inside the grant for a symlink.
var beforeOpen func(path string)

// openIn opens path, which is canonical and covered by grant (fsRequest
// returns both), so that the open cannot leave grant even if the tree
// changes after the check. A directory grant is opened as an os.Root and
// path is opened beneath it: os.Root follows a symlink only while it stays
// inside the root and refuses one that escapes ("path escapes from
// parent"), so a directory swapped for a symlink to outside the grant is
// refused, while one swapped for a symlink to elsewhere inside it reaches a
// file the task may open anyway. When path is the grant itself (a file
// grant), there is no directory inside the grant to confine, and openLeaf
// opens it directly with O_NOFOLLOW.
//
// Errors name path, as a plain open would.
func openIn(grant, path string, flag int, perm uint32) (*os.File, error) {
	if beforeOpen != nil {
		beforeOpen(path)
	}
	if path == grant {
		return openLeaf(path, flag, perm)
	}
	rel, ok := strings.CutPrefix(path, grant)
	if grant != string(filepath.Separator) {
		rel, ok = strings.CutPrefix(rel, string(filepath.Separator))
	}
	if !ok || rel == "" {
		return nil, &fs.PathError{Op: "open", Path: path, Err: errors.New("path is not inside its grant")}
	}
	root, err := os.OpenRoot(grant)
	if err != nil {
		return nil, renamePathError(err, path)
	}
	defer root.Close()
	f, err := root.OpenFile(rel, flag|rootOpenFlags, fs.FileMode(perm))
	if err != nil {
		return nil, renamePathError(err, path)
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

// openRegular opens path for reading only if it is a regular file, treating
// path as its own grant (see openIn).
func openRegular(path string) (*os.File, error) { return openRegularIn(path, path) }

// openRegularIn opens path (beneath grant, see openIn) for reading only if
// it is a regular file. It opens with O_NONBLOCK on unix so a FIFO with no
// writer cannot block the open (and the task, which cancellation cannot
// interrupt mid-syscall), then fstats the result and rejects anything not
// regular.
func openRegularIn(grant, path string) (*os.File, error) {
	f, err := openIn(grant, path, os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	return checkRegular(f)
}

// openWritable opens path for writing, treating path as its own grant.
func openWritable(path string, perm uint32) (*os.File, error) {
	return openWritableIn(path, path, perm)
}

// openWritableIn opens path (beneath grant, see openIn) for writing,
// creating it (mode perm) if missing, only if it is a regular file, and
// truncates it. With O_NONBLOCK, a FIFO with no reader fails at once
// (ENXIO) instead of blocking until a reader appears, deaf to cancellation;
// one with a reader opens and is refused by the fstat check. O_TRUNC would
// act on whatever was opened, so the file is truncated only once it is
// known to be regular.
//
// Known limit: a non-regular file is opened before it is refused, so a FIFO
// reader is woken with EOF and a device's open side effects still happen.
// This needs an fs_write grant that covers that file, so the task already
// holds authority over it.
func openWritableIn(grant, path string, perm uint32) (*os.File, error) {
	f, err := openIn(grant, path, os.O_WRONLY|os.O_CREATE, perm)
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
