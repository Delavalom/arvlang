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
	manifest               bool // whether --manifest was given
}

var limitFlags = map[string]bool{"--timeout": true, "--steps": true, "--tasks": true, "--alloc-mb": true,
	"--tokens": true, "--usd-cents": true, "--max-turns": true}

// splitLogFlags takes the log flags out of run's arguments, leaving the
// rest for parseRun. --fsync takes no value; the others take a file.
// --manifest, which takes no value either, stays in the rest.
func splitLogFlags(args []string) (logArgs, []string, error) {
	var la logArgs
	var rest []string
	for len(args) > 0 && strings.HasPrefix(args[0], "--") {
		flag := args[0]
		switch flag {
		case "--fsync":
			la.fsync = true
			args = args[1:]
			continue
		case "--manifest":
			la.manifest = true
			rest = append(rest, flag)
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
	case la.replay != "" && la.manifest:
		return la, nil, errors.New("--replay takes its grants from the log; drop --manifest")
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
		// The file is created (or emptied) at the run's first line, the
		// header, which arv.Run writes once the script has parsed and the
		// grants are valid: a typo does not destroy an earlier recording.
		lf := &lazyLog{path: la.record}
		opts.Record = lf
		if la.fsync {
			opts.RecordSync = lf.sync
		}
		return lf.close, nil
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
			opts.RecordSync = syncOf(f)
		}
		warn(stderr, l.Header, src, file)
		if l.End != nil {
			// A finished run is replayed, not resumed: nothing is
			// appended. It still needs the grants it had (--allow).
			caps, err := arv.LogGrants(l.Header)
			if err != nil {
				f.Close()
				return nil, err
			}
			fmt.Fprintf(stderr, "replay: the run finished; grants: %s\n", arv.GrantList(caps))
		}
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

// syncOf is f.Sync: what --fsync runs after every line. A test replaces
// it to see that it runs.
var syncOf = func(f *os.File) func() error { return f.Sync }

// lazyLog is a --record file, opened and emptied at its first write.
type lazyLog struct {
	path string
	f    *os.File
	err  error
}

func (lf *lazyLog) open() error {
	if lf.f != nil || lf.err != nil {
		return lf.err
	}
	f, err := os.OpenFile(lf.path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err == nil {
		// The log may hold secrets: an old file keeps its mode otherwise.
		// Truncated first, so nothing is written while others may read it.
		if err = f.Chmod(0o600); err != nil {
			f.Close()
		}
	}
	if err != nil {
		lf.err = err
		return err
	}
	lf.f = f
	return nil
}

func (lf *lazyLog) Write(p []byte) (int, error) {
	if err := lf.open(); err != nil {
		return 0, err
	}
	return lf.f.Write(p)
}

func (lf *lazyLog) sync() error {
	if lf.f == nil {
		return nil
	}
	return syncOf(lf.f)()
}

func (lf *lazyLog) close() error {
	if lf.f == nil {
		return nil
	}
	return lf.f.Close()
}
