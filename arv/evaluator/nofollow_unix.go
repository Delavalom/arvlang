//go:build unix

package evaluator

import "syscall"

// oNoFollow makes open fail (ELOOP) if the final path component is a symlink.
const oNoFollow = syscall.O_NOFOLLOW
