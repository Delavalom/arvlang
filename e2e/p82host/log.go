package main

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/delavalom/arvlang/arv"
	"github.com/delavalom/arvlang/arv/runtime"
)

// logOptions sets opts up for -record, -replay or -resume FILE (at most
// one of them), as the arv CLI does: a replay takes its grants and limits
// from the log, a resume its limits, and a flag set (set, by name) that
// the log would override is refused. It returns what closes the file.
func logOptions(opts *arv.Options, set map[string]bool, record, replay, resume string) (func(), error) {
	limits := []string{"steps", "alloc-mb", "timeout-ms"}
	if replay != "" {
		if err := refuse(set, "a replay takes its grants and limits from the log", append([]string{"cap"}, limits...)); err != nil {
			return nil, err
		}
	}
	if resume != "" {
		if err := refuse(set, "a resume takes its limits from the log", limits); err != nil {
			return nil, err
		}
	}
	n := 0
	for _, f := range []string{record, replay, resume} {
		if f != "" {
			n++
		}
	}
	switch {
	case n > 1:
		return nil, errors.New("-record, -replay and -resume exclude each other")
	case record != "":
		f, err := os.OpenFile(record, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			return nil, err
		}
		opts.Record = f
		return func() { f.Close() }, nil
	case replay != "":
		f, err := os.Open(replay)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		l, err := runtime.ReadLog(f)
		if err != nil {
			return nil, err
		}
		opts.Replay, opts.Caps, opts.Limits = l, nil, runtime.Limits{}
		return func() {}, nil
	case resume != "":
		f, err := os.OpenFile(resume, os.O_RDWR, 0)
		if err != nil {
			return nil, err
		}
		l, err := runtime.ReadLog(f)
		if err == nil { // drop a partial last line: the resume appends after the whole ones
			if err = f.Truncate(l.Size); err == nil {
				_, err = f.Seek(l.Size, io.SeekStart)
			}
		}
		if err != nil {
			f.Close()
			return nil, err
		}
		opts.Resume, opts.Record, opts.Limits = l, f, runtime.Limits{}
		return func() { f.Close() }, nil
	}
	return func() {}, nil
}

// refuse fails, saying why, if any flag in names was set.
func refuse(set map[string]bool, why string, names []string) error {
	for _, f := range names {
		if set[f] {
			return fmt.Errorf("%s: drop -%s", why, f)
		}
	}
	return nil
}
