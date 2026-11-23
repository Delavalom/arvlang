package evaluator

import (
	"fmt"
	"math"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

func init() {
	builtins["parse_json"] = &value.Builtin{Charging: parseJSON}
}

// jsonContainerSize is what parse_json charges for each array or hash it
// creates, beyond its elements: the value and its slice or map headers.
const jsonContainerSize = 48

// parse_json(text) is the value JSON text describes, the inverse of json:
// objects become hashes with string keys (in the text's order), arrays
// arrays, strings strings, true and false booleans, and null nil. Arv has
// integers only (floats are open decision D3), so a number must have an
// integer value in the int64 range: 1e3 and 1.0 are the integers 1000
// and 1; 1.5 and 1e-1 are errors. An object that names a key twice is an
// error, so no two readers of the text can disagree on its value. Invalid
// UTF-8 and lone surrogates in a string become U+FFFD, as json writes them.
//
// What it builds is charged to alloc_mb before it is built, a chunk at a
// time (value.InspectChunk), as json charges its text: at most one chunk
// is ever uncharged. It checks between chunks of text read whether the
// task was cancelled, so a long text under no limit still ends at the
// scope's deadline. Nesting is bounded as json's is (value.MaxInspectDepth).
func parseJSON(t *runtime.Task, charge func(int64) error, args ...value.Object) value.Object {
	text, errObj := oneString("parse_json", args)
	if errObj != nil {
		return errObj
	}
	p := jsonParser{s: text, t: t, charge: charge}
	p.space()
	v := p.value(0)
	if p.err == nil {
		p.space()
		if p.i < len(p.s) {
			p.fail(p.unexpected("after the top-level value"))
		}
	}
	p.settle()
	if p.err != nil {
		return p.err
	}
	return v
}

type jsonParser struct {
	s       string
	i       int // the next byte to read
	t       *runtime.Task
	charge  func(int64) error
	pending int64        // bytes about to be built, not yet charged
	polled  int          // where the text was last checked for cancellation
	err     *value.Error // the first error; parsing has stopped
}

func (p *jsonParser) fail(e *value.Error) {
	if p.err == nil {
		p.err = e
	}
}

// spend notes n bytes about to be built, charging what is pending once it
// reaches a chunk. It reports whether parsing may go on.
func (p *jsonParser) spend(n int64) bool {
	p.pending += n
	if p.pending >= value.InspectChunk {
		p.settle()
	}
	return p.err == nil
}

// settle checks for cancellation and charges what is pending.
func (p *jsonParser) settle() {
	if p.err != nil || p.pending == 0 || p.cancelled() {
		return
	}
	n := p.pending
	p.pending = 0
	if err := p.charge(n); err != nil {
		p.fail(errorOf(err, runtime.KindBudget))
	}
}

func (p *jsonParser) cancelled() bool {
	p.polled = p.i
	if err := p.t.Err(); err != nil {
		p.fail(newErrorKind(runtime.KindCancelled, "task %s cancelled: %s", p.t.ID, err))
		return true
	}
	return false
}

// poll checks for cancellation once another chunk of text has been read.
func (p *jsonParser) poll() bool {
	if p.i-p.polled >= value.InspectChunk {
		return !p.cancelled()
	}
	return true
}

func (p *jsonParser) space() {
	for p.i < len(p.s) && p.poll() {
		switch p.s[p.i] {
		case ' ', '\t', '\n', '\r':
			p.i++
		default:
			return
		}
	}
}

// at is the line and column (1-based, in bytes) of offset i, for errors.
func (p *jsonParser) at(i int) string {
	line := 1 + strings.Count(p.s[:i], "\n")
	col := i - strings.LastIndexByte(p.s[:i], '\n')
	return fmt.Sprintf("line %d, column %d", line, col)
}

func (p *jsonParser) errAt(i int, format string, a ...any) *value.Error {
	return newError("parse_json: %s at %s", fmt.Sprintf(format, a...), p.at(i))
}

var errJSONEnd = newError("parse_json: unexpected end of JSON input")

// unexpected is the error for the byte at p.i, or the end of the text.
func (p *jsonParser) unexpected(context string) *value.Error {
	if p.i >= len(p.s) {
		return errJSONEnd
	}
	return p.errAt(p.i, "invalid character %s %s", showByte(p.s[p.i]), context)
}

func showByte(c byte) string {
	if c >= 0x20 && c < 0x7f {
		return "'" + string(c) + "'"
	}
	return fmt.Sprintf(`'\x%02x'`, c)
}

// showBytes is s with every byte that is not printable ASCII as \xNN.
func showBytes(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if c := s[i]; c >= 0x20 && c < 0x7f {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, `\x%02x`, c)
		}
	}
	return b.String()
}

func (p *jsonParser) value(depth int) value.Object {
	if p.err != nil || !p.poll() {
		return nil
	}
	if p.i >= len(p.s) {
		p.fail(errJSONEnd)
		return nil
	}
	switch c := p.s[p.i]; {
	case c == '{':
		return p.object(depth)
	case c == '[':
		return p.array(depth)
	case c == '"':
		s, ok := p.str()
		if !ok {
			return nil
		}
		return &value.String{Value: s}
	case c == '-' || c >= '0' && c <= '9':
		return p.number()
	case c == 't':
		return p.literal("true", TRUE)
	case c == 'f':
		return p.literal("false", FALSE)
	case c == 'n':
		return p.literal("null", NIL)
	}
	p.fail(p.unexpected("looking for a value"))
	return nil
}

func (p *jsonParser) literal(word string, v value.Object) value.Object {
	if !strings.HasPrefix(p.s[p.i:], word) {
		if strings.HasPrefix(word, p.s[p.i:]) {
			p.fail(errJSONEnd)
		} else {
			p.fail(p.errAt(p.i, "invalid literal"))
		}
		return nil
	}
	p.i += len(word)
	return v
}

// room fails once nesting reaches value.MaxInspectDepth, as json does.
func (p *jsonParser) room(depth int) bool {
	if depth >= value.MaxInspectDepth {
		p.fail(newError("parse_json: nested deeper than %d levels", value.MaxInspectDepth))
		return false
	}
	return true
}

func (p *jsonParser) array(depth int) value.Object {
	if !p.room(depth) || !p.spend(jsonContainerSize) {
		return nil
	}
	p.i++ // [
	var els []value.Object
	p.space()
	if p.i < len(p.s) && p.s[p.i] == ']' {
		p.i++
		return &value.Array{Elements: []value.Object{}}
	}
	for {
		if !p.spend(arrayElemSize) {
			return nil
		}
		v := p.value(depth + 1)
		if p.err != nil {
			return nil
		}
		els = append(els, v)
		p.space()
		if p.i < len(p.s) && p.s[p.i] == ',' {
			p.i++
			p.space()
			continue
		}
		if p.i < len(p.s) && p.s[p.i] == ']' {
			p.i++
			return &value.Array{Elements: els}
		}
		p.fail(p.unexpected("after an array element"))
		return nil
	}
}

func (p *jsonParser) object(depth int) value.Object {
	if !p.room(depth) || !p.spend(jsonContainerSize) {
		return nil
	}
	p.i++ // {
	h := value.NewHashSize(0)
	p.space()
	if p.i < len(p.s) && p.s[p.i] == '}' {
		p.i++
		return h
	}
	for {
		if p.i >= len(p.s) || p.s[p.i] != '"' {
			p.fail(p.unexpected("looking for an object key"))
			return nil
		}
		at := p.i
		if !p.spend(hashPairSize) {
			return nil
		}
		k, ok := p.str()
		if !ok {
			return nil
		}
		key := &value.String{Value: k}
		hk := key.HashKey()
		if old, dup := h.Pairs[hk]; dup {
			if old.Key.(*value.String).Value == k {
				p.fail(p.errAt(at, "duplicate key %s", quoteClipped(k)))
			} else { // two keys with the same 64-bit hash: a hash cannot hold both
				p.fail(p.errAt(at, "keys %s and %s collide", quoteClipped(old.Key.(*value.String).Value), quoteClipped(k)))
			}
			return nil
		}
		p.space()
		if p.i >= len(p.s) || p.s[p.i] != ':' {
			p.fail(p.unexpected("after an object key"))
			return nil
		}
		p.i++
		p.space()
		v := p.value(depth + 1)
		if p.err != nil {
			return nil
		}
		h.Set(hk, value.HashPair{Key: key, Value: v})
		p.space()
		if p.i < len(p.s) && p.s[p.i] == ',' {
			p.i++
			p.space()
			continue
		}
		if p.i < len(p.s) && p.s[p.i] == '}' {
			p.i++
			return h
		}
		p.fail(p.unexpected("after an object value"))
		return nil
	}
}

// str reads the string at p.i (its opening quote). It scans it first, to
// know the size of the result and charge it before building it.
func (p *jsonParser) str() (string, bool) {
	start := p.i + 1
	size, plain := 0, true
	i := start
	for {
		if i-p.polled >= value.InspectChunk {
			p.i = i
			if p.cancelled() {
				return "", false
			}
		}
		if i >= len(p.s) {
			p.fail(errJSONEnd)
			return "", false
		}
		c := p.s[i]
		switch {
		case c == '"':
			p.i = i + 1
			if !p.spend(int64(size)) {
				return "", false
			}
			if plain {
				return p.s[start:i], true
			}
			return p.unescape(start, i, size), true
		case c == '\\':
			plain = false
			n, width, err := p.escape(i)
			if err != nil {
				p.fail(err)
				return "", false
			}
			size += width
			i += n
		case c < 0x20:
			p.fail(p.errAt(i, "invalid character %s in a string", showByte(c)))
			return "", false
		case c < utf8.RuneSelf:
			size++
			i++
		default:
			r, n := utf8.DecodeRuneInString(p.s[i:])
			if r == utf8.RuneError && n == 1 {
				plain = false
				size += utf8.RuneLen(utf8.RuneError)
			} else {
				size += n
			}
			i += n
		}
	}
}

// escape reads the escape at i (its backslash): the bytes it spans and
// the bytes it decodes to. A \u escape of a surrogate pair spans both.
func (p *jsonParser) escape(i int) (n, width int, err *value.Error) {
	if i+1 >= len(p.s) {
		return 0, 0, errJSONEnd
	}
	switch p.s[i+1] {
	case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
		return 2, 1, nil
	case 'u':
		r, ok, end := p.hex4(i)
		if end {
			return 0, 0, errJSONEnd
		}
		if !ok {
			return 0, 0, p.errAt(i, "invalid escape '%s' in a string", showBytes(p.s[i:min(i+6, len(p.s))]))
		}
		if utf16.IsSurrogate(r) {
			if r2, ok, _ := p.hex4(i + 6); ok && utf16.DecodeRune(r, r2) != utf8.RuneError {
				return 12, 4, nil
			}
			return 6, utf8.RuneLen(utf8.RuneError), nil
		}
		return 6, utf8.RuneLen(r), nil
	}
	return 0, 0, p.errAt(i, "invalid escape '%s' in a string", showBytes(p.s[i:i+2]))
}

// hex4 reads a \uXXXX escape at i. end is set if the text ends inside it.
func (p *jsonParser) hex4(i int) (r rune, ok, end bool) {
	if i+1 >= len(p.s) || p.s[i] != '\\' || p.s[i+1] != 'u' {
		return 0, false, false
	}
	for j := i + 2; j < i+6; j++ {
		if j >= len(p.s) {
			return 0, false, true
		}
		c := p.s[j]
		var d byte
		switch {
		case c >= '0' && c <= '9':
			d = c - '0'
		case c >= 'a' && c <= 'f':
			d = c - 'a' + 10
		case c >= 'A' && c <= 'F':
			d = c - 'A' + 10
		default:
			return 0, false, false
		}
		r = r<<4 | rune(d)
	}
	return r, true, false
}

// unescape builds the string between start and end, already scanned
// (str), into exactly size bytes.
func (p *jsonParser) unescape(start, end, size int) string {
	var b strings.Builder
	b.Grow(size)
	for i := start; i < end; {
		c := p.s[i]
		switch {
		case c == '\\':
			switch p.s[i+1] {
			case 'b':
				b.WriteByte('\b')
			case 'f':
				b.WriteByte('\f')
			case 'n':
				b.WriteByte('\n')
			case 'r':
				b.WriteByte('\r')
			case 't':
				b.WriteByte('\t')
			case 'u':
				r, _, _ := p.hex4(i)
				if utf16.IsSurrogate(r) {
					if r2, ok, _ := p.hex4(i + 6); ok {
						if d := utf16.DecodeRune(r, r2); d != utf8.RuneError {
							b.WriteRune(d)
							i += 12
							continue
						}
					}
					r = utf8.RuneError
				}
				b.WriteRune(r)
				i += 6
				continue
			default: // " \ /
				b.WriteByte(p.s[i+1])
			}
			i += 2
		case c < utf8.RuneSelf:
			b.WriteByte(c)
			i++
		default:
			r, n := utf8.DecodeRuneInString(p.s[i:end])
			if r == utf8.RuneError && n == 1 {
				b.WriteRune(utf8.RuneError)
			} else {
				b.WriteString(p.s[i : i+n])
			}
			i += n
		}
	}
	return b.String()
}

// number reads the number at p.i. Its value must be an integer in the
// int64 range, however it is written.
func (p *jsonParser) number() value.Object {
	start := p.i
	i := start
	digits := func() int {
		j := i
		for i < len(p.s) && p.s[i] >= '0' && p.s[i] <= '9' {
			i++
		}
		return i - j
	}
	neg := i < len(p.s) && p.s[i] == '-'
	if neg {
		i++
	}
	intStart := i
	if i < len(p.s) && p.s[i] == '0' {
		i++
	} else if digits() == 0 {
		return p.badNumber(start, i)
	}
	intPart := p.s[intStart:i]
	frac := ""
	if i < len(p.s) && p.s[i] == '.' {
		i++
		fs := i
		if digits() == 0 {
			return p.badNumber(start, i)
		}
		frac = p.s[fs:i]
	}
	var exp int64
	if i < len(p.s) && (p.s[i] == 'e' || p.s[i] == 'E') {
		i++
		expNeg := false
		if i < len(p.s) && (p.s[i] == '+' || p.s[i] == '-') {
			expNeg = p.s[i] == '-'
			i++
		}
		es := i
		if digits() == 0 {
			return p.badNumber(start, i)
		}
		for _, c := range p.s[es:i] {
			if exp < 1<<40 { // saturate: no text has 2^40 digits to move
				exp = exp*10 + int64(c-'0')
			}
		}
		if expNeg {
			exp = -exp
		}
	}
	p.i = i
	text := p.s[start:i]
	n, ok, isInt := intValue(intPart, frac, exp, neg)
	if !isInt {
		p.fail(newError("parse_json: number %s at %s is not an integer, and Arv has no floats yet", clipped(text), p.at(start)))
		return nil
	}
	if !ok {
		p.fail(newError("parse_json: number %s at %s is out of the integer range", clipped(text), p.at(start)))
		return nil
	}
	return &value.Integer{Value: n}
}

func (p *jsonParser) badNumber(start, i int) value.Object {
	if i >= len(p.s) {
		p.fail(errJSONEnd)
	} else {
		p.fail(p.errAt(start, "invalid number"))
	}
	return nil
}

// intValue is the value of the number with digits intPart.frac times
// 10^exp, negated if neg. isInt is false if that value is not an integer,
// ok false if it is one outside the int64 range.
func intValue(intPart, frac string, exp int64, neg bool) (n int64, ok, isInt bool) {
	ds := strings.TrimLeft(intPart+frac, "0")
	exp -= int64(len(frac))
	trimmed := strings.TrimRight(ds, "0")
	exp += int64(len(ds) - len(trimmed))
	ds = trimmed
	if ds == "" {
		return 0, true, true
	}
	if exp < 0 {
		return 0, false, false
	}
	if int64(len(ds))+exp > 19 {
		return 0, false, true
	}
	var u uint64
	for _, c := range ds {
		u = u*10 + uint64(c-'0') // at most 19 digits: no overflow
	}
	for ; exp > 0; exp-- {
		if u > math.MaxUint64/10 {
			return 0, false, true
		}
		u *= 10
	}
	limit := uint64(math.MaxInt64)
	if neg {
		limit++
	}
	if u > limit {
		return 0, false, true
	}
	if neg {
		return int64(-u), true, true // -(1<<63) wraps to itself
	}
	return int64(u), true, true
}
