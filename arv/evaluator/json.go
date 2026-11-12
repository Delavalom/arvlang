package evaluator

import (
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

func init() {
	builtins["json"] = &value.Builtin{Charging: jsonEncode}
}

// json(v) is v as compact JSON text: integers, strings, booleans, nil
// (null), arrays and hashes with STRING keys, written in key order (as
// Go's encoding/json writes a map), so the same value always gives the
// same text. Invalid UTF-8 becomes U+FFFD. Any other value is an error.
//
// A value can encode far larger than it is (an array holding the same
// sub-array twice, n levels deep), so the text is charged to alloc_mb
// before it is written, value.InspectChunk bytes at a time: at most one
// chunk is ever uncharged. Between chunks it stops if the task has been
// cancelled, so a huge encoding under no alloc limit still ends at the
// scope's deadline.
func jsonEncode(t *runtime.Task, charge func(int64) error, args ...value.Object) value.Object {
	if len(args) != 1 {
		return newError("json: want 1 argument, got %d", len(args))
	}
	w := jsonWriter{check: func(n int64) *value.Error {
		if err := t.Err(); err != nil {
			return newErrorKind(runtime.KindCancelled, "task %s cancelled: %s", t.ID, err)
		}
		if err := charge(n); err != nil {
			return errorOf(err, runtime.KindBudget)
		}
		return nil
	}}
	w.value(args[0], 0)
	w.settle()
	if w.err != nil {
		return w.err
	}
	return &value.String{Value: w.text()}
}

// jsonFirstChunk is the size of jsonWriter's first chunk; each next one
// doubles, up to value.InspectChunk.
const jsonFirstChunk = 64

// jsonWriter writes into chunks and copies them once, at the end, into a
// string of exactly the text's size: a held result keeps no slack, and an
// encoding allocates about twice its text (a strings.Builder grows by a
// quarter at a time once large, which allocated about 5 times).
type jsonWriter struct {
	chunks  [][]byte
	size    int // bytes written
	check   func(n int64) *value.Error
	pending int          // bytes accepted but not yet passed to check
	err     *value.Error // the first error; writing has stopped
}

func (w *jsonWriter) write(s string) {
	for len(s) > 0 {
		last := len(w.chunks) - 1
		if last < 0 || len(w.chunks[last]) == cap(w.chunks[last]) {
			size := jsonFirstChunk
			if last >= 0 {
				size = min(2*cap(w.chunks[last]), value.InspectChunk)
			}
			w.chunks = append(w.chunks, make([]byte, 0, size))
			last++
		}
		c := w.chunks[last]
		n := copy(c[len(c):cap(c)], s)
		w.chunks[last] = c[:len(c)+n]
		w.size += n
		s = s[n:]
	}
}

func (w *jsonWriter) text() string {
	var sb strings.Builder
	sb.Grow(w.size)
	for _, c := range w.chunks {
		sb.Write(c)
	}
	return sb.String()
}

// put appends s, first passing check the bytes pending once they reach
// value.InspectChunk.
func (w *jsonWriter) put(s string) {
	if w.err != nil {
		return
	}
	w.pending += len(s)
	if w.pending >= value.InspectChunk {
		w.settle()
		if w.err != nil {
			return
		}
	}
	w.write(s)
}

// settle passes check the bytes still pending.
func (w *jsonWriter) settle() {
	if w.err != nil || w.pending == 0 {
		return
	}
	n := w.pending
	w.pending = 0
	w.err = w.check(int64(n))
}

func (w *jsonWriter) fail(e *value.Error) {
	if w.err == nil {
		w.err = e
	}
}

func (w *jsonWriter) value(o value.Object, depth int) {
	if w.err != nil {
		return
	}
	switch o := o.(type) {
	case *value.Integer:
		w.put(strconv.FormatInt(o.Value, 10))
	case *value.Boolean:
		w.put(strconv.FormatBool(o.Value))
	case *value.Nil:
		w.put("null")
	case *value.String:
		w.str(o.Value)
	case *value.Array:
		if !w.room(depth) {
			return
		}
		w.put("[")
		for i, e := range o.Elements {
			if i > 0 {
				w.put(",")
			}
			w.value(e, depth+1)
		}
		w.put("]")
	case *value.Hash:
		if !w.room(depth) {
			return
		}
		w.hash(o, depth)
	default:
		w.fail(newError("json: cannot encode %s", o.Type()))
	}
}

// room fails once nesting reaches value.MaxInspectDepth, which keeps the
// recursion off the end of the Go stack.
func (w *jsonWriter) room(depth int) bool {
	if depth >= value.MaxInspectDepth {
		w.fail(newError("json: nested deeper than %d levels", value.MaxInspectDepth))
		return false
	}
	return true
}

func (w *jsonWriter) hash(h *value.Hash, depth int) {
	type entry struct {
		key string
		val value.Object
	}
	entries := make([]entry, 0, len(h.Pairs))
	for _, hk := range h.Keys() { // in order: the first bad key is always the same
		p := h.Pairs[hk]
		k, ok := p.Key.(*value.String)
		if !ok {
			w.fail(newError("json: hash keys must be STRING, got %s", p.Key.Type()))
			return
		}
		entries = append(entries, entry{k.Value, p.Value})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].key < entries[j].key })
	w.put("{")
	for i, e := range entries {
		if i > 0 {
			w.put(",")
		}
		w.str(e.key)
		w.put(":")
		w.value(e.val, depth+1)
	}
	w.put("}")
}

const hexDigits = "0123456789abcdef"

// str writes s as a JSON string, escaping what JSON requires.
func (w *jsonWriter) str(s string) {
	w.put(`"`)
	start := 0
	for i := 0; i < len(s) && w.err == nil; {
		c := s[i]
		if c >= utf8.RuneSelf {
			r, size := utf8.DecodeRuneInString(s[i:])
			if r == utf8.RuneError && size == 1 {
				w.put(s[start:i])
				w.put(`�`)
				start = i + 1
			}
			i += size
			continue
		}
		if c >= 0x20 && c != '"' && c != '\\' {
			i++
			continue
		}
		w.put(s[start:i])
		switch c {
		case '"', '\\':
			w.put(`\` + string(c))
		case '\n':
			w.put(`\n`)
		case '\r':
			w.put(`\r`)
		case '\t':
			w.put(`\t`)
		default:
			w.put(`\u00` + string(hexDigits[c>>4]) + string(hexDigits[c&0xf]))
		}
		i++
		start = i
	}
	w.put(s[start:])
	w.put(`"`)
}
