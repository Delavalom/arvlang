//go:build freebsd || dragonfly || darwin

package evaluator

import (
	"os"
	"syscall"
)

// onProcFS reports whether the open file f is on a procfs, by the file
// system name fstatfs gives for the descriptor (see isProcFSName). macOS
// has no procfs; it shares this code so the path runs in tests there. If
// the kernel cannot say, it reports true: refusing is safe.
func onProcFS(f *os.File) bool {
	name, err := fsTypeName(f)
	return err != nil || isProcFSName(name)
}

// fsTypeName is the name of the file system the open file f is on.
func fsTypeName(f *os.File) (string, error) {
	rc, err := f.SyscallConn()
	if err != nil {
		return "", err
	}
	var st syscall.Statfs_t
	var serr error
	if err := rc.Control(func(fd uintptr) { serr = syscall.Fstatfs(int(fd), &st) }); err != nil {
		return "", err
	}
	if serr != nil {
		return "", serr
	}
	return fsTypeString(st.Fstypename[:]), nil
}
