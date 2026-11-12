// Package arv is the embedding API: run Arv source with an explicit set of
// capabilities. Nothing reaches the OS unless the host grants it here.
package arv

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/delavalom/arvlang/arv/evaluator"
	"github.com/delavalom/arvlang/arv/lexer"
	"github.com/delavalom/arvlang/arv/parser"
	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// Options configure one program run.
type Options struct {
	Ctx    context.Context      // defaults to context.Background()
	Caps   []runtime.Capability // authority granted to the root task; validated by runtime.New
	Out    io.Writer            // where puts writes; defaults to io.Discard
	Limits runtime.Limits       // the run's budget; zero values mean no limit, except LiveTasks (runtime.DefaultLiveTasks); a negative one is an error

	// Record, unless nil, receives the run's effect log (JSON Lines, one
	// write per line; see runtime.LogRecord). RecordSync, unless nil, runs
	// after each line: an fsync, so a crash loses no logged effect.
	Record     io.Writer
	RecordSync func() error
	// Replay, unless nil, is a finished run's log: every effect is
	// answered from it and none runs live (I7). The log's header supplies
	// the grants and limits, so Caps and Limits must be empty.
	Replay *runtime.Log
	// Resume, unless nil, is the log of a run that did not finish: each
	// task is answered from it while it has records, then runs live,
	// appending to Record (which must start at Resume.Size). Caps must be
	// the grants the run was recorded with, and Limits empty: the header
	// supplies them.
	Resume *runtime.Log
}

// afterParse, when set (only by tests), runs between parsing and the root
// scope, where a panic is caught by Run's own recover alone.
var afterParse func()

// Run parses and evaluates src. A parse error (a *ParseError) or an Arv
// error value that reaches the top level (an *Error) is returned as a Go
// error. Every capability in
// opts.Caps is validated and canonicalised through runtime.New first (a
// Capability literal bypasses it); an invalid one is an error and nothing runs.
// So is a negative limit in opts.Limits (runtime.Limits.Validate).
//
// A Go panic in the interpreter is returned as an error, never re-raised in
// the host. (Panics inside the program body already become Arv errors in the
// evaluator; this guards everything else, the parser included.)
func Run(src string, opts Options) (result value.Object, err error) {
	defer func() {
		if r := recover(); r != nil {
			result, err = nil, fmt.Errorf("arv: interpreter panicked: %v", r)
		}
	}()
	if err := opts.Limits.Validate(); err != nil {
		return nil, fmt.Errorf("arv: %w", err)
	}
	logged, err := logSetup(&opts)
	if err != nil {
		return nil, err
	}
	var caps []runtime.Capability
	if opts.Replay != nil {
		caps = logged // the log's grants: canonical when recorded, not resolved again
	} else if caps, err = validCaps(opts.Caps); err != nil {
		return nil, err
	}
	if opts.Resume != nil && !sameGrants(caps, logged) {
		return nil, fmt.Errorf("arv: resume needs the grants the run was recorded with: %s", grantList(logged))
	}
	if opts.Ctx == nil {
		opts.Ctx = context.Background()
	}
	if opts.Out == nil {
		opts.Out = io.Discard
	}
	p := parser.New(lexer.New(src))
	program := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		return nil, &ParseError{Errs: errs}
	}
	if afterParse != nil {
		afterParse()
	}
	l, err := startLog(src, opts, caps)
	if err != nil {
		return nil, err
	}
	ctx := opts.Ctx
	if l != nil {
		ctx = l.Start(ctx)
	}
	root := runtime.NewRoot(ctx, runtime.NewCapSet(caps...), opts.Out)
	root.SetLog(l)
	result = evaluator.New(root).RunLimited(program, value.NewEnvironment(), opts.Limits)
	if l != nil {
		if err := l.Finish(value.EncodeRequest([]value.Object{result})); err != nil {
			return result, &Error{Msg: err.Error(), Kind: runtime.ErrorKind(err, runtime.KindReplay)}
		}
	}
	if e, ok := result.(*value.Error); ok {
		return result, &Error{Msg: e.Message, Pos: e.Pos, Kind: e.ErrKind()}
	}
	return result, nil
}

// Error is an Arv error that reached the top of a run. Error() is the
// message alone; Pos ("line:col", or "" if unknown) says where in the
// source it arose, for the host to report with the file name. Kind is
// what caused it, one of the runtime.Kind* constants.
type Error struct {
	Msg  string
	Pos  string
	Kind string
}

func (e *Error) Error() string { return e.Msg }

// ParseError is a program that did not parse. Each of Errs is one error as
// the parser reports it, "msg (at line:col)" when it has a position.
type ParseError struct {
	Errs []string
}

func (e *ParseError) Error() string { return e.In("") }

// In is the error with each position given in file, "(at file:line:col)",
// for a host that reports errors with the file name.
func (e *ParseError) In(file string) string {
	msgs := make([]string, len(e.Errs))
	for i, m := range e.Errs {
		msgs[i] = m
		if file == "" {
			continue
		}
		// Only a trailing "(at line:col)" is a position.
		at := strings.LastIndex(m, " (at ")
		if at < 0 || !strings.HasSuffix(m, ")") {
			continue
		}
		line, col, ok := strings.Cut(m[at+len(" (at "):len(m)-1], ":")
		if ok && digits(line) && digits(col) {
			msgs[i] = fmt.Sprintf("%s (at %s:%s:%s)", m[:at], file, line, col)
		}
	}
	return "parse error: " + strings.Join(msgs, "; ")
}

func digits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}
