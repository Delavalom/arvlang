package evaluator

import (
	"fmt"

	"github.com/delavalom/arvlang/arv/value"
)

// maxHostDepth bounds how deeply a host effect's result may nest. It also
// stops hostValue on a cyclic value, which a Go host can build and Arv
// cannot.
const maxHostDepth = 1000

// hostValue checks that v, a host effect's result (Phase 8.2), is Arv
// data: nil, integers, strings, booleans, and arrays and hashes of them,
// with string, integer or boolean keys. Anything else is refused: a
// builtin or function could be an effect no grant checks, a task handle
// or a capability value means nothing outside the run that made it, and
// Phase 6 can log data alone. It returns v with the host's booleans and
// nils replaced by TRUE, FALSE and NIL (== compares them by identity),
// copying an array or hash only if something in it was replaced, so the
// host's value is never changed. size is what the result costs the
// allocation budget, as allocSize counts a new builtin result, all the
// way down: its strings' bytes (hash keys too, which a host builds anew),
// 16 per array element and 48 per hash pair.
func hostValue(v value.Object) (out value.Object, size int64, err error) {
	return hostData(v, 0)
}

func hostData(v value.Object, depth int) (value.Object, int64, error) {
	if depth > maxHostDepth {
		return nil, 0, fmt.Errorf("result nested deeper than %d levels", maxHostDepth)
	}
	switch v := v.(type) {
	case nil:
		return NIL, 0, nil
	case *value.Nil:
		return NIL, 0, nil
	case *value.Boolean:
		if v == nil {
			return nil, 0, fmt.Errorf("result holds a nil %T", v)
		}
		return nativeBoolToBooleanObject(v.Value), 0, nil
	case *value.Integer:
		if v == nil {
			return nil, 0, fmt.Errorf("result holds a nil %T", v)
		}
		return v, 0, nil
	case *value.String:
		if v == nil {
			return nil, 0, fmt.Errorf("result holds a nil %T", v)
		}
		return v, int64(len(v.Value)), nil
	case *value.Array:
		if v == nil {
			return nil, 0, fmt.Errorf("result holds a nil %T", v)
		}
		return hostArray(v, depth)
	case *value.Hash:
		if v == nil {
			return nil, 0, fmt.Errorf("result holds a nil %T", v)
		}
		return hostHash(v, depth)
	}
	return nil, 0, fmt.Errorf("result holds a %s, which is not data", v.Type())
}

func hostArray(a *value.Array, depth int) (value.Object, int64, error) {
	size := arrayElemSize * int64(len(a.Elements))
	var copied []value.Object
	for i, el := range a.Elements {
		n, s, err := hostData(el, depth+1)
		if err != nil {
			return nil, 0, err
		}
		size += s
		if n != el && copied == nil {
			copied = make([]value.Object, len(a.Elements))
			copy(copied, a.Elements[:i])
		}
		if copied != nil {
			copied[i] = n
		}
	}
	if copied == nil {
		return a, size, nil
	}
	return &value.Array{Elements: copied}, size, nil
}

func hostHash(h *value.Hash, depth int) (value.Object, int64, error) {
	size := hashPairSize * int64(len(h.Pairs))
	var copied map[value.HashKey]value.HashPair
	for hk, p := range h.Pairs {
		key, ks, err := hostData(p.Key, depth+1)
		if err != nil {
			return nil, 0, fmt.Errorf("hash key: %w", err)
		}
		hashable, ok := key.(value.Hashable)
		if !ok || hashable.HashKey() != hk {
			return nil, 0, fmt.Errorf("result holds a hash whose key %s does not match its hash", key.Type())
		}
		val, vs, err := hostData(p.Value, depth+1)
		if err != nil {
			return nil, 0, err
		}
		size += ks + vs
		if copied == nil && (key != p.Key || val != p.Value) {
			copied = make(map[value.HashKey]value.HashPair, len(h.Pairs))
			for k, q := range h.Pairs { // pairs not reached yet are replaced below
				copied[k] = q
			}
		}
		if copied != nil {
			copied[hk] = value.HashPair{Key: key, Value: val}
		}
	}
	if copied == nil {
		return h, size, nil
	}
	return &value.Hash{Pairs: copied}, size, nil
}
