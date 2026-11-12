//go:build !linux && !freebsd && !dragonfly && !darwin

package evaluator

import "os"

// onProcFS reports whether the open file f is on procfs: never, where no
// procfs check exists (see procfs_linux.go and procfs_bsd.go).
func onProcFS(f *os.File) bool { return false }
