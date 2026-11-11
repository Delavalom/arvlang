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

	// Effects are the host's own effects, by the name the program calls
	// them (Phase 8.2). Each needs a grant of its Kind; Caps may grant
	// host kinds, and the program names them with a constructor per kind,
	// e.g. spawn f() with [k8s("staging")].
	Effects map[string]Effect
	// Kinds declares host kinds with their own coverage rule. A kind that
	// an effect names and Kinds does not is declared with exact match.
	Kinds []runtime.KindSpec
}

// afterParse, when set (only by tests), runs between parsing and the root
// scope, where a panic is caught by Run's own recover alone.
var afterParse func()

// Run parses and evaluates src. A parse error (a *ParseError) or an Arv
// error value that reaches the top level (an *Error) is returned as a Go
// error. Every capability in
// opts.Caps is validated and canonicalised through runtime.New first (a
// Capability literal bypasses it); an invalid one is an error and nothing runs.
// So is an invalid host effect or kind (opts.Effects, opts.Kinds).
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
	kinds, host, err := hostSetup(opts)
	if err != nil {
		return nil, fmt.Errorf("arv: %w", err)
	}
	caps := make([]runtime.Capability, len(opts.Caps))
	for i, c := range opts.Caps {
		valid, err := kinds.New(c.Kind, c.Scope)
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
	env := value.NewEnvironment()
	for name, b := range host {
		env.Set(name, b)
	}
	root := runtime.NewRoot(opts.Ctx, runtime.NewCapSetKinds(kinds, caps...), opts.Out)
	result = evaluator.New(root).RunLimited(program, env, opts.Limits)
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
