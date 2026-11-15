package arv

import (
	"bytes"
	"errors"
	"strings"

	"github.com/delavalom/arvlang/arv/runtime"
)

// A random expression in the Phase 5E syntax, written twice: as the
// ergonomic form and as the hand-written code it means (pE-plan-1.md):
//
//	h.k          h["k"]
//	a && b       if (a) { b } else { false }
//	a || b       if (a) { true } else { b }
//	"t${e}u"     "t" + str(e) + "u"   (empty pieces left out)
//
// Every other node is written the same way in both. The two programs must
// give the same value or error, print the same output, and be charged the
// same steps and bytes (ergo_prop_test.go).

// egen reads choices from bytes; once they run out every choice is 0, so
// any input gives a finite expression.
type egen struct {
	b     []byte
	i     int
	nodes int
}

func (g *egen) next() int {
	if g.i >= len(g.b) {
		return 0
	}
	g.i++
	return int(g.b[g.i-1])
}

type enode struct {
	kind  string // atom, field, and, or, bin, if, not, neg, tick, spawn, interp
	text  string // atom: its source; field: the key; bin: the operator
	kids  []*enode
	texts []string // interp: len(kids)+1 text pieces
}

// The atoms hold no string literal and no ${, so any of them can sit
// inside ${...}. boom fails; h.missing is nil.
var eAtoms = []string{"0", "1", "7", "a", "b", "s", "xs", "true", "false", "(1 / 0)", "tick(a)"}

var eFields = []string{"up", "n", "name", "missing", "in", "sub"}

var eBinops = []string{"==", "!=", "<", ">", "<=", ">=", "+", "-", "*"}

// Text pieces of an interpolated string. None ends in a backslash or a
// lone $, so no piece can join the next ${ into something else; \${ is the
// escape, written the same in both forms.
var eTexts = []string{"", "", "a", " ", "x-y", `\${n}`, "{", "}", "$a", `\a`, "\n"}

const eMaxNodes = 40

// gen builds an expression; inStr is true inside ${...}, where an
// interpolated string (it needs quotes) cannot appear.
func (g *egen) gen(depth int, inStr bool) *enode {
	g.nodes++
	c := g.next()
	if depth == 0 || g.nodes > eMaxNodes || c%3 == 0 {
		if c&32 != 0 {
			return g.field()
		}
		return &enode{kind: "atom", text: eAtoms[g.next()%len(eAtoms)]}
	}
	switch k := (c / 3) % 9; {
	case k <= 1:
		return &enode{kind: "and", kids: []*enode{g.gen(depth-1, inStr), g.gen(depth-1, inStr)}}
	case k <= 3:
		return &enode{kind: "or", kids: []*enode{g.gen(depth-1, inStr), g.gen(depth-1, inStr)}}
	case k == 4:
		op := eBinops[g.next()%len(eBinops)]
		return &enode{kind: "bin", text: op, kids: []*enode{g.gen(depth-1, inStr), g.gen(depth-1, inStr)}}
	case k == 5:
		return &enode{kind: "if", kids: []*enode{g.gen(depth-1, inStr), g.gen(depth-1, inStr), g.gen(depth-1, inStr)}}
	case k == 6:
		kinds := []string{"not", "neg", "tick", "spawn"}
		return &enode{kind: kinds[g.next()%len(kinds)], kids: []*enode{g.gen(depth-1, inStr)}}
	case inStr:
		return g.field()
	default:
		n := &enode{kind: "interp", texts: []string{eTexts[g.next()%len(eTexts)]}}
		for parts := 1 + g.next()%3; parts > 0; parts-- {
			n.kids = append(n.kids, g.gen(depth-1, true))
			n.texts = append(n.texts, eTexts[g.next()%len(eTexts)])
		}
		return n
	}
}

func (g *egen) field() *enode {
	n := &enode{kind: "field", text: eFields[g.next()%len(eFields)]}
	if n.text == "sub" && g.next()%2 == 0 {
		return &enode{kind: "field", text: "k", kids: []*enode{n}}
	}
	return n
}

// write renders n; ergonomic picks the form.
func (n *enode) write(ergonomic bool) string {
	k := func(i int) string { return n.kids[i].write(ergonomic) }
	switch n.kind {
	case "atom":
		return n.text
	case "field":
		base := "h"
		if len(n.kids) > 0 {
			base = k(0)
		}
		if ergonomic {
			return base + "." + n.text
		}
		return base + `["` + n.text + `"]`
	case "and":
		if ergonomic {
			return "(" + k(0) + " && " + k(1) + ")"
		}
		return "(if (" + k(0) + ") { " + k(1) + " } else { false })"
	case "or":
		if ergonomic {
			return "(" + k(0) + " || " + k(1) + ")"
		}
		return "(if (" + k(0) + ") { true } else { " + k(1) + " })"
	case "bin":
		return "(" + k(0) + " " + n.text + " " + k(1) + ")"
	case "if":
		return "(if (" + k(0) + ") { " + k(1) + " } else { " + k(2) + " })"
	case "not":
		return "(!" + k(0) + ")"
	case "neg":
		return "(-" + k(0) + ")"
	case "tick":
		return "tick(" + k(0) + ")"
	case "spawn":
		return "(await spawn tick(" + k(0) + "))"
	}
	// interp
	var b strings.Builder
	if ergonomic {
		b.WriteByte('"')
		for i, kid := range n.kids {
			b.WriteString(n.texts[i] + "${" + kid.write(true) + "}")
		}
		b.WriteString(n.texts[len(n.kids)] + `"`)
		return b.String()
	}
	var parts []string
	for i, kid := range n.kids {
		if n.texts[i] != "" {
			parts = append(parts, `"`+n.texts[i]+`"`)
		}
		parts = append(parts, "str("+kid.write(false)+")")
	}
	if last := n.texts[len(n.kids)]; last != "" {
		parts = append(parts, `"`+last+`"`)
	}
	return "(" + strings.Join(parts, " + ") + ")"
}

// ePrelude binds the atoms. h's pairs are printed one at a time only (sub
// has one pair), so output never depends on map order.
const ePrelude = `let a = 5; let b = true; let s = "str"; let xs = [1, "q", [true]];` + "\n" +
	`let h = {"up": true, "n": 3, "name": "bob", "in": -2, "sub": {"k": 1}};` + "\n" +
	"let tick = fn(v) { puts(v); v };\n"

// eProgram is the whole program for one form.
func eProgram(n *enode, ergonomic bool) string {
	return ePrelude + "let r = " + n.write(ergonomic) + ";\nputs(r)"
}

// eOutcome is what one run gave, positions aside (the two sources differ).
type eOutcome struct {
	value, err, kind, out string
}

func eRun(src string, l runtime.Limits) eOutcome {
	var out bytes.Buffer
	v, err := Run(src, Options{Out: &out, Limits: l})
	o := eOutcome{out: out.String()}
	var ae *Error
	switch {
	case errors.As(err, &ae):
		o.err, o.kind = ae.Msg, ae.Kind
	case err != nil:
		o.err = "go: " + err.Error()
	default:
		o.value = v.Inspect()
	}
	return o
}

// eFit is the smallest limit, made by limit, under which src stops with
// no budget error: the exact steps or bytes src is charged (the root
// charges its last partial chunk when it closes).
func eFit(src string, limit func(int64) runtime.Limits) (int64, bool) {
	lo, hi := int64(1), int64(1<<24)
	if eRun(src, limit(hi)).kind == runtime.KindBudget {
		return 0, false
	}
	for lo < hi {
		mid := (lo + hi) / 2
		if eRun(src, limit(mid)).kind == runtime.KindBudget {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	return lo, true
}

func eSteps(n int64) runtime.Limits { return runtime.Limits{Steps: n} }
func eBytes(n int64) runtime.Limits { return runtime.Limits{AllocBytes: n} }
