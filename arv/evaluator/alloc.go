package evaluator

import "github.com/delavalom/arvlang/arv/value"

// Rough Go sizes (measured on darwin/arm64, Go 1.26) of what evaluation
// creates, for the allocation budget (alloc_mb). It counts bytes created
// and never refunds them, so it bounds churn, not live memory (see
// 01-design.md, section 6); but everything a program can keep alive is
// charged when created, so live memory stays within a small constant
// factor of the limit. String literals cost nothing: they share the
// program's source. Integers (8 bytes) are not charged: one is kept alive
// only by a charged array element or binding, at least twice its size.
const (
	arrayElemSize  = 16  // one interface value
	hashPairSize   = 48  // key and value interfaces plus map overhead
	envSize        = 320 // a new environment, its map and first slot group
	bindingSize    = 48  // one name bound in an environment
	functionSize   = 128 // a closure
	handleSize     = 160 // a task handle and its channel
	capabilitySize = 32  // a capability value: its kind and scope headers
)

// alloc charges n bytes for v, a value just created, to the innermost
// scope's allocation budget, and returns v or the budget's error.
func (in *Interpreter) alloc(v value.Object, n int64) value.Object {
	if e := in.charge(n); e != nil {
		return e
	}
	return v
}

// charge charges n bytes about to be allocated to the innermost scope's
// allocation budget.
func (in *Interpreter) charge(n int64) *value.Error {
	if err := in.chargeBytes(n); err != nil {
		return newError("%s", err)
	}
	return nil
}

// chargeBytes is charge with the budget's own error: the charge function
// a ChargingFunction builtin gets.
func (in *Interpreter) chargeBytes(n int64) error {
	if n == 0 || len(in.scopes) == 0 {
		return nil
	}
	return in.top().budget.Alloc(n)
}

// allocSize is what a builtin's new result costs: the bytes of a string,
// 16 per array element (the elements existed before), and 48 per hash
// pair plus the bytes of its string values, since a builtin's record, like
// http_get's {status, body}, is new throughout. A capability holds its
// scope, which net() builds anew (normalised), so it costs the scope's
// bytes too.
func allocSize(v value.Object) int64 {
	switch v := v.(type) {
	case *value.String:
		return int64(len(v.Value))
	case *value.Array:
		return arrayElemSize * int64(len(v.Elements))
	case *value.Hash:
		n := hashPairSize * int64(len(v.Pairs))
		for _, p := range v.Pairs {
			if s, ok := p.Value.(*value.String); ok {
				n += int64(len(s.Value))
			}
		}
		return n
	case *value.Capability:
		return capabilitySize + int64(len(v.Cap.Scope))
	}
	return 0
}

// concatSize is the bytes left op right builds when it concatenates two
// strings, so the evaluator can charge them before building the result.
func concatSize(op string, left, right value.Object) (int64, bool) {
	l, ok := left.(*value.String)
	if !ok || op != "+" {
		return 0, false
	}
	r, ok := right.(*value.String)
	if !ok {
		return 0, false
	}
	return int64(len(l.Value)) + int64(len(r.Value)), true
}
