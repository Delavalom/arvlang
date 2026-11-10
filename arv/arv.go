// Package arv is the embedding API: run Arv source with an explicit set of
// capabilities. Nothing reaches the OS unless the host grants it here.
package arv

import (
	"context"
	"errors"
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
	Ctx  context.Context      // defaults to context.Background()
	Caps []runtime.Capability // authority granted to the root task; validated by runtime.New
	Out  io.Writer            // where puts writes; defaults to io.Discard
}

// Run parses and evaluates src. A parse error or an Arv error value that
// reaches the top level is returned as a Go error. Every capability in
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
		return nil, errors.New("parse error: " + strings.Join(errs, "; "))
	}
	root := runtime.NewRoot(opts.Ctx, runtime.NewCapSet(caps...), opts.Out)
	result = evaluator.New(root).Run(program, value.NewEnvironment())
	if e, ok := result.(*value.Error); ok {
		return result, errors.New(e.Message)
	}
	return result, nil
}
