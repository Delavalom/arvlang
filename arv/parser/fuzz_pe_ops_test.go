package parser

import (
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/ast"
)

// FuzzLogicalPrecedence decodes a random expression tree over && || <= >=
// and the older operators from the fuzzer's bytes, writes it with the
// fewest parentheses the precedence table allows (plus some extra ones and
// line breaks the bytes ask for), and checks that the parser builds
// exactly that tree, with every sub-expression placed at its own
// line:col. So precedence, left associativity and the positions of the
// desugared && / || (G5) hold for any mix.

// opGen reads choices from fuzz bytes; once they run out every choice is 0.
type opGen struct {
	b []byte
	i int
}

func (g *opGen) next() int {
	if g.i >= len(g.b) {
		return 0
	}
	g.i++
	return int(g.b[g.i-1])
}

// opNode is an operator (binary, or prefix when right is nil) or an atom.
type opNode struct {
	op          string
	left, right *opNode
	atom        string
	parens      bool // written in extra parentheses
}

var binaryOps = []string{"||", "&&", "==", "!=", "<", ">", "<=", ">=", "+", "-", "*", "/"}

var opPrec = map[string]int{
	"||": LOGOR, "&&": LOGAND, "==": EQUALS, "!=": EQUALS,
	"<": LESSGREATER, ">": LESSGREATER, "<=": LESSGREATER, ">=": LESSGREATER,
	"+": SUMMINUS, "-": SUMMINUS, "*": PRODUCT, "/": PRODUCT,
}

// Atoms bind tighter than any operator; each is placed (ast.Pos) at
// atomPos bytes from its start.
var opAtoms = []string{"a", "b", "1", "23", "true", "false", "r.up", "r.a.in", "f(x)", "xs[0]", `"t${a}"`, `"${a && b}u"`, `"\${a}"`}

func genOp(g *opGen, depth int) *opNode {
	c := g.next()
	if depth == 0 || c%4 == 0 {
		return &opNode{atom: opAtoms[g.next()%len(opAtoms)], parens: c&64 != 0}
	}
	if c%8 == 1 {
		op := "!"
		if c&16 != 0 {
			op = "-"
		}
		return &opNode{op: op, left: genOp(g, depth-1), parens: c&64 != 0}
	}
	n := &opNode{op: binaryOps[g.next()%len(binaryOps)], parens: c&64 != 0}
	n.left = genOp(g, depth-1)
	n.right = genOp(g, depth-1)
	return n
}

func (n *opNode) prec() int {
	switch {
	case n.atom != "" || n.parens:
		return INDEX + 1
	case n.right == nil:
		return PREFIX
	}
	return opPrec[n.op]
}

// opWriter writes a tree as source, recording where each node must be
// placed (its byte offset).
type opWriter struct {
	g   *opGen
	b   strings.Builder
	pos map[*opNode]int
}

func (w *opWriter) space() {
	if w.g.next()%5 == 0 {
		w.b.WriteString("\n  ")
	} else {
		w.b.WriteByte(' ')
	}
}

// write writes n where an operand of precedence min is expected; right is
// true for a right operand, which needs parentheses at equal precedence.
func (w *opWriter) write(n *opNode, min int, right bool) {
	p := n.prec()
	wrap := n.parens || p < min || (p == min && right)
	if wrap {
		w.b.WriteByte('(')
	}
	switch {
	case n.atom != "":
		w.pos[n] = w.b.Len() + atomPos(n.atom)
		w.b.WriteString(n.atom)
	case n.right == nil:
		w.pos[n] = w.b.Len()
		w.b.WriteString(n.op)
		w.write(n.left, PREFIX, false)
	default:
		w.write(n.left, opPrec[n.op], false)
		w.pos[n] = w.pos[leftmost(n)]
		w.space()
		w.b.WriteString(n.op)
		w.space()
		w.write(n.right, opPrec[n.op], true)
	}
	if wrap {
		w.b.WriteByte(')')
	}
}

// leftmost is the node whose position a binary node takes: its left
// operand's, down to an atom or a prefix operator.
func leftmost(n *opNode) *opNode {
	for n.atom == "" && n.right != nil {
		n = n.left
	}
	return n
}

func atomPos(atom string) int {
	prog, errs := fuzzParse(atom)
	if len(errs) > 0 || len(prog.Statements) != 1 {
		panic("bad atom " + atom)
	}
	_, col := ast.Pos(prog.Statements[0].(*ast.ExpressionStatement).Expression)
	return col - 1
}

// want is the String() the parser's tree must print.
func (n *opNode) want() string {
	switch {
	case n.atom != "":
		prog, _ := fuzzParse(n.atom)
		return prog.String()
	case n.right == nil:
		return "(" + n.op + n.left.want() + ")"
	}
	return "(" + n.left.want() + " " + n.op + " " + n.right.want() + ")"
}

func FuzzLogicalPrecedence(f *testing.F) {
	for _, s := range []string{"", "\x02\x00\x01\x02\x00\x03", "\x02\x01\x02\x00\x00\x02\x00\x01\x03\x00",
		"\x41\x07\x09\x02\x05\x06\x01\x00\x00\x02\x00\x00\x00", "\x11\x00\x01\x02\x03\x04\x05\x06\x07\x08\x09\x0a\x0b\x0c"} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		g := &opGen{b: data}
		tree := genOp(g, 6)
		w := &opWriter{g: g, pos: map[*opNode]int{}}
		w.write(tree, LOWEST, false)
		src := w.b.String()
		prog, errs := fuzzParse(src)
		if len(errs) > 0 {
			t.Fatalf("%q: %v", src, errs)
		}
		if got, want := prog.String(), tree.want(); got != want {
			t.Fatalf("%q parsed as %s, want %s", src, got, want)
		}
		checkOpPos(t, src, tree, prog.Statements[0].(*ast.ExpressionStatement).Expression, w.pos)
	})
}

// checkOpPos walks the tree and the parsed expression together and checks
// each node's line:col.
func checkOpPos(t *testing.T, src string, n *opNode, e ast.Expression, pos map[*opNode]int) {
	t.Helper()
	line, col := ast.Pos(e)
	if wl, wc := lineCol(src, pos[n]); line != wl || col != wc {
		t.Fatalf("%q: %s is at %d:%d, want %d:%d", src, e.String(), line, col, wl, wc)
	}
	if n.atom != "" {
		return
	}
	var l, r ast.Expression
	switch x := e.(type) {
	case *ast.PrefixExpression:
		l = x.Right
	case *ast.InfixExpression:
		l, r = x.Left, x.Right
	case *ast.IfExpression:
		l, r = x.Condition, x.Consequence.Statements[0].(*ast.ExpressionStatement).Expression
		if n.op == "||" {
			r = x.Alternative.Statements[0].(*ast.ExpressionStatement).Expression
		}
		if string(x.Token.Type) != n.op {
			t.Fatalf("%q: %s has token %s, want %s", src, e.String(), x.Token.Type, n.op)
		}
	default:
		t.Fatalf("%q: %s is a %T for %s", src, e.String(), e, n.op)
	}
	checkOpPos(t, src, n.left, l, pos)
	if n.right != nil {
		checkOpPos(t, src, n.right, r, pos)
	}
}

// lineCol is the line:col of byte off in src, as the lexer counts them.
func lineCol(src string, off int) (int, int) {
	line, col := 1, 1
	for i := 0; i < off; i++ {
		if src[i] == '\n' {
			line, col = line+1, 1
		} else {
			col++
		}
	}
	return line, col
}
