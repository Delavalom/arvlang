package utils

import (
	"fmt"

	"strings"

	"github.com/delavalom/arvlang/lang/ast"
)

func PrintAst(root ast.Node) {
	if root == nil {
		fmt.Println("nil")
		return
	}
	root.Traverse(0, func(level int, node ast.Node) {
		if node == nil {
			fmt.Println("nil")
			return
		}
		fmt.Println(strings.Repeat("  ", level) + node.String())
	})
}
