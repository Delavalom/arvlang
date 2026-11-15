package value

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"unicode/utf8"
)

// MaxCodecDepth bounds how deeply Encode nests JSON, and so how deeply
// Decode recurses. It is encoding/json's own limit, so whatever Encode
// writes, Decode reads back.
const MaxCodecDepth = 10_000

// RefMinLen is the length past which a string met again is encoded as a
// reference (see Encode). A shorter one is written out each time.
const RefMinLen = 16

// Encode is o as JSON, for the effect log (Phase 6): Decode gives back an
// equal value, byte for byte. It covers what effects return:
//
//	nil      null              integer  42 (a JSON number, always whole)
//	boolean  true / false      string   "text" if valid UTF-8, else {"bytes":"<base64>"}
//	array    [e1, e2]          hash     {"hash":[[k1, v1], [k2, v2]]} in key order
//	error    {"error":[message, kind, pos, task]}, each a string as above
//	shared   {"ref":n}
//
// A value can share parts: an array holding the same array twice, n
// levels deep, has n nodes but 2^n paths. So every array, hash, error and
// string of more than RefMinLen bytes is numbered, from 0, in the order
// the encoding meets it first, and each time it is met again it is
// written as {"ref":n}. The encoding then grows with the value's nodes
// and their elements, which the program was charged for, not with its
// paths. Decode shares them again, so a value's shape survives the log:
// what is shared in a recording is shared in its replay.
//
// Functions, builtins, tasks, capabilities and return values are never
// effect results; encoding one is a bug, and an error.
func Encode(o Object) ([]byte, error) {
	var e encoder
	if err := e.value(o, 0); err != nil {
		return nil, err
	}
	return e.b.Bytes(), nil
}

// EncodeRequest is an effect's arguments as a JSON array, for the log to
// compare a replayed request with the recorded one. Arguments need only
// compare, never decode: a value Encode refuses, or one nested past
// MaxCodecDepth, is written as {"type":"<TYPE>"}.
func EncodeRequest(args []Object) []byte {
	e := encoder{loose: true}
	e.b.WriteByte('[')
	for i, a := range args {
		if i > 0 {
			e.b.WriteByte(',')
		}
		e.value(a, 1) // loose: never fails
	}
	e.b.WriteByte(']')
	return e.b.Bytes()
}

type encoder struct {
	b     bytes.Buffer
	loose bool
	refs  map[Object]int // the shareable values written so far, by number
}

// shareable reports whether o is numbered for references (see Encode).
func shareable(o Object) bool {
	switch o := o.(type) {
	case *Array, *Hash, *Error:
		return true
	case *String:
		return len(o.Value) > RefMinLen
	}
	return false
}

func (e *encoder) value(o Object, depth int) error {
	if depth >= MaxCodecDepth {
		return e.refuse(o, "value nests deeper than %d levels", MaxCodecDepth)
	}
	if o != nil && shareable(o) {
		if n, ok := e.refs[o]; ok {
			e.b.WriteString(`{"ref":`)
			e.b.WriteString(strconv.Itoa(n))
			e.b.WriteByte('}')
			return nil
		}
		if e.refs == nil {
			e.refs = make(map[Object]int)
		}
		e.refs[o] = len(e.refs)
	}
	switch o := o.(type) {
	case *Nil:
		e.b.WriteString("null")
	case *Boolean:
		e.b.WriteString(strconv.FormatBool(o.Value))
	case *Integer:
		e.b.WriteString(strconv.FormatInt(o.Value, 10))
	case *String:
		e.str(o.Value)
	case *Array:
		e.b.WriteByte('[')
		for i, el := range o.Elements {
			if i > 0 {
				e.b.WriteByte(',')
			}
			if err := e.value(el, depth+1); err != nil {
				return err
			}
		}
		e.b.WriteByte(']')
	case *Hash:
		if depth+3 >= MaxCodecDepth { // {"hash":[[k, v]]} nests 3 levels
			return e.refuse(o, "value nests deeper than %d levels", MaxCodecDepth)
		}
		e.b.WriteString(`{"hash":[`)
		for i, k := range o.Keys() {
			if i > 0 {
				e.b.WriteByte(',')
			}
			p := o.Pairs[k]
			e.b.WriteByte('[')
			if err := e.value(p.Key, depth+3); err != nil {
				return err
			}
			e.b.WriteByte(',')
			if err := e.value(p.Value, depth+3); err != nil {
				return err
			}
			e.b.WriteByte(']')
		}
		e.b.WriteString(`]}`)
	case *Error:
		e.b.WriteString(`{"error":[`)
		for i, s := range []string{o.Message, o.Kind, o.Pos, o.Task} {
			if i > 0 {
				e.b.WriteByte(',')
			}
			e.str(s)
		}
		e.b.WriteString(`]}`)
	default:
		if o == nil {
			return e.refuse(o, "cannot encode a Go nil")
		}
		return e.refuse(o, "cannot encode %s", o.Type())
	}
	return nil
}

// refuse fails, or in a loose encoder writes o's type and carries on.
func (e *encoder) refuse(o Object, format string, a ...any) error {
	if !e.loose {
		return fmt.Errorf("value: "+format, a...)
	}
	t := ObjectType("NONE")
	if o != nil {
		t = o.Type()
	}
	e.b.WriteString(`{"type":`)
	e.str(string(t))
	e.b.WriteByte('}')
	return nil
}

const hexDigits = "0123456789abcdef"

// str writes s as a JSON string, or as {"bytes":...} if it is not valid
// UTF-8: JSON text is UTF-8, and Go's encoder would replace the bad bytes.
func (e *encoder) str(s string) {
	if !utf8.ValidString(s) {
		e.b.WriteString(`{"bytes":"`)
		e.b.WriteString(base64.StdEncoding.EncodeToString([]byte(s)))
		e.b.WriteString(`"}`)
		return
	}
	e.b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '"' || c == '\\':
			e.b.WriteByte('\\')
			e.b.WriteByte(c)
		case c < 0x20:
			e.b.WriteString(`\u00`)
			e.b.WriteByte(hexDigits[c>>4])
			e.b.WriteByte(hexDigits[c&0xf])
		default:
			e.b.WriteByte(c)
		}
	}
	e.b.WriteByte('"')
}

// Decode reads what Encode wrote. Anything else, including trailing data,
// a float, a hash key that is not an integer, string or boolean, or a
// repeated hash key, is an error; Decode never panics on any input.
func Decode(b []byte) (Object, error) {
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	var v any
	if err := d.Decode(&v); err != nil {
		return nil, fmt.Errorf("value: %w", err)
	}
	if _, err := d.Token(); err != io.EOF {
		return nil, errors.New("value: data after the value")
	}
	var d2 decoder
	return d2.value(v)
}

// decoder reads what an encoder wrote, numbering shareable values in the
// same order (see Encode).
type decoder struct {
	table []Object // by number; nil while a value is still being read
}

// reserve numbers the shareable value about to be read.
func (d *decoder) reserve() int {
	d.table = append(d.table, nil)
	return len(d.table) - 1
}

// str numbers s if it is shareable, and returns it as a String.
func (d *decoder) str(s string) Object {
	o := &String{Value: s}
	if len(s) > RefMinLen {
		d.table = append(d.table, o)
	}
	return o
}

func (d *decoder) ref(raw any) (Object, error) {
	n, ok := raw.(json.Number)
	if !ok {
		return nil, errors.New(`value: "ref" must be a number`)
	}
	i, err := strconv.ParseInt(string(n), 10, 64)
	if err != nil || i < 0 || i >= int64(len(d.table)) || d.table[i] == nil {
		return nil, fmt.Errorf("value: ref %s names no value read before it", n)
	}
	return d.table[i], nil
}

func (d *decoder) value(v any) (Object, error) {
	switch v := v.(type) {
	case nil:
		return &Nil{}, nil
	case bool:
		return &Boolean{Value: v}, nil
	case json.Number:
		n, err := strconv.ParseInt(string(v), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("value: %s is not a whole number that fits in 64 bits", v)
		}
		return &Integer{Value: n}, nil
	case string:
		return d.str(v), nil
	case []any:
		slot := d.reserve()
		els := make([]Object, len(v))
		for i, el := range v {
			o, err := d.value(el)
			if err != nil {
				return nil, err
			}
			els[i] = o
		}
		a := &Array{Elements: els}
		d.table[slot] = a
		return a, nil
	case map[string]any:
		if len(v) != 1 {
			return nil, fmt.Errorf("value: an object must have exactly one of bytes, hash, error, ref; got %d keys", len(v))
		}
		if raw, ok := v["bytes"]; ok {
			s, err := bytesString(raw)
			if err != nil {
				return nil, err
			}
			return d.str(s), nil
		}
		if raw, ok := v["hash"]; ok {
			slot := d.reserve()
			h, err := d.hash(raw)
			if err != nil {
				return nil, err
			}
			d.table[slot] = h
			return h, nil
		}
		if raw, ok := v["error"]; ok {
			slot := d.reserve()
			e, err := errorFromJSON(raw)
			if err != nil {
				return nil, err
			}
			d.table[slot] = e
			return e, nil
		}
		if raw, ok := v["ref"]; ok {
			return d.ref(raw)
		}
	}
	return nil, fmt.Errorf("value: cannot decode %T", v)
}

func bytesString(raw any) (string, error) {
	s, ok := raw.(string)
	if !ok {
		return "", errors.New(`value: "bytes" must be a base64 string`)
	}
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return "", fmt.Errorf(`value: "bytes": %w`, err)
	}
	return string(b), nil
}

// stringFromJSON is one string as str wrote it.
func stringFromJSON(raw any) (string, error) {
	if s, ok := raw.(string); ok {
		return s, nil
	}
	if m, ok := raw.(map[string]any); ok && len(m) == 1 {
		if b, ok := m["bytes"]; ok {
			return bytesString(b)
		}
	}
	return "", errors.New("value: want a string")
}

func (d *decoder) hash(raw any) (Object, error) {
	pairs, ok := raw.([]any)
	if !ok {
		return nil, errors.New(`value: "hash" must be an array of [key, value] pairs`)
	}
	h := NewHashSize(len(pairs))
	for _, p := range pairs {
		kv, ok := p.([]any)
		if !ok || len(kv) != 2 {
			return nil, errors.New(`value: "hash" must be an array of [key, value] pairs`)
		}
		k, err := d.value(kv[0])
		if err != nil {
			return nil, err
		}
		hk, ok := k.(Hashable)
		if !ok {
			return nil, fmt.Errorf("value: %s cannot be a hash key", k.Type())
		}
		if _, dup := h.Pairs[hk.HashKey()]; dup {
			return nil, fmt.Errorf("value: hash key %s appears twice", k.Inspect())
		}
		val, err := d.value(kv[1])
		if err != nil {
			return nil, err
		}
		h.Set(hk.HashKey(), HashPair{Key: k, Value: val})
	}
	return h, nil
}

func errorFromJSON(raw any) (Object, error) {
	fields, ok := raw.([]any)
	if !ok || len(fields) != 4 {
		return nil, errors.New(`value: "error" must be [message, kind, pos, task]`)
	}
	var s [4]string
	for i, f := range fields {
		str, err := stringFromJSON(f)
		if err != nil {
			return nil, err
		}
		s[i] = str
	}
	return &Error{Message: s[0], Kind: s[1], Pos: s[2], Task: s[3]}, nil
}
