package value

// Small integers are shared, not allocated: an Integer is immutable, and
// nothing tells two equal integers apart (== compares their values, a
// hash key is the value, and the codec never numbers integers). Phase 8.4:
// arithmetic, comparisons' operands and loop counters are mostly small.
const (
	smallIntMin = -128
	smallIntMax = 1023
)

var smallInts = func() []Integer {
	s := make([]Integer, smallIntMax-smallIntMin+1)
	for i := range s {
		s[i].Value = int64(i + smallIntMin)
	}
	return s
}()

// Int is n as an Integer: a shared one when n is small, else a new one.
func Int(n int64) *Integer {
	if n >= smallIntMin && n <= smallIntMax {
		return &smallInts[n-smallIntMin]
	}
	return &Integer{Value: n}
}
