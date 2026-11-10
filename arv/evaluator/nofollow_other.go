//go:build !unix

package evaluator

import "os"

// oNoFollow is unavailable here; Canonical's resolution is the only guard.
const oNoFollow = 0

// openRegular opens path for reading only if it is a regular file.
func openRegular(path string) (*os.File, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	return checkRegular(f)
}
