package evaluator

import "testing"

func TestForComprehension(t *testing.T) {
	tests := []struct{ input, want string }{
		{`for x in [1, 2, 3] { x * 2 }`, `[2, 4, 6]`},
		{`for x in [] { x }`, `[]`},
		{`let xs = ["a", "b"]; for x in xs { x + "!" }`, `[a!, b!]`},
		{`for x in [1, 2] { let y = x; }`, `[nil, nil]`},
		{`for x in [[1, 2], [3]] { for y in x { y + 10 } }`, `[[11, 12], [13]]`},
		// each iteration has its own environment: closures keep their own x
		{`let fs = for x in [1, 2] { fn() { x } }; fs[0]() + fs[1]()`, `3`},
		// the loop variable and body lets do not leak
		{`let x = 10; for x in [1] { x }; x`, `10`},
		// return leaves the enclosing function, not just the loop
		{`let f = fn() { for x in [1, 2, 3] { if (x == 2) { return 20 } x } }; f()`, `20`},
	}
	for _, tt := range tests {
		got, _ := runWith(t, tt.input)
		if got.Inspect() != tt.want {
			t.Errorf("%s\n  = %s, want %s", tt.input, got.Inspect(), tt.want)
		}
	}
}

func TestForErrors(t *testing.T) {
	got, _ := runWith(t, "let n = 5;\n  for x in n { x }")
	wantErrorAt(t, got, "for: want ARRAY, got INTEGER", "2:3")

	got, _ = runWith(t, `for x in [1] { let y = x }; y`)
	wantErrorAt(t, got, "identifier not found: y", "1:29")

	// the first error stops the loop
	got, out := runWith(t, "for x in [1, 0, 2] {\n puts(x); 10 / x }")
	wantErrorAt(t, got, "division by zero", "2:11")
	if out != "1\n0\n" {
		t.Fatalf("output = %q, want the loop to stop at the error", out)
	}
}

// for is how fan-out is written now: one spawn per element.
func TestForSpawnsOneTaskPerElement(t *testing.T) {
	got, _ := runWith(t, `
		let double = fn(n) { n * 2 };
		scope {
			let ts = for i in [1, 2, 3] { spawn double(i) };
			for t in ts { await t }
		}`)
	if got.Inspect() != "[2, 4, 6]" {
		t.Fatalf("got %s", got.Inspect())
	}
}
