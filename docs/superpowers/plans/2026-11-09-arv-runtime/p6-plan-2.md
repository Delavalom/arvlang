# Phase 6, task p6.2: The value codec

> **For agentic workers:** this file is one task, p6.2. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: tasks p6.1 to p6.1 are committed in your tree. Start every shell command with `cd <your worktree> &&`. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify`; keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0` (toolchain 1.26.2), so do not use `testing/synctest` or `sync.WaitGroup.Go`. `arv/runtime` stays stdlib-only.
>
> Every code block below was applied, in task order, to a scratch copy of the tree and passed `go vet ./...` and `go test -race ./...` (including `e2e/`). Design: `13-phase6-durable-replay.md`. Deviations from it, and why: `p6-plan-1.md`.

### Task p6.2: The value codec

The log stores effect requests and results as JSON. `value.Encode` and `Decode` round-trip every value an effect can return or take:
- `null`, booleans and integers as JSON; a string as a JSON string if it is valid UTF-8, else `{"bytes":"<base64>"}`; an array as an array; a hash as `{"hash":[[k,v],...]}` in `Keys()` order; an error as `{"error":[message,kind,pos,task]}`.
- Functions, tasks, capabilities and builtins are never effect results: encoding one is an error. Nesting deeper than `MaxCodecDepth` (10000; a hash counts three levels) is an error.
- `Decode` refuses anything `Encode` cannot write: floats, trailing data, objects with other or several keys, duplicate or unhashable hash keys. It never panics (fuzzed).
- `EncodeRequest(args)` encodes an effect's arguments as an array and never fails: a value `Encode` refuses becomes `{"type":"FUNCTION"}`, so a divergence can still name the request.

The encoder writes its own JSON strings, so a request or result is the same bytes on every run, whatever `encoding/json` would escape.

**Files:**
- Create: `arv/value/codec.go`
- Create (test): `arv/value/codec_test.go`

- [ ] **Step 1: Write the failing tests**

Create `arv/value/codec_test.go`:

```go
package value

import (
	"bytes"
	"math"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
)

func hashOf(pairs ...Object) *Hash {
	h := NewHashSize(len(pairs) / 2)
	for i := 0; i < len(pairs); i += 2 {
		h.Set(pairs[i].(Hashable).HashKey(), HashPair{Key: pairs[i], Value: pairs[i+1]})
	}
	return h
}

// same reports whether a and b are the same value, field by field: Inspect
// alone would not tell an error's kind or a hash's key types apart.
func same(a, b Object) bool {
	if a.Type() != b.Type() {
		return false
	}
	switch a := a.(type) {
	case *Array:
		b := b.(*Array)
		if len(a.Elements) != len(b.Elements) {
			return false
		}
		for i := range a.Elements {
			if !same(a.Elements[i], b.Elements[i]) {
				return false
			}
		}
		return true
	case *Hash:
		b := b.(*Hash)
		ak, bk := a.Keys(), b.Keys()
		if len(ak) != len(bk) {
			return false
		}
		for i := range ak {
			if ak[i] != bk[i] || !same(a.Pairs[ak[i]].Key, b.Pairs[bk[i]].Key) || !same(a.Pairs[ak[i]].Value, b.Pairs[bk[i]].Value) {
				return false
			}
		}
		return true
	case *Error:
		return *a == *b.(*Error)
	}
	return a.Inspect() == b.Inspect()
}

// Every encodable type comes back equal, byte for byte (I7: a replayed
// effect returns exactly what the recorded one did).
func TestCodecRoundTrip(t *testing.T) {
	for _, v := range []Object{
		&Nil{}, &Boolean{Value: true}, &Boolean{Value: false},
		&Integer{Value: 0}, &Integer{Value: -42}, &Integer{Value: math.MaxInt64}, &Integer{Value: math.MinInt64},
		&String{Value: ""}, &String{Value: "plain"}, &String{Value: "quote \" back \\ nl \n tab \t nul \x00 bell \x07"},
		&String{Value: "ünïcödé ✓   <&>"}, &String{Value: "bad utf-8 \xff\xfe end"},
		&Array{}, &Array{Elements: []Object{&Integer{Value: 1}, &String{Value: "a"}, &Array{Elements: []Object{&Nil{}}}}},
		hashOf(), hashOf(&String{Value: "status"}, &Integer{Value: 200}, &String{Value: "body"}, &String{Value: "x\xff"}),
		hashOf(&Integer{Value: 1}, &Boolean{Value: true}, &Boolean{Value: false}, hashOf(&String{Value: "k"}, &Nil{})),
		&Error{Message: "boom \xff", Kind: runtime.KindEffect, Pos: "3:7", Task: "root/0#2"},
		&Error{Message: "no kind"},
	} {
		b, err := Encode(v)
		if err != nil {
			t.Fatalf("Encode(%s): %v", v.Inspect(), err)
		}
		got, err := Decode(b)
		if err != nil {
			t.Fatalf("Decode(%s): %v", b, err)
		}
		if !same(got, v) {
			t.Errorf("round trip of %s gave %s (via %s)", v.Inspect(), got.Inspect(), b)
		}
		if again, _ := Encode(got); !bytes.Equal(again, b) {
			t.Errorf("re-encoding %s gave %s", b, again)
		}
	}
}

func TestCodecFormat(t *testing.T) {
	for _, tt := range []struct {
		v    Object
		want string
	}{
		{&Integer{Value: -7}, `-7`},
		{&String{Value: "a\"b"}, `"a\"b"`},
		{&String{Value: "\xff"}, `{"bytes":"/w=="}`},
		{hashOf(&String{Value: "b"}, &Integer{Value: 1}, &String{Value: "a"}, &Nil{}), `{"hash":[["b",1],["a",null]]}`},
		{&Error{Message: "m", Kind: "user", Pos: "1:2", Task: "root"}, `{"error":["m","user","1:2","root"]}`},
	} {
		if b, err := Encode(tt.v); err != nil || string(b) != tt.want {
			t.Errorf("Encode(%s) = %s, %v; want %s", tt.v.Inspect(), b, err, tt.want)
		}
	}
}

func TestCodecRefusesWhatEffectsNeverReturn(t *testing.T) {
	for _, v := range []Object{
		&Function{}, &Builtin{}, &Capability{}, NewTaskHandle("root/0", nil), &ReturnValue{Value: &Nil{}},
		&Array{Elements: []Object{&Function{}}}, hashOf(&String{Value: "f"}, &Builtin{}), nil,
	} {
		if b, err := Encode(v); err == nil {
			t.Errorf("Encode(%T) = %s, want an error", v, b)
		}
	}
}

// A value nested as deeply as Encode allows decodes; one level more is
// refused by Encode, never written for Decode to choke on.
func TestCodecDepth(t *testing.T) {
	nest := func(n int) Object {
		var v Object = &Integer{Value: 1}
		for i := 0; i < n; i++ {
			v = &Array{Elements: []Object{v}}
		}
		return v
	}
	b, err := Encode(nest(MaxCodecDepth - 1))
	if err != nil {
		t.Fatalf("Encode at the limit: %v", err)
	}
	if _, err := Decode(b); err != nil {
		t.Fatalf("Decode at the limit: %v", err)
	}
	if _, err := Encode(nest(MaxCodecDepth)); err == nil || !strings.Contains(err.Error(), "deeper") {
		t.Fatalf("Encode past the limit = %v", err)
	}
	var h Object = &Nil{}
	for i := 0; i < MaxCodecDepth/3; i++ {
		h = hashOf(&Integer{Value: 1}, h)
	}
	if b, err := Encode(h); err == nil {
		if _, err := Decode(b); err != nil {
			t.Fatalf("Encode wrote a hash Decode refuses: %v", err)
		}
	}
}

func TestDecodeRejects(t *testing.T) {
	for _, in := range []string{
		``, `1.5`, `1e3`, `99999999999999999999`, `{}`, `{"bytes":"%%"}`, `{"bytes":1}`,
		`{"hash":[[1]]}`, `{"hash":[[[1],2]]}`, `{"hash":[["a",1],["a",2]]}`, `{"hash":{}}`,
		`{"error":["a","b","c"]}`, `{"error":[1,"b","c","d"]}`, `{"nope":1}`, `{"bytes":"","hash":[]}`,
		`1 2`, `[1,`, `"\ud800"x`, `{"type":"FUNCTION"}`,
	} {
		if v, err := Decode([]byte(in)); err == nil {
			t.Errorf("Decode(%s) = %s, want an error", in, v.Inspect())
		}
	}
}

func TestEncodeRequestIsLoose(t *testing.T) {
	got := EncodeRequest([]Object{&String{Value: "u"}, &Function{}, &Integer{Value: 3}, nil})
	if want := `["u",{"type":"FUNCTION"},3,{"type":"NONE"}]`; string(got) != want {
		t.Fatalf("EncodeRequest = %s, want %s", got, want)
	}
	if got := EncodeRequest(nil); string(got) != `[]` {
		t.Fatalf("EncodeRequest(nil) = %s", got)
	}
}

// FuzzDecode: Decode never panics, and whatever it accepts encodes and
// decodes again to the same bytes.
func FuzzDecode(f *testing.F) {
	for _, s := range []string{`null`, `[1,"a",{"bytes":"/w=="}]`, `{"hash":[["k",[true]]]}`, `{"error":["m","user","1:1","root"]}`, `-0`, `"\u0000"`} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		v, err := Decode(b)
		if err != nil {
			return
		}
		enc, err := Encode(v)
		if err != nil {
			t.Fatalf("Decode(%q) gave %s, which Encode refuses: %v", b, v.Inspect(), err)
		}
		v2, err := Decode(enc)
		if err != nil {
			t.Fatalf("Decode(Encode(...)) = %v for %s", err, enc)
		}
		if enc2, _ := Encode(v2); !bytes.Equal(enc, enc2) {
			t.Fatalf("not stable: %s then %s", enc, enc2)
		}
	})
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd <worktree> && go test -race ./arv/value/ -run 'TestCodec|TestDecodeRejects|TestEncodeRequestIsLoose'`

Expected: the package fails to build: `undefined: Encode`, `undefined: Decode`, `undefined: EncodeRequest`, `undefined: MaxCodecDepth`.

- [ ] **Step 3: Implement**

Create `arv/value/codec.go`:

```go
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

// Encode is o as JSON, for the effect log (Phase 6): Decode gives back an
// equal value, byte for byte. It covers what effects return:
//
//	nil      null              integer  42 (a JSON number, always whole)
//	boolean  true / false      string   "text" if valid UTF-8, else {"bytes":"<base64>"}
//	array    [e1, e2]          hash     {"hash":[[k1, v1], [k2, v2]]} in key order
//	error    {"error":[message, kind, pos, task]}, each a string as above
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
}

func (e *encoder) value(o Object, depth int) error {
	if depth >= MaxCodecDepth {
		return e.refuse(o, "value nests deeper than %d levels", MaxCodecDepth)
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
	return fromJSON(v)
}

func fromJSON(v any) (Object, error) {
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
		return &String{Value: v}, nil
	case []any:
		els := make([]Object, len(v))
		for i, el := range v {
			o, err := fromJSON(el)
			if err != nil {
				return nil, err
			}
			els[i] = o
		}
		return &Array{Elements: els}, nil
	case map[string]any:
		if len(v) != 1 {
			return nil, fmt.Errorf("value: an object must have exactly one of bytes, hash, error; got %d keys", len(v))
		}
		if raw, ok := v["bytes"]; ok {
			s, err := bytesString(raw)
			if err != nil {
				return nil, err
			}
			return &String{Value: s}, nil
		}
		if raw, ok := v["hash"]; ok {
			return hashFromJSON(raw)
		}
		if raw, ok := v["error"]; ok {
			return errorFromJSON(raw)
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

func hashFromJSON(raw any) (Object, error) {
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
		k, err := fromJSON(kv[0])
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
		val, err := fromJSON(kv[1])
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
```

- [ ] **Step 4: Run the tests, then the full checks**

Run: `cd <worktree> && go test -race ./arv/value/ -run 'TestCodec|TestDecodeRejects|TestEncodeRequestIsLoose'`, then `cd <worktree> && gofmt -l . && go vet ./... && go test -race ./...`

Expected: `ok` for every package, and `gofmt -l .` prints nothing. Then fuzz briefly (check `df -h /System/Volumes/Data` first; stop if under 1 GB free): `cd <worktree> && go test ./arv/value/ -run '^$' -fuzz FuzzDecode -fuzztime 20s` passes. Do not commit `testdata/fuzz` unless it found a crash you fixed.

- [ ] **Step 5: Commit**

```bash
cd <worktree> && git add arv/value/codec.go arv/value/codec_test.go && git commit -F - <<'MSG'
feat(value): codec for effect requests and results

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
MSG
```
