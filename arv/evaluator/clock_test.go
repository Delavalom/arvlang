package evaluator

import (
	"testing"

	"github.com/delavalom/arvlang/arv/value"
)

// clock_ms measures the time between two readings.
func TestClockMSMeasuresElapsedTime(t *testing.T) {
	got, _ := runWith(t, `let a = clock_ms(); sleep(30); let b = clock_ms(); [a, b - a]`)
	arr, ok := got.(*value.Array)
	if !ok || len(arr.Elements) != 2 {
		t.Fatalf("got %s, want [a, b - a]", got.Inspect())
	}
	a := arr.Elements[0].(*value.Integer).Value
	d := arr.Elements[1].(*value.Integer).Value
	if a < 0 {
		t.Errorf("clock_ms() = %d, want >= 0", a)
	}
	// sleep(30) waits at least 30 ms; the upper bound is generous for a
	// loaded machine.
	if d < 30 || d > 30000 {
		t.Errorf("clock_ms() after sleep(30) advanced %d ms, want 30 to 30000", d)
	}
}

func TestClockMSTakesNoArguments(t *testing.T) {
	got, _ := runWith(t, `clock_ms(1)`)
	if !isError(got) || got.(*value.Error).Message != "clock_ms: want 0 arguments, got 1" {
		t.Errorf("clock_ms(1) = %s", got.Inspect())
	}
}
