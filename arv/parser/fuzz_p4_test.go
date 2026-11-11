package parser

import (
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/ast"
	"github.com/delavalom/arvlang/arv/lexer"
	"github.com/delavalom/arvlang/arv/token"
)

// The Phase 4 syntax fuzz targets compose a match or a scope with options
// from fuzzed parts, one part per line. When every part parses on its own
// (an expression, a name, a block body) the whole must parse to exactly
// those parts; whatever the parts, parsing must not panic, must be
// deterministic and must give every error a position inside the input.

// checkErrors applies FuzzParser's checks on errors to one input.
func checkErrors(t *testing.T, input string) (*ast.Program, []string) {
	t.Helper()
	prog, errs := fuzzParse(input)
	if _, errs2 := fuzzParse(input); !reflect.DeepEqual(errs, errs2) {
		t.Fatalf("two parses of %q gave different errors: %q and %q", input, errs, errs2)
	}
	lines := strings.Count(input, "\n") + 1
	for _, e := range errs {
		if strings.HasPrefix(e, "nesting deeper than ") {
			continue
		}
		m := atPos.FindStringSubmatch(e)
		if m == nil {
			t.Fatalf("error %q for %q has no (at line:col)", e, input)
		}
		line, _ := strconv.Atoi(m[1])
		col, _ := strconv.Atoi(m[2])
		if line < 1 || line > lines || col < 1 || col > len(input)+1 {
			t.Fatalf("error %q is outside the %d lines of %q", e, lines, input)
		}
	}
	if len(errs) == 0 {
		fuzzParse(prog.String())
	}
	return prog, errs
}

func tooDeep(errs []string) bool {
	for _, e := range errs {
		if strings.HasPrefix(e, "nesting deeper than ") {
			return true
		}
	}
	return false
}

type tok struct {
	typ token.TokenType
	lit string
}

func lexAll(s string) []tok {
	l := lexer.New(s)
	var out []tok
	for {
		tk := l.NextToken()
		if tk.Type == token.EOF {
			return out
		}
		out = append(out, tok{tk.Type, tk.Literal})
	}
}

// closed reports whether s lexes the same whatever follows it on the next
// line (no unterminated string or block, no NUL ending the input early)
// and has no semicolon outside brackets, so it can be placed inside a
// larger construct unchanged.
func closed(s string) bool {
	toks := lexAll(s)
	after := lexAll(s + "\n)")
	if len(after) != len(toks)+1 || !reflect.DeepEqual(after[:len(toks)], toks) ||
		after[len(toks)] != (tok{token.RPAREN, ")"}) {
		return false
	}
	depth := 0
	for _, tk := range toks {
		switch tk.typ {
		case token.LPAREN, token.LBRACE, token.LBRACKET:
			depth++
		case token.RPAREN, token.RBRACE, token.RBRACKET:
			depth--
		case token.SEMICOLON:
			if depth <= 0 {
				return false
			}
		}
		if depth < 0 {
			return false
		}
	}
	return true
}

// alone parses s as a lone expression; ok is false unless it is closed and
// parses, with no error, to exactly one expression statement.
func alone(s string) (ast.Expression, bool) {
	if !closed(s) {
		return nil, false
	}
	prog, errs := fuzzParse(s)
	if len(errs) > 0 || len(prog.Statements) != 1 {
		return nil, false
	}
	es, ok := prog.Statements[0].(*ast.ExpressionStatement)
	if !ok || es.Expression == nil {
		return nil, false
	}
	return es.Expression, true
}

// isName reports whether s is exactly one identifier token.
func isName(s string) bool {
	toks := lexAll(s)
	return len(toks) == 1 && toks[0].typ == token.IDENT && toks[0].lit == s
}

// FuzzMatchSyntax composes
//
//	match <subject>
//	{
//	ok(<okName>) =>
//	<okBody>
//	,
//	err(<errName>) =>
//	<errBody>
//	,?
//	}
//
// with the arms in either order. If every part is valid alone, the match
// parses to those parts; if a name is not one identifier, it is an error.
func FuzzMatchSyntax(f *testing.F) {
	f.Add("await t", "v", "v", "e", `e["kind"]`, false, false)
	f.Add("5", "v", "v + 1", "e", "0", true, true)
	f.Add("match x { ok(a) => a, err(b) => b }", "ok", "ok", "err", "err", false, true)
	f.Add("scope (supervise: \"collect\") { 1 }", "v", "if (v) { 1 } else { 2 }", "e", "fn(x) { x; x }", false, false)
	f.Add(`getenv("X")`, "v", `match v { ok(w) => w, err(f) => f }`, "e", `{"k": [e]}`, true, false)
	f.Add("x", "fn", "1", "e", "2", false, false)
	f.Add("x", "v", "1;", "e", "2", false, false)
	f.Add(`"abc`, "v", "1", "e", "2", false, false)
	f.Add("1 /* c", "v", "1 // c", "e", "2", false, false)
	f.Add("", "", "", "", "", false, false)
	f.Fuzz(func(t *testing.T, subject, okName, okBody, errName, errBody string, errFirst, comma bool) {
		okArm := "ok(" + okName + ") =>\n" + okBody + "\n"
		errArm := "err(" + errName + ") =>\n" + errBody + "\n"
		arms := okArm + ",\n" + errArm
		if errFirst {
			arms = errArm + ",\n" + okArm
		}
		if comma {
			arms += ",\n"
		}
		input := "match " + subject + "\n{\n" + arms + "}"
		prog, errs := checkErrors(t, input)

		namesOK := isName(okName) && isName(errName)
		if !namesOK && len(errs) == 0 {
			t.Fatalf("names %q and %q are not both identifiers, but %q parsed", okName, errName, input)
		}
		s, ok1 := alone(subject)
		ob, ok2 := alone(okBody)
		eb, ok3 := alone(errBody)
		if !namesOK || !ok1 || !ok2 || !ok3 || tooDeep(errs) {
			return
		}
		if len(errs) > 0 {
			t.Fatalf("every part parses alone, but %q gave %q", input, errs)
		}
		if len(prog.Statements) != 1 {
			t.Fatalf("%q parsed to %d statements", input, len(prog.Statements))
		}
		es, _ := prog.Statements[0].(*ast.ExpressionStatement)
		me, ok := es.Expression.(*ast.MatchExpression)
		if !ok {
			t.Fatalf("%q parsed to %T, want a match", input, es.Expression)
		}
		if me.Subject.String() != s.String() || me.Ok.String() != ob.String() || me.Err.String() != eb.String() ||
			me.OkName.Value != okName || me.ErrName.Value != errName {
			t.Fatalf("%q parsed to %s; want subject %s, ok(%s) %s, err(%s) %s",
				input, me, s, okName, ob, errName, eb)
		}
	})
}

// FuzzScopeOptionsSyntax composes scope (<name>: <value>, ...) { <body> }
// from spec, alternating names and values separated by NUL bytes (NUL ends
// a program, so it never appears in one), one part per line; with bare
// and no options it is scope { <body> }. If every part is valid alone, the
// scope parses to those options, in order, and that body.
func FuzzScopeOptionsSyntax(f *testing.F) {
	f.Add("supervise\x00\"collect\"", "spawn f(); 1", false)
	f.Add("supervise\x00\"restart\"\x00retries\x002\x00backoff_ms\x001\x00retry_on\x00[\"effect\", \"user\"]", "await spawn f()", false)
	f.Add("timeout_ms\x002000\x00tasks\x00n + 1\x00steps\x00scope (steps: 1) { 2 }", "let x = 1; x", false)
	f.Add("supervise\x00match x { ok(v) => v, err(e) => \"collect\" }", "", false)
	f.Add("", "1", true)
	f.Add("", "1", false)
	f.Add("fn\x001", "1", false)
	f.Add("a b\x001", "1", false)
	f.Add("supervise\x00\"collect\";", "1", false)
	f.Add("supervise\x00\"collect", "1", false)
	f.Add("x\x001", "}", false)
	f.Add("x", "1", false)
	f.Fuzz(func(t *testing.T, spec, body string, bare bool) {
		parts := strings.Split(spec, "\x00")
		if spec == "" {
			parts = nil
		}
		if len(parts)%2 == 1 {
			parts = parts[:len(parts)-1]
		}
		if len(parts) > 16 {
			parts = parts[:16]
		}
		var b strings.Builder
		b.WriteString("scope ")
		if len(parts) > 0 || !bare {
			b.WriteString("(\n")
			for i := 0; i < len(parts); i += 2 {
				if i > 0 {
					b.WriteString(",\n")
				}
				b.WriteString(parts[i] + "\n:\n" + parts[i+1] + "\n")
			}
			b.WriteString(")\n")
		}
		b.WriteString("{\n" + body + "\n}")
		input := b.String()
		prog, errs := checkErrors(t, input)

		namesOK := true
		values := make([]ast.Expression, 0, len(parts)/2)
		valuesOK := true
		for i := 0; i < len(parts); i += 2 {
			namesOK = namesOK && isName(parts[i])
			v, ok := alone(parts[i+1])
			valuesOK = valuesOK && ok
			values = append(values, v)
		}
		if !namesOK && len(errs) == 0 {
			t.Fatalf("an option name in %q is not an identifier, but %q parsed", parts, input)
		}
		bodyProg, bodyErrs := fuzzParse(body)
		if !namesOK || !valuesOK || !closed(body) || len(bodyErrs) > 0 || tooDeep(errs) {
			return
		}
		if len(errs) > 0 {
			t.Fatalf("every part parses alone, but %q gave %q", input, errs)
		}
		if len(prog.Statements) != 1 {
			t.Fatalf("%q parsed to %d statements", input, len(prog.Statements))
		}
		es, _ := prog.Statements[0].(*ast.ExpressionStatement)
		sc, ok := es.Expression.(*ast.ScopeExpression)
		if !ok {
			t.Fatalf("%q parsed to %T, want a scope", input, es.Expression)
		}
		if bare && len(parts) == 0 {
			if sc.Options != nil {
				t.Fatalf("%q: options %v, want none", input, sc.Options)
			}
		} else if sc.Options == nil || len(sc.Options) != len(values) {
			t.Fatalf("%q: %d options, want %d", input, len(sc.Options), len(values))
		}
		for i, o := range sc.Options {
			if o.Name != parts[2*i] || o.Value.String() != values[i].String() {
				t.Fatalf("%q: option %d is %s: %s, want %s: %s", input, i, o.Name, o.Value, parts[2*i], values[i])
			}
		}
		if sc.Body.String() != bodyProg.String() {
			t.Fatalf("%q: body %q, want %q", input, sc.Body.String(), bodyProg.String())
		}
	})
}
