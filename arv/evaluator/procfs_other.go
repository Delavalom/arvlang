//go:build !linux

package evaluator

import "os"

// onProcFS reports whether the open file f is on procfs: never, off Linux
// (see procfs_linux.go).
func onProcFS(f *os.File) bool { return false }
