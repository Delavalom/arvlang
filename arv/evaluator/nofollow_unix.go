//go:build unix

package evaluator

import (
	"os"
	"syscall"
)

// rootOpenFlags are added to every open beneath a grant (openIn). os.Root
// adds O_NOFOLLOW and O_CLOEXEC itself; O_NONBLOCK keeps a FIFO from
// blocking the open (see openRegularIn). Regular files ignore O_NONBLOCK,
// so a descriptor that passes checkRegular reads and writes as usual.
const rootOpenFlags = syscall.O_NONBLOCK

// openLeaf opens path for a file grant (the path is the grant itself), so
// there is no directory inside the grant to confine: O_NOFOLLOW refuses a
// symlink planted at the leaf after the check, and O_NONBLOCK keeps a FIFO
// from blocking the open.
func openLeaf(path string, flag int, perm uint32) (*os.File, error) {
	return openNonblock(path, flag, perm)
}

// openNonblock opens path with mode plus O_NOFOLLOW, O_NONBLOCK and
// O_CLOEXEC, then clears O_NONBLOCK so reads and writes block as usual.
func openNonblock(path string, mode int, perm uint32) (*os.File, error) {
	var fd int
	var err error
	for {
		fd, err = syscall.Open(path, mode|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, perm)
		if err != syscall.EINTR {
			break
		}
	}
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	if err := syscall.SetNonblock(fd, false); err != nil {
		syscall.Close(fd)
		return nil, &os.PathError{Op: "fcntl", Path: path, Err: err}
	}
	return os.NewFile(uintptr(fd), path), nil
}
