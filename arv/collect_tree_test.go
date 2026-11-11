package arv

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// A random supervision tree for the collect property (collect_prop_test.go):
// leaves that succeed or fail with each kind, collect scopes that await
// every child or only some, and fail_fast scopes with at most one failing
// leaf. Each node is one top-level function, n<id>, on line id+2.
type pnode struct {
	id      int
	kind    int
	kids    []*pnode
	awaited []bool // collectSome: which kids the scope awaits
}

const (
	leafOK = iota
	leafUser
	leafCap
	leafEffect
	leafRuntime
	leafBudget
	collectAll  // awaits every kid with match
	collectSome // awaits only the kids marked in awaited
	failFast    // leaves, at most one failing; awaits them all
	numKinds
)

const maxTreeNodes = 80

// treeGen decodes a tree from bytes; once they run out every node is an ok
// leaf, so any input gives a finite tree.
type treeGen struct {
	b []byte
	i int
	n int
}

func (g *treeGen) next() byte {
	if g.i >= len(g.b) {
		return 0
	}
	g.i++
	return g.b[g.i-1]
}

func (g *treeGen) leaf(kind int) *pnode {
	n := &pnode{id: g.n, kind: kind}
	g.n++
	return n
}

func (g *treeGen) node(depth int, kind int) *pnode {
	if g.n >= maxTreeNodes || depth >= 3 && kind >= collectAll {
		kind %= collectAll
	}
	if g.n >= maxTreeNodes {
		kind = leafOK
	}
	n := g.leaf(kind)
	switch kind {
	case collectAll, collectSome:
		k := 1 + int(g.next()%4)
		mask := g.next()
		for i := 0; i < k; i++ {
			n.kids = append(n.kids, g.node(depth+1, int(g.next()%numKinds)))
			n.awaited = append(n.awaited, kind == collectAll || mask>>i&1 == 1)
		}
	case failFast:
		k := 1 + int(g.next()%3)
		bad := int(g.next() % byte(k+1)) // k: none fails
		badKind := leafUser + int(g.next()%5)
		for i := 0; i < k; i++ {
			lk := leafOK
			if i == bad {
				lk = badKind
			}
			n.kids = append(n.kids, g.leaf(lk))
		}
	}
	return n
}

// genTree decodes the tree; its root always collects every child.
func genTree(b []byte) *pnode {
	g := &treeGen{b: b}
	return g.node(0, collectAll)
}

func all(n *pnode, f func(*pnode)) {
	f(n)
	for _, k := range n.kids {
		all(k, f)
	}
}

// program renders the tree. dir is a granted directory, so read_file of a
// missing file in it is an effect error.
func (n *pnode) program(dir string) string {
	lines := make([]string, 0, maxTreeNodes+2)
	lines = append(lines, "let burn = fn(n) { if (n == 0) { 0 } else { burn(n - 1) } };")
	var nodes []*pnode
	all(n, func(p *pnode) { nodes = append(nodes, p) })
	byID := make([]string, len(nodes))
	for _, p := range nodes {
		byID[p.id] = fmt.Sprintf("let n%d = fn() { %s };", p.id, p.body(dir))
	}
	lines = append(lines, byID...)
	return strings.Join(append(lines, "n0()"), "\n")
}

const arm = "{ ok(v) => v, err(e) => e }"

func (n *pnode) body(dir string) string {
	switch n.kind {
	case leafOK:
		return fmt.Sprint(n.id)
	case leafUser:
		return fmt.Sprintf(`error("c%d")`, n.id)
	case leafCap:
		return fmt.Sprintf(`getenv("ARV_P4_NOPE_%d")`, n.id)
	case leafEffect:
		return fmt.Sprintf(`read_file("%s/m%d")`, dir, n.id)
	case leafRuntime:
		return fmt.Sprintf("len(%d)", n.id)
	case leafBudget:
		return "scope (steps: 50) { burn(100000) }"
	}
	spawns := make([]string, len(n.kids))
	for i, k := range n.kids {
		spawns[i] = fmt.Sprintf(`spawn n%d() with [fs_read("%s")]`, k.id, dir)
	}
	ts := "let ts = [" + strings.Join(spawns, ", ") + "]; "
	switch n.kind {
	case collectAll:
		return `scope (supervise: "collect") { ` + ts + "for t in ts { match await t " + arm + " } }"
	case collectSome:
		var picks []string
		for i, a := range n.awaited {
			if a {
				picks = append(picks, fmt.Sprintf("match await ts[%d] %s", i, arm))
			}
		}
		return `scope (supervise: "collect") { ` + ts + "[" + strings.Join(picks, ", ") + "] }"
	default: // failFast
		return "scope { " + ts + "for t in ts { await t } }"
	}
}

// outcome is what a node must produce: a value (an int, or the kids'
// outcomes in order) or an error with its kind, message, task and position.
type outcome struct {
	err        bool
	val        int
	list       []outcome
	isList     bool
	kind, task string
	pos        string // "" when not checked (budget)
	msg        string // exact message, or with msgHas a substring
	msgHas     bool
}

// expect is node n's outcome when it runs as task.
func (n *pnode) expect(task string) outcome {
	pos := fmt.Sprintf("%d:%d", n.id+2, len(fmt.Sprintf("let n%d = fn() { ", n.id))+1)
	fail := func(kind, msg string, has bool) outcome {
		return outcome{err: true, kind: kind, task: task, pos: pos, msg: msg, msgHas: has}
	}
	switch n.kind {
	case leafOK:
		return outcome{val: n.id}
	case leafUser:
		return fail(runtime.KindUser, fmt.Sprintf("c%d", n.id), false)
	case leafCap:
		return fail(runtime.KindCapability, fmt.Sprintf(`env("ARV_P4_NOPE_%d")`, n.id), true)
	case leafEffect:
		return fail(runtime.KindEffect, fmt.Sprintf("/m%d: no such file", n.id), true)
	case leafRuntime:
		return fail(runtime.KindRuntime, "argument to `len` not supported, got INTEGER", false)
	case leafBudget:
		o := fail(runtime.KindBudget, "budget exceeded: steps (limit 50)", false)
		o.pos = ""
		return o
	}
	kids := make([]outcome, len(n.kids))
	for i, k := range n.kids {
		kids[i] = k.expect(fmt.Sprintf("%s/%d", task, i))
	}
	res := outcome{isList: true}
	switch n.kind {
	case collectSome:
		var first *outcome
		count := 0
		for i := range kids {
			if n.awaited[i] {
				res.list = append(res.list, kids[i])
			} else if kids[i].err {
				if count++; first == nil {
					first = &kids[i]
				}
			}
		}
		if first != nil {
			o := *first
			o.msg, o.msgHas = fmt.Sprintf("%d unawaited task failures: ", count), true
			return o
		}
		return res
	case failFast:
		for _, k := range kids {
			if k.err {
				return k
			}
		}
	}
	res.list = kids
	return res
}

// checkOutcome compares got with want, naming the path on a mismatch.
func checkOutcome(path string, got value.Object, want outcome) error {
	switch {
	case want.err:
		h, ok := got.(*value.Hash)
		if !ok {
			return fmt.Errorf("%s: got %s, want an error of kind %s", path, got.Inspect(), want.kind)
		}
		field := func(k string) string {
			p, ok := h.Pairs[(&value.String{Value: k}).HashKey()]
			if !ok {
				return "<missing>"
			}
			if s, ok := p.Value.(*value.String); ok {
				return s.Value
			}
			return p.Value.Inspect()
		}
		msg := field("message")
		msgOK := msg == want.msg || want.msgHas && strings.Contains(msg, want.msg)
		if field("kind") != want.kind || !msgOK || field("task") != want.task ||
			want.pos != "" && field("pos") != want.pos {
			return fmt.Errorf("%s: got %s, want kind %s, message %q, task %s, pos %q",
				path, got.Inspect(), want.kind, want.msg, want.task, want.pos)
		}
	case want.isList:
		a, ok := got.(*value.Array)
		if !ok || len(a.Elements) != len(want.list) {
			return fmt.Errorf("%s: got %s, want %d outcomes", path, inspectOf(got), len(want.list))
		}
		for i, w := range want.list {
			if err := checkOutcome(fmt.Sprintf("%s[%d]", path, i), a.Elements[i], w); err != nil {
				return err
			}
		}
	default:
		if i, ok := got.(*value.Integer); !ok || i.Value != int64(want.val) {
			return fmt.Errorf("%s: got %s, want %d", path, inspectOf(got), want.val)
		}
	}
	return nil
}

func inspectOf(o value.Object) string {
	if o == nil {
		return "<nil>"
	}
	return o.Inspect()
}

// runTree runs the tree from b with dir granted and checks every child's
// outcome is accounted for exactly once, in spawn order.
func runTree(b []byte, dir string) (src string, err error) {
	tree := genTree(b)
	src = tree.program(dir)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	res, err := Run(src, Options{Ctx: ctx, Caps: []runtime.Capability{{Kind: runtime.FSRead, Scope: dir}}})
	if err != nil {
		return src, fmt.Errorf("run failed: %v", err)
	}
	return src, checkOutcome("n0", res, tree.expect("root"))
}

// realDir is dir with its symlinks resolved (macOS temp dirs are under one).
func realDir(dir string) string {
	if r, err := filepath.EvalSymlinks(dir); err == nil {
		return r
	}
	return dir
}
