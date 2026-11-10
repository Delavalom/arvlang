//go:build !unix

package evaluator

import "os"

// rootOpenFlags are added to every open beneath a grant (openIn).
const rootOpenFlags = 0

// openLeaf opens path for a file grant. There is no O_NOFOLLOW here;
// Canonical's resolution is the only guard against a leaf symlink.
func openLeaf(path string, flag int, perm uint32) (*os.File, error) {
	return os.OpenFile(path, flag, os.FileMode(perm))
}
