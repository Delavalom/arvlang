//go:build unix

package evaluator

import (
	"os"
	"syscall"
)

// openRegular opens path for reading only if it is a regular file. It opens
// with O_NONBLOCK so a FIFO with no writer cannot block the open (and the
// task, which cancellation cannot interrupt mid-syscall), fstats the result
// and rejects anything not regular. O_NONBLOCK is then cleared, so reads see
// an ordinary blocking descriptor. O_NOFOLLOW refuses a symlink at the leaf.
func openRegular(path string) (*os.File, error) {
	f, err := openNonblock(path, syscall.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	return checkRegular(f)
}

// openWritable opens path for writing, creating it (mode perm) if missing,
// only if it is a regular file, and truncates it. Like openRegular it opens
// with O_NONBLOCK: opening a FIFO for writing otherwise blocks until a
// reader appears, deaf to cancellation, and the task's scope could never
// finish. With O_NONBLOCK a FIFO with no reader fails at once (ENXIO); one
// with a reader opens and is refused by the fstat check. O_TRUNC would act
// on whatever was opened, so the file is truncated only once it is known to
// be regular. O_NOFOLLOW refuses a symlink at the leaf.
//
// Known limit: a non-regular file is opened before it is refused, so a FIFO
// reader is woken with EOF and a device's open side effects still happen.
// This needs an fs_write grant that covers that file, so the task already
// holds authority over it.
func openWritable(path string, perm uint32) (*os.File, error) {
	f, err := openNonblock(path, syscall.O_WRONLY|syscall.O_CREAT, perm)
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
