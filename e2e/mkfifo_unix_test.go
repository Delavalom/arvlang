//go:build unix

package e2e

import "syscall"

func mkfifo(path string) error { return syscall.Mkfifo(path, 0o644) }
