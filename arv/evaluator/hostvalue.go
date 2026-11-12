package evaluator

import (
	"errors"
	"fmt"

	"github.com/delavalom/arvlang/arv/value"
)

// maxHostDepth bounds how deeply a host effect's result may nest. It also
// stops hostValue on a cyclic value, which a Go host can build and Arv
// cannot.
const maxHostDepth = 1000

// hostCheckEvery is how many nodes hostWalk visits between checks, and
// hostCheckBytes how many bytes it may count before it checks anyway.
const (
	hostCheckEvery = 1024
	hostCheckBytes = 64 << 10
)

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
//
// A result can share sub-values: Fn may return the program's own DAG
// (an array holding the same array twice, n levels deep), which has 2^n
// paths but n nodes. Each string, array and hash is walked and counted
// once, and a shared one that needed a copy is copied once, so the walk
// costs what the nodes hold, not what their paths would.
func hostValue(v value.Object) (out value.Object, size int64, err error) {
	w := hostWalk{}
	out, _, err = w.data(v, 0)
	return out, w.size, err
}

// hostWalk is one walk of a host result. check, unless nil, is called
// with the bytes counted since its last call, every hostCheckEvery nodes
// or hostCheckBytes bytes and once at the end; an error from it stops the
// walk (stop holds it).
type hostWalk struct {
	seen    map[value.Object]hostSeen       // strings, arrays and hashes walked
	keys    map[*value.String]value.HashKey // string hash keys hashed
	size    int64
	pending int64
	visits  int
	hashed  int64 // bytes of string keys hashed; read only by tests, nothing checks or charges it
	check   func(int64) *value.Error
	stop    *value.Error
}

// hostSeen is a node already walked: what it became, and how deeply it
// nests (0 for a string), so a node met again further down still keeps
// the result within maxHostDepth.
type hostSeen struct {
	out    value.Object
	height int
}

var errHostStopped = errors.New("stopped")

// count adds n bytes, and runs check when it is due.
func (w *hostWalk) count(n int64) error {
	w.size += n
	w.pending += n
	w.visits++
	if w.check != nil && (w.visits%hostCheckEvery == 0 || w.pending >= hostCheckBytes) {
		return w.settle()
	}
	return nil
}

// settle passes the bytes counted so far to check.
func (w *hostWalk) settle() error {
	if w.check == nil || w.stop != nil {
		return nil
	}
	n := w.pending
	w.pending = 0
	if e := w.check(n); e != nil {
		w.stop = e
		return errHostStopped
	}
	return nil
}

// remembered returns a node already walked, if v is one, refusing it if
// it would nest too deeply where it is met now.
func (w *hostWalk) remembered(v value.Object, depth int) (hostSeen, bool, error) {
	s, ok := w.seen[v]
	if ok && depth+s.height > maxHostDepth {
		return s, true, fmt.Errorf("result nested deeper than %d levels", maxHostDepth)
	}
	return s, ok, nil
}

// remember records a node just walked, depth levels down. The result
// itself (depth 0) is met only once, so a result that is a lone string or
// an array of numbers allocates no map.
func (w *hostWalk) remember(v, out value.Object, height, depth int) {
	if depth == 0 {
		return
	}
	if w.seen == nil {
		w.seen = make(map[value.Object]hostSeen)
	}
	w.seen[v] = hostSeen{out: out, height: height}
}

// data walks v, depth levels down, and returns what it becomes and how
// deeply it nests.
func (w *hostWalk) data(v value.Object, depth int) (value.Object, int, error) {
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
		if _, ok := w.seen[v]; ok {
			return v, 0, nil
		}
		w.remember(v, v, 0, depth)
		return v, 0, w.count(int64(len(v.Value)))
	case *value.Array, *value.Hash:
		if s, ok, err := w.remembered(v, depth); ok || err != nil {
			return s.out, s.height, err
		}
		var out value.Object
		var height int
		var err error
		switch v := v.(type) {
		case *value.Array:
			if v == nil {
				return nil, 0, fmt.Errorf("result holds a nil %T", v)
			}
			out, height, err = w.array(v, depth)
		case *value.Hash:
			if v == nil {
				return nil, 0, fmt.Errorf("result holds a nil %T", v)
			}
			out, height, err = w.hash(v, depth)
		}
		if err != nil {
			return nil, 0, err
		}
		w.remember(v, out, height, depth)
		return out, height, nil
	}
	return nil, 0, fmt.Errorf("result holds a %s, which is not data", v.Type())
}

func (w *hostWalk) array(a *value.Array, depth int) (value.Object, int, error) {
	if err := w.count(arrayElemSize * int64(len(a.Elements))); err != nil {
		return nil, 0, err
	}
	height := 0
	var copied []value.Object
	for i, el := range a.Elements {
		n, h, err := w.data(el, depth+1)
		if err != nil {
			return nil, 0, err
		}
		height = max(height, h+1)
		if n != el && copied == nil {
			copied = make([]value.Object, len(a.Elements))
			copy(copied, a.Elements[:i])
		}
		if copied != nil {
			copied[i] = n
		}
	}
	if copied == nil {
		return a, height, nil
	}
	return &value.Array{Elements: copied}, height, nil
}

func (w *hostWalk) hash(h *value.Hash, depth int) (value.Object, int, error) {
	if err := w.count(hashPairSize * int64(len(h.Pairs))); err != nil {
		return nil, 0, err
	}
	height := 0
	var copied map[value.HashKey]value.HashPair
	for hk, p := range h.Pairs {
		key, _, err := w.data(p.Key, depth+1)
		if err != nil {
			if err == errHostStopped {
				return nil, 0, err
			}
			return nil, 0, fmt.Errorf("hash key: %w", err)
		}
		if got, ok := w.keyHash(key); !ok || got != hk {
			return nil, 0, fmt.Errorf("result holds a hash whose key %s does not match its hash", key.Type())
		}
		val, vh, err := w.data(p.Value, depth+1)
		if err != nil {
			return nil, 0, err
		}
		height = max(height, vh+1)
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
		return h, height, nil
	}
	return &value.Hash{Pairs: copied}, height, nil
}

// keyHash returns key's HashKey, if key can be one. A string key's is
// remembered: String.HashKey reads the whole string, and a key shared by
// many hashes is counted once (data remembers it), so hashing it again
// for each hash would be work that grows with its length and that
// neither the charge nor the checks see.
func (w *hostWalk) keyHash(key value.Object) (value.HashKey, bool) {
	hashable, ok := key.(value.Hashable)
	if !ok {
		return value.HashKey{}, false
	}
	s, ok := key.(*value.String)
	if !ok {
		return hashable.HashKey(), true // an integer's or boolean's costs nothing
	}
	if hk, ok := w.keys[s]; ok {
		return hk, true
	}
	w.hashed += int64(len(s.Value))
	hk := s.HashKey()
	if w.keys == nil {
		w.keys = make(map[*value.String]value.HashKey)
	}
	w.keys[s] = hk
	return hk, true
}
