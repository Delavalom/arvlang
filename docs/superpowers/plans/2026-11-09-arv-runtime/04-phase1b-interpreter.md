# Phase 1b: An interpreter bound to a task

**Goal:** Replace the package-level `Eval(node, env)` with an `Interpreter` that belongs to one `runtime.Task`, so builtins can check capabilities and honour cancellation. The existing 500+ lines of evaluator tests must keep passing without edits.

**Invariants touched:** I1 (builtins receive the task, which is the only route to the OS).

---

### Task 1.3: `Interpreter`, task-aware builtins, depth limit

**Files:**
- Modify: `arv/value/value.go` (`BuiltinFunction` signature, import `arv/runtime`)
- Modify: `arv/evaluator/builtins.go` (new signature; `puts` writes to `t.Out`)
- Modify: `arv/evaluator/eval_functions.go` (5 functions become methods)
- Replace: `arv/evaluator/evaluator.go`
- Test: `arv/evaluator/interpreter_test.go`

- [ ] **Step 1: Write the failing tests**

`runWith` is the helper every later evaluator test uses. It runs a program as the root task with the given capabilities and captures `puts` output.

```go
package evaluator

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/lexer"
	"github.com/delavalom/arvlang/arv/parser"
	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// runWith evaluates input as the root task holding caps; it returns the
// result and everything puts printed.
func runWith(t *testing.T, input string, caps ...runtime.Capability) (value.Object, string) {
	t.Helper()
	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}
	var out bytes.Buffer
	root := runtime.NewRoot(context.Background(), runtime.NewCapSet(caps...), &out)
	return New(root).Eval(program, value.NewEnvironment()), out.String()
}

func TestPutsWritesToTaskOutput(t *testing.T) {
	_, out := runWith(t, `puts("hi"); puts(1 + 1);`)
	if out != "hi\n2\n" {
		t.Fatalf("output = %q", out)
	}
}

func TestRunawayRecursionIsAnArvError(t *testing.T) {
	got, _ := runWith(t, `let f = fn(n) { f(n + 1) }; f(0);`)
	errObj, ok := got.(*value.Error)
	if !ok || !strings.Contains(errObj.Message, "stack overflow") {
		t.Fatalf("got %v, want stack overflow error", got)
	}
}

func TestWrongArgCountIsAnError(t *testing.T) {
	got, _ := runWith(t, `let f = fn(a, b) { a }; f(1);`)
	if _, ok := got.(*value.Error); !ok {
		t.Fatalf("got %v, want error", got)
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `go test ./arv/evaluator/`
Expected: build failure, `undefined: New`.

- [ ] **Step 3: Change the builtin signature in `arv/value/value.go`**

Add `"github.com/delavalom/arvlang/arv/runtime"` to the imports, then replace the `BuiltinFunction` line with:

```go
// BuiltinFunction receives the calling task so effects can check its
// capabilities, honour its cancellation and write to its output.
type BuiltinFunction func(t *runtime.Task, args ...Object) Object
```

- [ ] **Step 4: Update `arv/evaluator/builtins.go`**

Add the `arv/runtime` import, then rewrite every builtin's signature and make `puts` write to the task's output:

```bash
sed -i '' 's/Fn: func(args \.\.\.value\.Object) value\.Object {/Fn: func(t *runtime.Task, args ...value.Object) value.Object {/' arv/evaluator/builtins.go
sed -i '' 's/fmt\.Println(arg\.Inspect())/fmt.Fprintln(t.Out, arg.Inspect())/' arv/evaluator/builtins.go
```

- [ ] **Step 5: Turn the recursive helpers in `arv/evaluator/eval_functions.go` into methods**

Two commands. The first turns exactly five recursive helpers (`evalProgram`, `evalBlockStatement`, `evalIfExpression`, `evalExpressions`, `evalHashLiteral`) into methods on `*Interpreter`. The second points every `Eval(` call in the file at the method:

```bash
sed -i '' -E 's/^func (evalProgram|evalBlockStatement|evalIfExpression|evalExpressions|evalHashLiteral)\(/func (in *Interpreter) \1(/' arv/evaluator/eval_functions.go
sed -i '' -E 's/([^.A-Za-z])Eval\(/\1in.Eval(/g' arv/evaluator/eval_functions.go
```

The second command also rewrites three comments to say `calls in.Eval()`. That's harmless.

The pure helpers (`evalPrefixExpression`, `evalInfixExpression`, `evalIdentifier`, `evalIndexExpression`, …) stay plain functions.

- [ ] **Step 6: Replace `arv/evaluator/evaluator.go`**

```go
package evaluator

import (
	"context"
	"fmt"
	"os"

	"github.com/delavalom/arvlang/arv/ast"
	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

var (
	NIL   = &value.Nil{}
	TRUE  = &value.Boolean{Value: true}
	FALSE = &value.Boolean{Value: false}
)

// Interpreter evaluates Arv code on behalf of exactly one runtime.Task.
// It is not safe for concurrent use; every spawned task gets its own.
type Interpreter struct {
	Task  *runtime.Task
	depth int
}

// New returns an interpreter bound to task t.
func New(t *runtime.Task) *Interpreter { return &Interpreter{Task: t} }

// Eval runs node with no capabilities, writing to stdout. Kept so the
// existing tests and the REPL keep working unchanged.
func Eval(node ast.Node, env *value.Environment) value.Object {
	root := runtime.NewRoot(context.Background(), runtime.NewCapSet(), os.Stdout)
	return New(root).Eval(node, env)
}

func (in *Interpreter) Eval(node ast.Node, env *value.Environment) value.Object {
	if err := in.Task.Tick(); err != nil {
		return newError("task %s cancelled: %s", in.Task.ID, err)
	}
	switch node := node.(type) {
	case *ast.Program:
		return in.evalProgram(node, env)
	case *ast.ExpressionStatement:
		return in.Eval(node.Expression, env)
	case *ast.IntegerLiteral:
		return &value.Integer{Value: node.Value}
	case *ast.Boolean:
		return nativeBoolToBooleanObject(node.Value)
	case *ast.ReturnStatement:
		val := in.Eval(node.ReturnValue, env)
		if isError(val) {
			return val
		}
		return &value.ReturnValue{Value: val}
	case *ast.PrefixExpression:
		right := in.Eval(node.Right, env)
		if isError(right) {
			return right
		}
		return evalPrefixExpression(node.Operator, right)
	case *ast.InfixExpression:
		left := in.Eval(node.Left, env)
		if isError(left) {
			return left
		}
		right := in.Eval(node.Right, env)
		if isError(right) {
			return right
		}
		return evalInfixExpression(node.Operator, left, right)
	case *ast.BlockStatement:
		return in.evalBlockStatement(node, env)
	case *ast.IfExpression:
		return in.evalIfExpression(node, env)
	case *ast.LetStatement:
		val := in.Eval(node.Value, env)
		if isError(val) {
			return val
		}
		env.Set(node.Name.Value, val)
	case *ast.Identifier:
		return evalIdentifier(node, env)
	case *ast.FunctionLiteral:
		return &value.Function{Parameters: node.Parameters, Env: env, Body: node.Body}
	case *ast.CallExpression:
		function := in.Eval(node.Function, env)
		if isError(function) {
			return function
		}
		args := in.evalExpressions(node.Arguments, env)
		if len(args) == 1 && isError(args[0]) {
			return args[0]
		}
		return in.applyFunction(function, args)
	case *ast.ArrayLiteral:
		elements := in.evalExpressions(node.Elements, env)
		if len(elements) == 1 && isError(elements[0]) {
			return elements[0]
		}
		return &value.Array{Elements: elements}
	case *ast.IndexExpression:
		left := in.Eval(node.Left, env)
		if isError(left) {
			return left
		}
		index := in.Eval(node.Index, env)
		if isError(index) {
			return index
		}
		return evalIndexExpression(left, index)
	case *ast.StringLiteral:
		return &value.String{Value: node.Value}
	case *ast.HashLiteral:
		return in.evalHashLiteral(node, env)
	}

	return nil
}

func (in *Interpreter) applyFunction(fn value.Object, args []value.Object) value.Object {
	switch fn := fn.(type) {
	case *value.Function:
		if len(args) != len(fn.Parameters) {
			return newError("wrong number of arguments: want=%d, got=%d", len(fn.Parameters), len(args))
		}
		if in.depth >= runtime.MaxDepth {
			return newError("stack overflow: call depth exceeded %d", runtime.MaxDepth)
		}
		in.depth++
		defer func() { in.depth-- }()
		extendedEnv := extendFunctionEnv(fn, args)
		evaluated := in.Eval(fn.Body, extendedEnv)
		return unwrapReturnValue(evaluated)
	case *value.Builtin:
		return fn.Fn(in.Task, args...)
	default:
		return newError("not a function: %s", fn.Type())
	}
}

func extendFunctionEnv(fn *value.Function, args []value.Object) *value.Environment {
	env := value.NewEnclosedEnvironment(fn.Env)
	for paramIdx, param := range fn.Parameters {
		env.Set(param.Value, args[paramIdx])
	}
	return env
}

func unwrapReturnValue(obj value.Object) value.Object {
	if returnValue, ok := obj.(*value.ReturnValue); ok {
		return returnValue.Value
	}
	return obj
}

func nativeBoolToBooleanObject(input bool) *value.Boolean {
	if input {
		return TRUE
	}
	return FALSE
}

func newError(format string, a ...interface{}) *value.Error {
	return &value.Error{Message: fmt.Sprintf(format, a...)}
}

func isError(obj value.Object) bool {
	if obj != nil {
		return obj.Type() == value.ERROR_VAL
	}
	return false
}
```

What changed versus the Monkey version:
- `Eval` is a method that calls `Task.Tick()` first, so a cancelled task stops at the next node.
- `applyFunction` checks the argument count. The old code panicked with index-out-of-range on `f(1)` for `fn(a, b)`.
- `applyFunction` enforces `runtime.MaxDepth`.
- The package-level `Eval` wraps a capability-less root task, so the old tests and the REPL keep working.

- [ ] **Step 7: Run everything**

Run: `go vet ./... && go test ./...`
Expected: all `ok`, including the untouched `evaluator_test.go`.

- [ ] **Step 8: Commit**

```bash
git add arv
git commit -m "refactor(evaluator): Interpreter bound to a runtime.Task

Builtins now receive the calling task. Adds an argument-count check
and a call-depth limit so runaway recursion is an Arv error."
```
