//go:build unix

package evaluator

import (
	"os"
	"syscall"
)

// oNoFollow makes open fail (ELOOP) if the final path component is a symlink.
const oNoFollow = syscall.O_NOFOLLOW

// openRegular opens path for reading only if it is a regular file. It opens
// with O_NONBLOCK so a FIFO with no writer cannot block the open (and the
// task, which cancellation cannot interrupt mid-syscall), fstats the result
// and rejects anything not regular. O_NONBLOCK is then cleared, so reads see
// an ordinary blocking descriptor. O_NOFOLLOW refuses a symlink at the leaf.
func openRegular(path string) (*os.File, error) {
	var fd int
	var err error
	for {
		fd, err = syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
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
	return checkRegular(os.NewFile(uintptr(fd), path))
}
