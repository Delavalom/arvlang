# Phase 5, task p5.3: `json(v)`

> **For agentic workers:** this file is one task, p5.3. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: tasks p5.1 to p5.2 are committed in your tree. Start every shell command with `cd <your worktree> &&` and use absolute paths: the shell's working directory does not persist. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify` and never push; keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0` (toolchain 1.26.2): `t.Chdir`, `min` and `max` are available; do not use `testing/synctest` or `sync.WaitGroup.Go`. Before a long test run, check `df -h /System/Volumes/Data`; below 1 GB free, stop and report (only `go clean -cache` may be deleted). This machine is heavily loaded: keep wall-clock bounds generous.
>
> Every code block below was applied, in task order, to a scratch copy of `feat/phases-3-8` (Phases 3 and 4 merged) and passed `gofmt -l .`, `go vet ./...` and `go test -race ./...` (including `e2e/`), and `bench/uptime-go`'s own `go vet` and `go test -race`. Design: `12-phase5-gate.md`. Deviations from it, and why: `p5-plan-1.md`. **Do not measure or write the gate verdict**: a separate step does that.

### Task p5.3: `json(v)`

`json(v)` is `v` as compact JSON: integers, strings, booleans, `nil` (as `null`), arrays, and hashes with string keys written in key order, as Go's `encoding/json` writes a map. Any other value (a function, a builtin, a capability, a task handle) is an error, and so are non-string hash keys and nesting deeper than `value.MaxInspectDepth`. Invalid UTF-8 becomes U+FFFD byte by byte, as in `encoding/json`. The uptime checker writes `report.json` and its summary line with it.

**Budget.** A value can encode far larger than it is: an array holding the same sub-array twice, n levels deep, encodes to about 2^n times its leaf. So the text is charged before it is written, `value.InspectChunk` (64 KiB) at a time, and at most one chunk is ever uncharged. Between chunks it stops if the task was cancelled, so a huge encoding under no `alloc_mb` still ends at the scope's deadline (like `puts`). The writer keeps chunks and copies them once into an exact-size string. A `strings.Builder`, which grows by a quarter at a time once large, allocated 5 MiB to refuse a 14 MiB encoding under a 1 MiB limit; chunks allocate 1.07 MiB.

`FuzzJSONString` checks every string against `encoding/json` (as a seeded test here; a later step fuzzes it for real).

**Files:**
- Create: `arv/evaluator/json.go`
- Create (test): `arv/evaluator/json_test.go`
- Create (test): `arv/evaluator/json_budget_test.go`

- [ ] **Step 1: Write the failing tests**

Create `arv/evaluator/json_test.go`:

```go
package evaluator

import (
	"context"
	"encoding/json"
	"io"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

func TestJSONValues(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{`json(42)`, `42`},
		{`json(-7)`, `-7`},
		{`json(true)`, `true`},
		{`json(if (false) { 1 })`, `null`},
		{`json("hi")`, `"hi"`},
		{`json([])`, `[]`},
		{`json({})`, `{}`},
		{`json([1, "a", false, [2]])`, `[1,"a",false,[2]]`},
		{"json(\"a\\b\nc\td\")", `"a\\b\nc\td"`},
	} {
		got := testEval(tc.in)
		if s, ok := got.(*value.String); !ok || s.Value != tc.want {
			t.Errorf("%s = %s, want %s", tc.in, got.Inspect(), tc.want)
		}
	}
}

// Hash pairs are written in key order, every time, though a hash's own
// order is random.
func TestJSONHashKeyOrder(t *testing.T) {
	src := `json({"b": 1, "a": [2], "c": {"z": "q", "y": true}, "aa": 3})`
	want := `{"a":[2],"aa":3,"b":1,"c":{"y":true,"z":"q"}}`
	for i := 0; i < 50; i++ {
		got := testEval(src)
		if s, ok := got.(*value.String); !ok || s.Value != want {
			t.Fatalf("run %d: %s, want %s", i, got.Inspect(), want)
		}
	}
}

func TestJSONErrors(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{`json()`, `json: want 1 argument, got 0`},
		{`json(1, 2)`, `json: want 1 argument, got 2`},
		{`json(fn(x) { x })`, `json: cannot encode FUNCTION`},
		{`json([1, len])`, `json: cannot encode BUILTIN`},
		{`json({1: 2})`, `json: hash keys must be STRING, got INTEGER`},
		{`json({"a": {true: 1}})`, `json: hash keys must be STRING, got BOOLEAN`},
	} {
		got := testEval(tc.in)
		if !isError(got) || got.(*value.Error).Message != tc.want {
			t.Errorf("%s = %s, want error %q", tc.in, got.Inspect(), tc.want)
		}
	}
}

// encode calls the builtin directly, with no budget.
func encode(t *testing.T, ctx context.Context, v value.Object) value.Object {
	t.Helper()
	root := runtime.NewRoot(ctx, runtime.NewCapSet(), io.Discard)
	return jsonEncode(root, func(int64) error { return nil }, v)
}

// Any string comes back unchanged through encoding/json, apart from
// invalid UTF-8, which both sides turn into U+FFFD byte by byte.
func FuzzJSONString(f *testing.F) {
	for _, s := range []string{"", "plain", "q\"b\\s/", "\x00\x01\x1f\x7f", "\b\f\n\r\t", "é€😀", "\xff\xfe a \xc3", " <&>"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		got := encode(t, context.Background(), &value.String{Value: s})
		out, ok := got.(*value.String)
		if !ok {
			t.Fatalf("json(%q) = %s", s, got.Inspect())
		}
		var back string
		if err := json.Unmarshal([]byte(out.Value), &back); err != nil {
			t.Fatalf("json(%q) = %s: not JSON: %v", s, out.Value, err)
		}
		ref, _ := json.Marshal(s)
		var want string
		if err := json.Unmarshal(ref, &want); err != nil {
			t.Fatal(err)
		}
		if back != want {
			t.Fatalf("json(%q) decodes to %q, want %q", s, back, want)
		}
	})
}

// Nesting is bounded, as Inspect's is, so no value overflows the Go stack.
func TestJSONNestingLimit(t *testing.T) {
	var v value.Object = &value.Array{}
	for i := 0; i < value.MaxInspectDepth; i++ {
		v = &value.Array{Elements: []value.Object{v}}
	}
	got := encode(t, context.Background(), v)
	if !isError(got) || got.(*value.Error).Message != "json: nested deeper than 10000 levels" {
		t.Fatalf("got %.80s", got.Inspect())
	}
}
```

Create `arv/evaluator/json_budget_test.go`:

```go
package evaluator

import (
	"context"
	"testing"
	"time"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// json's text costs its bytes: "abc" encodes as 5.
func TestJSONChargesItsText(t *testing.T) {
	if got := runCapsIn(t, 5, runtime.NewCapSet(), `json("abc")`); isError(got) {
		t.Fatalf("json under 5 bytes: %s", got.Inspect())
	}
	got := runCapsIn(t, 4, runtime.NewCapSet(), `json("abc")`)
	if !isError(got) || got.(*value.Error).ErrKind() != runtime.KindBudget {
		t.Fatalf("json under 4 bytes: got %s, want a budget error", got.Inspect())
	}
}

// doubling is a value of 2^levels "xxxxxxxxxx" strings, built in levels
// arrays of two: about 14 bytes of JSON per string.
func doubling(levels int) value.Object {
	var v value.Object = &value.String{Value: "xxxxxxxxxx"}
	for i := 0; i < levels; i++ {
		v = &value.Array{Elements: []value.Object{v, v}}
	}
	return v
}

// A value that encodes far larger than it is stops at the limit: the
// text is charged before it is written, one chunk at a time.
func TestJSONBlowupStopsAtTheLimit(t *testing.T) {
	env := value.NewEnvironment()
	env.Set("v", doubling(20)) // about 14 MiB of JSON
	root := runtime.NewRoot(context.Background(), runtime.NewCapSet(), nil)
	in := New(root)
	b := runtime.Root(runtime.Limits{AllocBytes: 1 << 20})
	program := parse(t, `json(v)`)
	var got value.Object
	used := heapDuring(func() {
		got = in.withScope(frame{runtime.NewScope(root.Ctx), b}, func() value.Object {
			return in.Eval(program, env)
		})
	})
	wantErrorAt(t, got, allocErr, "1:1")
	// 1 MiB of chunks and at most one uncharged chunk: measured 1.07 MiB.
	// A strings.Builder, growing by a quarter at a time, allocated 5 MiB.
	if used > 2<<20 {
		t.Errorf("json past a 1 MiB limit allocated %d bytes, want under 2 MiB", used)
	}
}

// With no alloc limit, a cancelled task stops encoding at the next chunk.
func TestJSONStopsWhenCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	got := encode(t, ctx, doubling(40)) // about 15 TB of JSON
	if !isError(got) || got.(*value.Error).ErrKind() != runtime.KindCancelled {
		t.Fatalf("got %.80s, want a cancelled error", got.Inspect())
	}
	if d := time.Since(start); d > 30*time.Second {
		t.Errorf("a cancelled json took %s", d)
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test -run 'TestJSON|FuzzJSON' ./arv/evaluator/`
Expected: FAIL, build error `undefined: jsonEncode`.

- [ ] **Step 3: Implement**

Create `arv/evaluator/json.go`:

```go
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
	for _, p := range h.Pairs {
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
```

- [ ] **Step 4: Run to verify they pass**

Run: `go test -race -run 'TestJSON|FuzzJSON' ./arv/evaluator/`, then `gofmt -l .`, `go vet ./...` and `go test -race ./...`.
Expected: PASS everywhere. `TestJSONBlowupStopsAtTheLimit` measures heap with `runtime.ReadMemStats`; the evaluator tests do not run in parallel, so its 2 MiB bound holds.

- [ ] **Step 5: Commit**

```bash
git add arv/evaluator/json.go arv/evaluator/json_test.go arv/evaluator/json_budget_test.go
git commit -m "feat(evaluator): json builtin

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
