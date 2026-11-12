//go:build linux

package evaluator

import (
	"os"
	"syscall"
)

// procSuperMagic is procfs's f_type in statfs(2).
const procSuperMagic = 0x9fa0

// onProcFS reports whether the open file f is on procfs. It asks the
// kernel about the descriptor, so no path, symlink or PID spelling gets
// around it. If the kernel cannot say, it reports true: refusing is safe.
func onProcFS(f *os.File) bool {
	rc, err := f.SyscallConn()
	if err != nil {
		return true
	}
	var st syscall.Statfs_t
	var serr error
	if err := rc.Control(func(fd uintptr) { serr = syscall.Fstatfs(int(fd), &st) }); err != nil || serr != nil {
		return true
	}
	return st.Type == procSuperMagic
}
