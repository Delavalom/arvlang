//go:build !unix

package evaluator

import "os"

// openRegular opens path for reading only if it is a regular file.
func openRegular(path string) (*os.File, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	return checkRegular(f)
}

// openWritable opens path for writing, creating it if missing, only if it
// is a regular file, and only then truncates it. There is no O_NOFOLLOW
// here; Canonical's resolution is the only guard against a leaf symlink.
func openWritable(path string, perm uint32) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE, os.FileMode(perm))
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
