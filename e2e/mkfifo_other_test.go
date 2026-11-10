//go:build !unix

package e2e

import "errors"

func mkfifo(string) error { return errors.New("mkfifo: not supported") }
