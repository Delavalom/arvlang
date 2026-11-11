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
