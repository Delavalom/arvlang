# Phase 8.1, task p81.2: `walk`: every node, no recursion, fail closed

> **For agentic workers:** this file is one task, p81.2. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: task p81.1 is committed in your tree. Start every shell command with `cd <your worktree> &&` and use absolute paths: the shell's working directory does not persist. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify` and never push; keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0` (toolchain 1.26.2): `t.Chdir`, `min` and `max` are available; do not use `testing/synctest` or `sync.WaitGroup.Go`. Before a long test run, check `df -h /System/Volumes/Data`; below 1 GB free, stop and report (only `go clean -cache` may be deleted). This machine is heavily loaded: keep wall-clock bounds generous.
>
> Every code block below was applied, in task order, to a scratch copy of `feat/phases-3-8` (860a158) and passed `gofmt -l .`, `go vet ./...` and `go test -race ./...` (including `e2e/`). Design: `15-phase8-tooling.md` §8.1. Deviations from it, and why: `p81-plan-1.md`. Phases 5E, 6, 7 and 8.2 are built at the same time and also touch the CLI, parser and evaluator: change only the files this task names.

### Task p81.2: `walk`: every node, no recursion, fail closed

The walker visits every node of a program with an explicit stack, so a 200000-term infix chain (the parser does not count those as nesting) costs no Go frames. `children` lists what each node evaluates, binding names excluded (a `let`'s name, parameters, a `for` variable, `match`'s names are not references). A node type it does not know is an error, so syntax added later can never be skipped silently.

Two tests pin `arv/ast` by reading its source with `go/parser`: every node type must be in `nodeTypes` (and `children` must take its zero value, nil fields included), and every struct's field list must match `nodeFields`. If Phase 5E adds a node or a field instead of desugaring, these fail at merge until `children` (and p81.5's binding count) handle it.

**Files:**
- Create: `arv/check/walk.go`
- Create (test): `arv/check/walk_test.go`

- [ ] **Step 1: Write the failing test**

Create `arv/check/walk_test.go`:

```go
package check

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	arvast "github.com/delavalom/arvlang/arv/ast"
)

// nodeTypes has a zero value of every node type in package ast. children
// must handle each one, nil fields included.
var nodeTypes = map[string]arvast.Node{
	"ArrayLiteral":        &arvast.ArrayLiteral{},
	"AwaitExpression":     &arvast.AwaitExpression{},
	"BlockStatement":      &arvast.BlockStatement{},
	"Boolean":             &arvast.Boolean{},
	"CallExpression":      &arvast.CallExpression{},
	"ExpressionStatement": &arvast.ExpressionStatement{},
	"ForExpression":       &arvast.ForExpression{},
	"FunctionLiteral":     &arvast.FunctionLiteral{},
	"HashLiteral":         &arvast.HashLiteral{},
	"Identifier":          &arvast.Identifier{},
	"IfExpression":        &arvast.IfExpression{},
	"IndexExpression":     &arvast.IndexExpression{},
	"InfixExpression":     &arvast.InfixExpression{},
	"IntegerLiteral":      &arvast.IntegerLiteral{},
	"LetStatement":        &arvast.LetStatement{},
	"MatchExpression":     &arvast.MatchExpression{},
	"PrefixExpression":    &arvast.PrefixExpression{},
	"Program":             &arvast.Program{},
	"ReturnStatement":     &arvast.ReturnStatement{},
	"ScopeExpression":     &arvast.ScopeExpression{},
	"SpawnExpression":     &arvast.SpawnExpression{},
	"StringLiteral":       &arvast.StringLiteral{},
}

// astFiles parses package ast's source, tests left out.
func astFiles(t *testing.T) []*ast.File {
	t.Helper()
	entries, err := os.ReadDir("../ast")
	if err != nil {
		t.Fatal(err)
	}
	var files []*ast.File
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), filepath.Join("../ast", e.Name()), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, f)
	}
	return files
}

// astNodeTypes returns every type in package ast with an expressionNode
// or statementNode method, plus Program.
func astNodeTypes(t *testing.T) []string {
	t.Helper()
	found := map[string]bool{"Program": true}
	for _, f := range astFiles(t) {
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Recv == nil || (fd.Name.Name != "expressionNode" && fd.Name.Name != "statementNode") {
				continue
			}
			recv := fd.Recv.List[0].Type
			if star, ok := recv.(*ast.StarExpr); ok {
				recv = star.X
			}
			if id, ok := recv.(*ast.Ident); ok {
				found[id.Name] = true
			}
		}
	}
	var names []string
	for n := range found {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// A node type added to package ast (by a later phase) fails this test
// until children knows it: arv check must not skip syntax it cannot see.
func TestChildrenKnowsEveryNodeType(t *testing.T) {
	names := astNodeTypes(t)
	if len(names) < len(nodeTypes) {
		t.Fatalf("found only %d node types in ../ast: %v", len(names), names)
	}
	for _, name := range names {
		n, ok := nodeTypes[name]
		if !ok {
			t.Errorf("ast.%s is new: handle it in children (walk.go) and add it to nodeTypes", name)
			continue
		}
		if err := children(n, func(arvast.Node) {}); err != nil {
			t.Errorf("children(ast.%s): %v", name, err)
		}
		if err := walk(n, func(arvast.Node) bool { return true }); err != nil {
			t.Errorf("walk(ast.%s): %v", name, err)
		}
	}
}

type alienNode struct{ arvast.Expression }

func TestUnknownNodeStopsTheWalk(t *testing.T) {
	prog := &arvast.Program{Statements: []arvast.Statement{
		&arvast.ExpressionStatement{Expression: alienNode{}},
	}}
	err := walk(prog, func(arvast.Node) bool { return true })
	if err == nil || !strings.Contains(err.Error(), "syntax it does not know") {
		t.Fatalf("walk = %v, want an error", err)
	}
}

// nodeFields pins the fields of every struct in package ast. children
// visits the fields that are evaluated, and the constant folding in
// consts.go counts the names a node binds, so a field added by a later
// phase (a second loop variable, say) fails this test until both are
// checked against it and the entry here is updated.
var nodeFields = map[string]string{
	"ArrayLiteral":        "Token Elements",
	"AwaitExpression":     "Token Task",
	"BlockStatement":      "Token Statements",
	"Boolean":             "Token Value",
	"CallExpression":      "Token Function Arguments",
	"ExpressionStatement": "Token Expression",
	"ForExpression":       "Token Var Iterable Body",
	"FunctionLiteral":     "Token Parameters Body",
	"HashLiteral":         "Token Pairs",
	"Identifier":          "Token Value",
	"IfExpression":        "Token Condition Consequence Alternative",
	"IndexExpression":     "Token Left Index",
	"InfixExpression":     "Token Operator Right Left",
	"IntegerLiteral":      "Token Value",
	"LetStatement":        "Token Name Value",
	"MatchExpression":     "Token Subject OkName Ok ErrName Err",
	"PrefixExpression":    "Token Operator Right",
	"Program":             "Statements",
	"ReturnStatement":     "Token ReturnValue",
	"ScopeExpression":     "Token Options Body",
	"ScopeOption":         "Token Name Value",
	"SpawnExpression":     "Token Call Caps",
	"StringLiteral":       "Token Value",
}

func TestNodeFieldsAreKnown(t *testing.T) {
	found := map[string]string{}
	for _, f := range astFiles(t) {
		for _, d := range f.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok {
				continue
			}
			for _, sp := range gd.Specs {
				ts, ok := sp.(*ast.TypeSpec)
				if !ok {
					continue
				}
				st, ok := ts.Type.(*ast.StructType)
				if !ok {
					continue
				}
				var names []string
				for _, fl := range st.Fields.List {
					for _, n := range fl.Names {
						names = append(names, n.Name)
					}
					if len(fl.Names) == 0 {
						names = append(names, fmt.Sprint(fl.Type))
					}
				}
				found[ts.Name.Name] = strings.Join(names, " ")
			}
		}
	}
	for name, fields := range found {
		if want, ok := nodeFields[name]; !ok || want != fields {
			t.Errorf("ast.%s has fields %q, nodeFields says %q: check children (walk.go) and bindings (consts.go), then update nodeFields", name, fields, want)
		}
	}
	for name := range nodeFields {
		if _, ok := found[name]; !ok {
			t.Errorf("nodeFields names ast.%s, which no longer exists", name)
		}
	}
}
```

- [ ] **Step 2: Run and watch it fail**

Run: `go test ./arv/check/`
Expected: build failure: `undefined: children`, `undefined: walk`.

- [ ] **Step 3: Implement**

Create `arv/check/walk.go`:

```go
package check

import (
	"fmt"
	"reflect"

	"github.com/delavalom/arvlang/arv/ast"
)

// walk calls f on root and every node below it, parents before children,
// with an explicit stack: a 100000-term infix chain is as deep as the tree
// gets, and it must not cost a Go frame per level. f returns false to skip
// a node's children. A node of a type children does not know stops the
// walk with an error, so syntax added later can never be skipped silently.
func walk(root ast.Node, f func(ast.Node) bool) error {
	stack := []ast.Node{root}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if isNil(n) || !f(n) {
			continue
		}
		if err := children(n, func(c ast.Node) { stack = append(stack, c) }); err != nil {
			return err
		}
	}
	return nil
}

// isNil reports a missing child: a nil interface, or a nil pointer such as
// an if without else.
func isNil(n ast.Node) bool {
	if n == nil {
		return true
	}
	v := reflect.ValueOf(n)
	return v.Kind() == reflect.Pointer && v.IsNil()
}

// children calls add on each child of n that is evaluated. Names that a
// node binds (a let's name, a function's parameters, a for's variable,
// match's ok and err names) are not children: they are not references.
func children(n ast.Node, add func(ast.Node)) error {
	switch n := n.(type) {
	case *ast.Program:
		for _, s := range n.Statements {
			add(s)
		}
	case *ast.BlockStatement:
		for _, s := range n.Statements {
			add(s)
		}
	case *ast.ExpressionStatement:
		add(n.Expression)
	case *ast.LetStatement:
		add(n.Value)
	case *ast.ReturnStatement:
		add(n.ReturnValue)
	case *ast.Identifier, *ast.IntegerLiteral, *ast.StringLiteral, *ast.Boolean:
	case *ast.PrefixExpression:
		add(n.Right)
	case *ast.InfixExpression:
		add(n.Left)
		add(n.Right)
	case *ast.IfExpression:
		add(n.Condition)
		add(n.Consequence)
		add(n.Alternative)
	case *ast.FunctionLiteral:
		add(n.Body)
	case *ast.CallExpression:
		add(n.Function)
		for _, a := range n.Arguments {
			add(a)
		}
	case *ast.ArrayLiteral:
		for _, e := range n.Elements {
			add(e)
		}
	case *ast.IndexExpression:
		add(n.Left)
		add(n.Index)
	case *ast.HashLiteral:
		for k, v := range n.Pairs {
			add(k)
			add(v)
		}
	case *ast.ScopeExpression:
		for _, o := range n.Options {
			add(o.Value)
		}
		add(n.Body)
	case *ast.SpawnExpression:
		add(n.Call)
		add(n.Caps)
	case *ast.AwaitExpression:
		add(n.Task)
	case *ast.ForExpression:
		add(n.Iterable)
		add(n.Body)
	case *ast.MatchExpression:
		add(n.Subject)
		add(n.Ok)
		add(n.Err)
	default:
		line, col := ast.Pos(n)
		return fmt.Errorf("arv check: syntax it does not know, %T at %d:%d", n, line, col)
	}
	return nil
}
```

- [ ] **Step 4: Run and watch it pass**

Run: `go test -race ./arv/check/`
Expected: `ok`. Then `gofmt -l .` (prints nothing), `go vet ./...` and `go test -race ./...`.

- [ ] **Step 5: Commit**

```bash
git add arv/check
git commit -m "feat(check): walk every syntax node, failing on unknown ones" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
