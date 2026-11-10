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
	Limits runtime.Limits       // the run's budget; zero values mean no limit, except LiveTasks (runtime.DefaultLiveTasks)
}

// afterParse, when set (only by tests), runs between parsing and the root
// scope, where a panic is caught by Run's own recover alone.
var afterParse func()

// Run parses and evaluates src. A parse error (a *ParseError) or an Arv
// error value that reaches the top level (an *Error) is returned as a Go
// error. Every capability in
// opts.Caps is validated and canonicalised through runtime.New first (a
// Capability literal bypasses it); an invalid one is an error and nothing runs.
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
	caps := make([]runtime.Capability, len(opts.Caps))
	for i, c := range opts.Caps {
		valid, err := runtime.New(c.Kind, c.Scope)
		if err != nil {
			return nil, fmt.Errorf("invalid capability %s: %w", c, err)
		}
		caps[i] = valid
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
	root := runtime.NewRoot(opts.Ctx, runtime.NewCapSet(caps...), opts.Out)
	result = evaluator.New(root).RunLimited(program, value.NewEnvironment(), opts.Limits)
	if e, ok := result.(*value.Error); ok {
		return result, &Error{Msg: e.Message, Pos: e.Pos}
	}
	return result, nil
}

// Error is an Arv error that reached the top of a run. Error() is the
// message alone; Pos ("line:col", or "" if unknown) says where in the
// source it arose, for the host to report with the file name.
type Error struct {
	Msg string
	Pos string
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
