package evaluator

import (
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// "<${v}>" holds exactly the text puts prints for v.
func TestInterpolationRendersAsPuts(t *testing.T) {
	for _, name := range []string{"i", "s", "b", "n", "a", "h", "f"} {
		_, printed := runWith(t, values+"puts("+name+")")
		got, _ := runWith(t, values+`"<${`+name+`}>"`)
		s, ok := got.(*value.String)
		if !ok || s.Value != "<"+printed[:len(printed)-1]+">" {
			t.Errorf("<${%s}> = %s, puts printed %q", name, got.Inspect(), printed)
		}
	}
	tests := []struct{ input, want string }{
		{`let r = {"up": true, "status": 503}; "${r.status} up=${r.up}"`, "503 up=true"},
		{`let x = 2; "${x * 10 + 1}${x}"`, "212"},
		{`let ok = 1 < 2; "${if (ok) { 1 } else { 0 }}"`, "1"},
		{`"\${x} costs $5, {not} ${1}"`, "${x} costs $5, {not} 1"},
		{`"plain $ and { and }"`, "plain $ and { and }"},
		// a program's own str changes nothing
		{`let str = fn(x) { "mine" }; "${1}" + str(1)`, "1mine"},
	}
	for _, tt := range tests {
		if got, _ := runWith(t, tt.input); got.Inspect() != tt.want {
			t.Errorf("%s = %s, want %s", tt.input, got.Inspect(), tt.want)
		}
	}
}

// An interpolated string is the concatenation it stands for, so it costs
// exactly what that costs, in steps and bytes.
func TestInterpolationCostsWhatConcatenationCosts(t *testing.T) {
	sameCost(t, values+`"a ${i} b ${s}${a}!"`, values+`"a " + str(i) + " b " + str(s) + str(a) + "!"`)
	sameCost(t, values+`"${h}"`, values+`str(h)`)
	// The binding (48), str(12345) (5), "n=12345" (7) and "n=12345!" (8).
	if n := fit(t, `let x = 12345; "n=${x}!"`, bytesLimit); n != 48+5+7+8 {
		t.Errorf("costs %d bytes, want 68", n)
	}
}

// Errors point into the string: at the failing node inside ${...}, at the
// ${ whose value could not be rendered, or, for the concatenation, where
// the string starts.
func TestInterpolationErrorPositions(t *testing.T) {
	got, _ := runWith(t, "let a = 1;\n  \"v=${a + nope}\"")
	wantErrorAt(t, got, "identifier not found: nope", "2:12")
	got, _ = runWith(t, `"x ${1 / 0}"`)
	wantErrorAt(t, got, "division by zero", "1:6")
	got, _ = runWith(t, "\"line one\nline ${two}\"")
	wantErrorAt(t, got, "identifier not found: two", "2:8")

	// "[1, 2]" (6 bytes) does not fit after the array (32) and binding (48).
	got = runLimited(t, "let v = [1, 2];\n  \"x ${v}\"", runtime.Limits{AllocBytes: 85})
	wantErrorAt(t, got, "budget exceeded: alloc (limit 85 bytes)", "2:6")
	// "x" + "abc" (4 bytes) does not fit after the binding (48).
	got = runLimited(t, "let s = \"abc\";\n\"x${s}\"", runtime.Limits{AllocBytes: 51})
	wantErrorAt(t, got, "budget exceeded: alloc (limit 51 bytes)", "2:1")
}

// A concatenation's error sits at its leftmost operand, as for the
// hand-written form: the opening quote when the string starts with text,
// the $ of the first ${ when it starts with one.
func TestInterpolationConcatErrorAtLeftmostPiece(t *testing.T) {
	const msg = "budget exceeded: alloc (limit 51 bytes)"
	// "abc" + "abc" (6 bytes) does not fit after the binding (48).
	got := runLimited(t, "let s = \"abc\";\n\"${s}${s}\"", runtime.Limits{AllocBytes: 51})
	wantErrorAt(t, got, msg, "2:2")
	got = runLimited(t, "let s = \"abc\";\nstr(s) + str(s)", runtime.Limits{AllocBytes: 51})
	wantErrorAt(t, got, msg, "2:1")
	got = runLimited(t, "let s = \"abc\";\n\"a${s}${s}\"", runtime.Limits{AllocBytes: 51})
	wantErrorAt(t, got, msg, "2:1")
}

// The form an interpolation is charged as calls str even on a string, so
// a ${} of a string value costs 2 steps (the $str name and its call) more
// than + on the string itself; the bytes are the same.
func TestInterpolatingAStringCostsTheStrCall(t *testing.T) {
	const pre = `let s = "abc"; `
	plain, interp := fit(t, pre+`"x " + s + " y"`, stepsLimit), fit(t, pre+`"x ${s} y"`, stepsLimit)
	if interp != plain+2 {
		t.Errorf("steps: + takes %d, ${} takes %d, want 2 more", plain, interp)
	}
	two := fit(t, pre+`"x ${s}${s}"`, stepsLimit)
	if twoPlain := fit(t, pre+`"x " + s + s`, stepsLimit); two != twoPlain+4 {
		t.Errorf("steps: + takes %d, two ${} take %d, want 4 more", twoPlain, two)
	}
	if a, b := fit(t, pre+`"x " + s + " y"`, bytesLimit), fit(t, pre+`"x ${s} y"`, bytesLimit); a != b {
		t.Errorf("bytes: + takes %d, ${} takes %d", a, b)
	}
}

// A } inside a // comment in ${...} does not end the expression.
func TestInterpolationCommentBraceIsSkipped(t *testing.T) {
	got, _ := runWith(t, "let x = 1; \"${ x // }\n}\"")
	if got.Inspect() != "1" {
		t.Errorf("a } in a comment ended the ${: got %s", got.Inspect())
	}
}
