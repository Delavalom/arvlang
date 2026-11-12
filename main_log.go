package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/delavalom/arvlang/arv"
	"github.com/delavalom/arvlang/arv/runtime"
)

// logArgs are run's effect log flags (Phase 6).
type logArgs struct {
	record, replay, resume string // a log file each; at most one set
	fsync                  bool
	allow, limits          bool // whether --allow, or a limit flag, was given
}

var limitFlags = map[string]bool{"--timeout": true, "--steps": true, "--tasks": true, "--alloc-mb": true}

// splitLogFlags takes the log flags out of run's arguments, leaving the
// rest for parseRun. --fsync takes no value; the others take a file.
func splitLogFlags(args []string) (logArgs, []string, error) {
	var la logArgs
	var rest []string
	for len(args) > 0 && strings.HasPrefix(args[0], "--") {
		flag := args[0]
		if flag == "--fsync" {
			la.fsync = true
			args = args[1:]
			continue
		}
		if len(args) < 2 {
			break
		}
		switch flag {
		case "--record":
			la.record = args[1]
		case "--replay":
			la.replay = args[1]
		case "--resume":
			la.resume = args[1]
		default:
			la.allow = la.allow || flag == "--allow"
			la.limits = la.limits || limitFlags[flag]
			rest = append(rest, args[0], args[1])
		}
		args = args[2:]
	}
	n := 0
	for _, f := range []string{la.record, la.replay, la.resume} {
		if f != "" {
			n++
		}
	}
	switch {
	case n > 1:
		return la, nil, errors.New("use one of --record, --replay and --resume")
	case la.fsync && la.record == "" && la.resume == "":
		return la, nil, errors.New("--fsync needs --record or --resume")
	case la.replay != "" && la.allow:
		return la, nil, errors.New("--replay takes its grants from the log; drop --allow")
	case (la.replay != "" || la.resume != "") && la.limits:
		return la, nil, errors.New("--replay and --resume take their limits from the log; drop the limit flags")
	}
	return la, append(rest, args...), nil
}

// setup opens the log la names and sets opts to use it. It returns what
// to close once the run is over.
func (la logArgs) setup(opts *arv.Options, src, file string, stderr io.Writer) (func() error, error) {
	switch {
	case la.record != "":
		f, err := os.OpenFile(la.record, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
		if err != nil {
			return nil, err
		}
		// The log may hold secrets: an old file keeps its mode otherwise.
		// Truncated first, so nothing is written while others may read it.
		if err := f.Chmod(0o600); err != nil {
			f.Close()
			return nil, err
		}
		opts.Record = f
		if la.fsync {
			opts.RecordSync = f.Sync
		}
		return f.Close, nil
	case la.replay != "":
		f, err := os.Open(la.replay)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		l, err := runtime.ReadLog(f)
		if err == nil && l.End == nil {
			err = runtime.ErrUnfinished
		}
		if err != nil {
			return nil, err
		}
		caps, err := arv.LogGrants(l.Header) // checked before they are named
		if err != nil {
			return nil, err
		}
		opts.Replay, opts.Limits = l, runtime.Limits{}
		warn(stderr, l.Header, src, file)
		fmt.Fprintf(stderr, "replay: grants: %s\n", arv.GrantList(caps))
		return func() error { return nil }, nil
	case la.resume != "":
		f, err := os.OpenFile(la.resume, os.O_RDWR, 0)
		if err != nil {
			return nil, err
		}
		l, err := runtime.ReadLog(f)
		if err == nil {
			err = f.Truncate(l.Size) // a partial last line: the crash cut it off
		}
		if err == nil {
			_, err = f.Seek(l.Size, io.SeekStart)
		}
		if err != nil {
			f.Close()
			return nil, err
		}
		opts.Resume, opts.Record, opts.Limits = l, f, runtime.Limits{}
		if la.fsync {
			opts.RecordSync = f.Sync
		}
		warn(stderr, l.Header, src, file)
		return f.Close, nil
	}
	return func() error { return nil }, nil
}

// warn says when the script or the directory is not the recorded run's:
// the run goes on, and a divergence will name what differs.
func warn(stderr io.Writer, h runtime.Header, src, file string) {
	if h.ScriptSHA256 != arv.ScriptSHA256(src) {
		fmt.Fprintf(stderr, "warning: %s has changed since the log was recorded\n", file)
	}
	if cwd, err := os.Getwd(); err == nil && h.Cwd != "" && cwd != h.Cwd {
		fmt.Fprintf(stderr, "warning: the log was recorded in %s; relative paths resolve against %s\n", h.Cwd, cwd)
	}
}
