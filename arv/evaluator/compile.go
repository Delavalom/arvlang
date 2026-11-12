package evaluator

import "github.com/delavalom/arvlang/arv/ast"

// compile turns node and everything under it into code. Until the node
// types get their own cases (p84.3 to p84.5), every node runs on the
// tree-walker.
func compile(node ast.Node) *code { return walked(node) }
