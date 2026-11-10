package evaluator

import "github.com/delavalom/arvlang/arv/value"

// Rough Go sizes of Arv values, for the allocation budget (alloc_mb). It
// counts bytes of values created and never refunds them, so it bounds
// churn, not live memory (see 01-design.md, section 6). String literals
// cost nothing: they share the program's source.
const (
	arrayElemSize = 16 // one interface value
	hashPairSize  = 48 // key and value interfaces plus map overhead
)

// alloc charges n bytes for v, a value just created, to the innermost
// scope's allocation budget, and returns v or the budget's error.
func (in *Interpreter) alloc(v value.Object, n int64) value.Object {
	if n == 0 || len(in.scopes) == 0 {
		return v
	}
	if err := in.top().budget.Alloc(n); err != nil {
		return newError("%s", err)
	}
	return v
}

// allocSize is what a builtin's new result costs: the bytes of a string,
// 16 per array element (the elements existed before), and 48 per hash
// pair plus the bytes of its string values, since a builtin's record, like
// http_get's {status, body}, is new throughout.
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
	}
	return 0
}
