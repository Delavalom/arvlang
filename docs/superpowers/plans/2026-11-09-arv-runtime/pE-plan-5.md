# Phase 5E, task pE.5: the `str` builtin

> **For agentic workers:** this file is one task, pE.5. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: tasks pE.1 to pE.4 are committed on `phase/pE` in `/Users/delavalom/delavalom-labs/arvlang-wt/pE`. Start every shell command with `cd /Users/delavalom/delavalom-labs/arvlang-wt/pE &&`. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green (first check `df -h /System/Volumes/Data`: below 1 GB free, stop and report "blocked: disk"; the only thing you may delete is the build cache, `go clean -cache`). End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify`, never push. Keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0`; the toolchain is 1.26.2. Create files that contain `${` with the Write or Edit tool, not with a shell heredoc.
>
> Every code block below was applied, in task order, to a scratch copy of the tree and passed `go vet ./...` and `go test -race ./...` (including `e2e/`). Overview, guarantees G1-G6, decisions and deviations: `pE-plan-1.md`.

### Task pE.5: `str(v)` renders a value as `puts` prints it

Interpolation (pE.6) needs one rule for turning a value into text, and the rule is `puts`'s (decision 2 in `pE-plan-1.md`): a string is itself, any other value its `Inspect` text (`-42`, `true`, `nil`, `[1, x, [true]]`, `{k: [1]}`, a function's source). `str` makes the rule a builtin, so the hand-written equivalent of `"a ${x}"` exists (`"a " + str(x)`), and so its cost can be tested against interpolation's (G4).

**Budgets (G6).** A string comes back as it is (shared, like `last`), so it costs nothing. Any other value can print far larger than it is (an array holding the same sub-array twice, n levels deep), so, as `puts` does, the text is charged to `alloc_mb` while it is built, `value.InspectChunk` (64 KiB) at a time, and building stops at the first refused chunk or once the task is cancelled; the rest is charged before the string is returned, so the whole text is charged exactly once. The result is copied to its exact size, so a held string keeps none of the builder's slack. Rendering a large value allocates about 6.8 times its text while it runs (the builder's growth plus that copy); that is a constant factor per call, a Minor known limit under the `alloc_mb` rule, recorded in pE.8's notes.

**The second name.** The builtin is registered twice: as `str`, and as `ast.StrBuiltin` (`$str`). The lexer cannot produce `$` in an identifier, so no program can bind `$str`; pE.6 makes interpolation call that name, so `let str = ...` never changes what `"${x}"` prints.

**Files:**
- Modify: `arv/ast/string.go`
- Create: `arv/evaluator/str.go`
- Create (test): `arv/evaluator/str_test.go`

- [ ] **Step 1: Write the failing test**

Create `arv/evaluator/str_test.go`:

```go
package evaluator

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/delavalom/arvlang/arv/ast"
	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// values are bound before each str case: one of every kind puts prints.
const values = `let i = -42; let s = "text"; let b = true; let n = if (false) { 1 };
let a = [1, "x", [true]]; let h = {"k": [1]}; let f = fn(x) { x + 1 };`

// str(v) is exactly the text puts prints for v.
func TestStrRendersAsPuts(t *testing.T) {
	for _, name := range []string{"i", "s", "b", "n", "a", "h", "f"} {
		_, printed := runWith(t, values+"puts("+name+")")
		got, _ := runWith(t, values+"str("+name+")")
		s, ok := got.(*value.String)
		if !ok || s.Value+"\n" != printed {
			t.Errorf("str(%s) = %s, puts printed %q", name, got.Inspect(), printed)
		}
	}
	got, _ := runWith(t, "str(1, 2)")
	wantErrorAt(t, got, "str: want 1 argument, got 2", "1:1")
}

// A string is returned as it is, so it costs nothing; any other value
// costs its text's bytes, charged once.
func TestStrChargesItsText(t *testing.T) {
	if n := fit(t, `str("abc")`, bytesLimit); n != 1 {
		t.Errorf("str of a string costs %d bytes, want nothing", n)
	}
	if n := fit(t, `str(12345)`, bytesLimit); n != 5 {
		t.Errorf("str(12345) costs %d bytes, want 5", n)
	}
	if n := fit(t, `str([1, 2])`, bytesLimit) - fit(t, `[1, 2]`, bytesLimit); n != 6 {
		t.Errorf("str([1, 2]) costs %d bytes more than the array, want 6 for \"[1, 2]\"", n)
	}
	// Past value.InspectChunk the text is charged as it is built; the
	// total is still its length, exactly.
	const build = `let d = fn(n, a) { if (n == 0) { a } else { d(n - 1, [a, a]) } }; let v = d(15, 7);`
	text := runLimited(t, build+"v", runtime.Limits{}).Inspect()
	if len(text) <= value.InspectChunk {
		t.Fatalf("text is %d bytes, want more than one chunk", len(text))
	}
	if n := fit(t, build+"str(v)", bytesLimit) - fit(t, build+"v", bytesLimit); n != int64(len(text)) {
		t.Errorf("str(v) costs %d bytes more than v, want %d", n, len(text))
	}
}

// A value that prints far larger than it is stops at the limit, and a
// cancelled task stops rendering at the next chunk.
func TestStrStopsAtTheLimitAndWhenCancelled(t *testing.T) {
	env := value.NewEnvironment()
	env.Set("v", doubling(20)) // about 14 MiB of text
	root := runtime.NewRoot(context.Background(), runtime.NewCapSet(), nil)
	in := New(root)
	program := parse(t, `str(v)`)
	var got value.Object
	used := heapDuring(func() {
		got = in.withScope(frame{runtime.NewScope(root.Ctx), runtime.Root(runtime.Limits{AllocBytes: 1 << 20})},
			func() value.Object { return in.Eval(program, env) })
	})
	wantErrorAt(t, got, allocErr, "1:1")
	// Measured 5.0 MiB: the builder grows by a quarter at a time (a known
	// limit, see the notes), but nowhere near the 14 MiB of the full text.
	if used > 8<<20 {
		t.Errorf("str past a 1 MiB limit allocated %d bytes", used)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	got = str(runtime.NewRoot(ctx, runtime.NewCapSet(), io.Discard), func(int64) error { return nil }, doubling(40))
	if !isError(got) || got.(*value.Error).ErrKind() != runtime.KindCancelled {
		t.Fatalf("got %.80s, want a cancelled error", got.Inspect())
	}
	if d := time.Since(start); d > 30*time.Second {
		t.Errorf("a cancelled str took %s", d)
	}
}

// Interpolation calls str by a second name, which no program can bind.
func TestStrHasAnUnbindableName(t *testing.T) {
	if builtins[ast.StrBuiltin] != builtins["str"] || builtins["str"] == nil {
		t.Fatalf("builtins[%q] is not str", ast.StrBuiltin)
	}
}
```

It uses helpers that already exist in the package's tests: `runWith`, `runLimited`, `wantErrorAt`, `parse`, `heapDuring`, `allocErr` and `doubling` (from `json_budget_test.go`), and pE.3's `fit` and `bytesLimit`.

- [ ] **Step 2: Run it and watch it fail**

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/pE && go test ./arv/evaluator/ -run TestStr`
Expected: FAIL to compile: `undefined: ast.StrBuiltin` and `undefined: str`.

- [ ] **Step 3: Name the builtin for interpolation**

In `arv/ast/string.go`, replace:

```go
type StringLiteral struct {
```

with:

```go
// StrBuiltin is the name an interpolated string calls the str builtin by:
// the parser turns "a ${x}" into "a " + $str(x). No program can bind it,
// since an identifier cannot contain $, so a program's own str never
// changes what interpolation prints.
const StrBuiltin = "$str"

type StringLiteral struct {
```

- [ ] **Step 4: Write `str`**

Create `arv/evaluator/str.go`:

```go
package evaluator

import (
	"strings"

	"github.com/delavalom/arvlang/arv/ast"
	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

func init() {
	b := &value.Builtin{Charging: str}
	builtins["str"] = b
	builtins[ast.StrBuiltin] = b // what "${x}" calls; no program can rebind it
}

// str(v) is v as puts prints it: a string is itself (shared, so it costs
// nothing), any other value its Inspect text. An interpolated string calls
// it for each ${...}, so "a ${x}" prints x as puts would and costs what
// "a " + str(x) costs.
//
// A value can print far larger than it is, so its text is charged to
// alloc_mb as it is built, value.InspectChunk bytes at a time, stopping if
// the task has been cancelled; the rest is charged before the string is
// returned. The result is copied once to its exact size, so a held string
// keeps none of the builder's slack.
func str(t *runtime.Task, charge func(int64) error, args ...value.Object) value.Object {
	if len(args) != 1 {
		return newError("str: want 1 argument, got %d", len(args))
	}
	if s, ok := args[0].(*value.String); ok {
		return s
	}
	var charged int64
	text, err := value.InspectChecked(args[0], func(n int64) error {
		if err := t.Err(); err != nil {
			return &failure{newErrorKind(runtime.KindCancelled, "task %s cancelled: %s", t.ID, err)}
		}
		charged += n
		if err := charge(n); err != nil {
			return &failure{errorOf(err, runtime.KindBudget)}
		}
		return nil
	})
	if err != nil {
		return errorValue(err)
	}
	if err := charge(int64(len(text)) - charged); err != nil {
		return errorOf(err, runtime.KindBudget)
	}
	return &value.String{Value: strings.Clone(text)}
}
```

`failure` and `errorValue` (in `concurrency.go`) carry an Arv error through `InspectChecked`'s Go error, as `Interpreter.puts` does. `applyFunction` settles the task's steps and checks its budgets before any builtin, so `str` starts nothing past a spent budget. `TestBuiltinsNeverPanicOrReturnNil` now also covers `str` and `$str`.

- [ ] **Step 5: Run the tests and watch them pass**

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/pE && go test ./arv/evaluator/`
Expected: PASS.

- [ ] **Step 6: Commit**

Run the pre-commit checks (header), then:

```bash
cd /Users/delavalom/delavalom-labs/arvlang-wt/pE && git add arv/ast/string.go arv/evaluator/str.go arv/evaluator/str_test.go && git commit -m "feat(evaluator): str(v) renders a value as puts prints it

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
