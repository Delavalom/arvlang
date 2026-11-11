package check

import (
	"fmt"
	"sort"
	"strings"

	"github.com/delavalom/arvlang/arv/ast"
	"github.com/delavalom/arvlang/arv/evaluator"
	"github.com/delavalom/arvlang/arv/lexer"
	"github.com/delavalom/arvlang/arv/parser"
	"github.com/delavalom/arvlang/arv/runtime"
)

// Need is a grant the program can need: a capability constructor, or an
// effect, called with an argument known before running.
type Need struct {
	Kind runtime.Kind
	// Scope is normalised for net (as the runtime compares it), and as
	// written for a path (relative to the working directory, as --allow
	// takes it) or an environment variable.
	Scope     string
	Line, Col int    // the call
	Fn        string // the builtin called
}

// String is the need in --allow form, kind=scope.
func (n Need) String() string { return string(n.Kind) + "=" + n.Scope }

// Note is a call arv check cannot turn into a need: its argument is not
// known before running, the builtin is used as a value, or the runtime
// would refuse the call before admitting anything.
type Note struct {
	Line, Col int
	Fn        string
	Kind      runtime.Kind // what the call can need; "" if arv check does not know
	Msg       string
}

func (n Note) String() string {
	return fmt.Sprintf("%s at %d:%d: %s", n.Fn, n.Line, n.Col, n.Msg)
}

// Report is what a program can be admitted for.
type Report struct {
	Needs []Need // the least set of grants that covers Sites, sorted by kind, then scope
	Sites []Need // every call that needs a grant, in source order
	Notes []Note // in source order
}

// ParseError is a program that did not parse; Errs are the parser's.
type ParseError struct{ Errs []string }

func (e *ParseError) Error() string { return "parse error: " + strings.Join(e.Errs, "; ") }

// Source parses src and checks it. A program that does not parse is a
// *ParseError.
func Source(src string) (*Report, error) {
	p := parser.New(lexer.New(src))
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		return nil, &ParseError{Errs: errs}
	}
	return Program(prog)
}

// Program checks prog. Every call of an effect or a capability constructor
// in the program text becomes a Need or a Note, wherever it is: inside a
// function never called, a branch never taken, a scope option, a match
// arm. Using such a builtin other than by calling it by name, as in
// let f = http_get, is a Note, since the value can then be called with
// anything. So the report covers every grant the program can be admitted
// for, except where a Note says it cannot tell.
func Program(prog *ast.Program) (*Report, error) {
	a := &analysis{builtins: map[string]bool{}, callees: map[*ast.Identifier]bool{}}
	for _, name := range evaluator.BuiltinNames() {
		a.builtins[name] = true
	}
	if err := walk(prog, a.visit); err != nil {
		return nil, err
	}
	return a.report(), nil
}

type analysis struct {
	builtins map[string]bool
	callees  map[*ast.Identifier]bool // names called directly, handled at the call
	sites    []Need
	notes    []Note
}

func (a *analysis) visit(n ast.Node) bool {
	switch n := n.(type) {
	case *ast.CallExpression:
		if id, ok := n.Function.(*ast.Identifier); ok {
			if e, ok := effects[id.Value]; ok {
				a.callees[id] = true
				a.call(id, e, n.Arguments)
			}
		}
	case *ast.Identifier:
		if !a.callees[n] {
			a.reference(n)
		}
	}
	return true
}

// call records what calling the builtin id with args needs.
func (a *analysis) call(id *ast.Identifier, e effect, args []ast.Expression) {
	if len(args) != e.nargs {
		a.note(id, e.kind, fmt.Sprintf("never allowed: %d arguments (it takes %d)", len(args), e.nargs))
		return
	}
	arg, ok := a.constString(args[0])
	if !ok {
		a.note(id, e.kind, e.what+" not statically known")
		return
	}
	scope, err := e.scope(e.kind, arg)
	if err != nil {
		a.note(id, e.kind, "never allowed: "+err.Error())
		return
	}
	line, col := ast.Pos(id)
	a.sites = append(a.sites, Need{Kind: e.kind, Scope: scope, Line: line, Col: col, Fn: id.Value})
}

// reference handles a name used other than by calling it. A user binding
// may shadow a builtin's name; it is treated as the builtin all the same,
// which can only add notes.
func (a *analysis) reference(id *ast.Identifier) {
	if e, ok := effects[id.Value]; ok {
		a.note(id, e.kind, "used as a value: what it needs is not statically known")
	} else if a.builtins[id.Value] && !pure[id.Value] {
		a.note(id, "", "a builtin arv check does not know: what it needs is not known")
	}
}

// constString is e's value if it is a string known before running.
func (a *analysis) constString(e ast.Expression) (string, bool) {
	if s, ok := e.(*ast.StringLiteral); ok {
		return s.Value, true
	}
	return "", false
}

func (a *analysis) note(id *ast.Identifier, kind runtime.Kind, msg string) {
	line, col := ast.Pos(id)
	a.notes = append(a.notes, Note{Line: line, Col: col, Fn: id.Value, Kind: kind, Msg: msg})
}

// report sorts the sites and notes by position (the walk's order is not
// the source's) and collects the distinct needs.
func (a *analysis) report() *Report {
	sort.SliceStable(a.sites, func(i, j int) bool { return before(a.sites[i].Line, a.sites[i].Col, a.sites[j].Line, a.sites[j].Col) })
	sort.SliceStable(a.notes, func(i, j int) bool { return before(a.notes[i].Line, a.notes[i].Col, a.notes[j].Line, a.notes[j].Col) })
	return &Report{Needs: least(distinct(a.sites)), Sites: a.sites, Notes: a.notes}
}

func before(l1, c1, l2, c2 int) bool { return l1 < l2 || l1 == l2 && c1 < c2 }

// rank is the order needs are printed in: the README's order of kinds,
// then any other kind.
func rank(k runtime.Kind) int {
	for i, o := range []runtime.Kind{runtime.Net, runtime.FSRead, runtime.FSWrite, runtime.Env} {
		if k == o {
			return i
		}
	}
	return 4
}

// distinct keeps the first site of each grant, sorted by kind, then scope.
func distinct(sites []Need) []Need {
	seen := map[[2]string]bool{}
	var out []Need
	for _, s := range sites {
		k := [2]string{string(s.Kind), s.Scope}
		if !seen[k] {
			seen[k] = true
			out = append(out, s)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return rank(out[i].Kind) < rank(out[j].Kind)
		}
		return out[i].Scope < out[j].Scope
	})
	return out
}
