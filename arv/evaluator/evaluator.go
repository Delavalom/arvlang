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
	Task      *runtime.Task
	depth     int                           // Arv call depth, bounded by runtime.MaxDepth
	evalDepth int                           // nested Eval calls, bounded by runtime.MaxEvalDepth
	scopes    []frame                       // innermost last; spawn adds tasks to the top one
	steps     int64                         // nodes evaluated since the last charge to a steps budget
	held      int                           // evaluator depth reserved from the run's pool (runtime.MaxRunEvalDepth)
	collects  map[*runtime.Scope]*collected // the open collect scopes' tasks
}

// depthChunk is how many levels of evaluator depth a task reserves from
// the run's pool at a time, so the shared counter is touched only every
// depthChunk levels. A task gives a chunk back once it is two chunks
// below what it holds, so a stack swinging across one boundary does not
// reserve and return on every call. A task never deeper than 256 levels
// holds at most four chunks, runtime.LiveTaskDepth, which the pool keeps
// for every task the live-task cap allows.
const depthChunk = 64

// stepChunk is how many nodes a task evaluates between charges to its
// steps budget: an atomic add per limited level every stepChunk nodes, not
// every node. A task may run up to stepChunk-1 nodes past a limit before
// it notices; closing a scope charges the rest, so a single task's total
// is checked exactly. A task also charges what it owes before every
// builtin and every spawn (settleSteps), so no effect starts, in it or in
// a task it spawns, once the steps it took to get there pass the limit.
// settleSteps also checks, without charging, that no other task has spent
// the steps or bytes they share.
const stepChunk = 256

// frame is the per-scope state of one open scope: its tasks and its level
// of the budget tree. It lives here, not in the Task, because it changes
// as scopes open and close.
type frame struct {
	scope  *runtime.Scope
	budget *runtime.Budget
}

// New returns an interpreter bound to task t.
func New(t *runtime.Task) *Interpreter { return &Interpreter{Task: t} }

// Eval runs node in a root scope with no capabilities, writing to stdout.
// Kept so the existing tests and the REPL keep working unchanged.
func Eval(node ast.Node, env *value.Environment) value.Object {
	root := runtime.NewRoot(context.Background(), runtime.NewCapSet(), os.Stdout)
	return New(root).Run(node, env)
}

// Eval evaluates node. It is the only way the evaluator recurses, so it
// bounds the total recursion: Arv calls and expression nesting multiply
// (a function nesting 200 levels deep, recursing 9999 times, is two
// million levels), and past runtime.MaxEvalDepth it fails with an Arv
// error instead of letting the Go stack overflow, which is fatal.
func (in *Interpreter) Eval(node ast.Node, env *value.Environment) value.Object {
	return in.enter(node, nil, env)
}

// enter is Eval for node, run by the tree-walker (c nil) or as c, its
// compiled code (exec). Both do the same checks in the same order, so a
// node costs the same depth and steps, and its errors get the same
// position, whichever engine runs it (Phase 8.4).
func (in *Interpreter) enter(node ast.Node, c *code, env *value.Environment) value.Object {
	if in.evalDepth >= runtime.MaxEvalDepth {
		return newError("stack overflow: evaluation nested deeper than %d levels", runtime.MaxEvalDepth)
	}
	if in.evalDepth >= in.held {
		if e := in.reserveDepth(); e != nil {
			return e
		}
	}
	in.evalDepth++
	var result value.Object
	if c == nil {
		result = in.eval(node, env)
	} else if e := in.tick(); e != nil {
		result = e
	} else {
		result = c.run(in, env)
	}
	in.evalDepth--
	if in.held-in.evalDepth > 2*depthChunk && len(in.scopes) > 0 {
		in.releaseDepth(in.top().budget, in.held-depthChunk)
	}
	if e, ok := result.(*value.Error); ok && (e.Pos == "" || e.Task == "") {
		return at(e, node, in.Task.ID)
	}
	return result
}

// reserveDepth takes another chunk of the run's evaluator depth. Outside
// any scope there is no run budget, and only MaxEvalDepth applies.
func (in *Interpreter) reserveDepth() *value.Error {
	if len(in.scopes) == 0 {
		return nil
	}
	if b := in.top().budget; !b.Depth(depthChunk) {
		return newError("stack overflow: the run's live tasks together nested deeper than %d levels", b.DepthLimit())
	}
	in.held += depthChunk
	return nil
}

// releaseDepth gives the run's pool, through b, all held depth above keep.
func (in *Interpreter) releaseDepth(b *runtime.Budget, keep int) {
	if in.held > keep {
		b.Depth(int64(keep - in.held))
		in.held = keep
	}
}

// at returns e with what it lacks filled in: the position where node
// starts (if node has one) and task, the ID of the task it arose in. It
// copies, keeping every other field (the kind among them): an error value
// may already be shared (a task's result is read by every task that
// awaits it), so it is never mutated. e itself comes back if it lacks
// neither.
func at(e *value.Error, node ast.Node, task string) *value.Error {
	if e.Pos != "" && e.Task != "" {
		return e
	}
	c := *e
	if c.Task == "" {
		c.Task = task
	}
	if line, col := ast.Pos(node); c.Pos == "" && line != 0 {
		c.Pos = fmt.Sprintf("%d:%d", line, col)
	}
	return &c
}

// tick is called once per evaluated node. It stops a cancelled task and
// counts the node against the steps budget.
func (in *Interpreter) tick() *value.Error {
	if err := in.Task.Tick(); err != nil {
		return newErrorKind(runtime.KindCancelled, "task %s cancelled: %s", in.Task.ID, err)
	}
	if in.steps++; in.steps >= stepChunk {
		return in.chargeSteps()
	}
	return nil
}

// chargeSteps charges the nodes evaluated since the last charge to the
// innermost scope's level of the budget tree (and so to every level above).
func (in *Interpreter) chargeSteps() *value.Error {
	n := in.steps
	in.steps = 0
	if len(in.scopes) == 0 {
		return nil
	}
	if err := in.top().budget.Steps(n); err != nil {
		return errorOf(err, runtime.KindBudget)
	}
	return nil
}

// settleSteps charges the steps not yet charged before an effect may
// happen. With none to charge, it still fails if the steps budget is
// already spent, and in any case if the bytes budget is: another task
// under the same scope may have passed the limit, and its error may not
// have cancelled the scope yet.
func (in *Interpreter) settleSteps() *value.Error {
	if len(in.scopes) == 0 {
		return in.chargeSteps()
	}
	b := in.top().budget
	if in.steps > 0 {
		if e := in.chargeSteps(); e != nil {
			return e
		}
	} else if err := b.StepsSpent(); err != nil {
		return errorOf(err, runtime.KindBudget)
	}
	if err := b.AllocSpent(); err != nil {
		return errorOf(err, runtime.KindBudget)
	}
	return nil
}

func (in *Interpreter) eval(node ast.Node, env *value.Environment) value.Object {
	if e := in.tick(); e != nil {
		return e
	}
	return in.evalNode(node, env)
}

// evalNode is the tree-walker's case for node, after eval's tick.
func (in *Interpreter) evalNode(node ast.Node, env *value.Environment) value.Object {
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
		return in.evalInfixChain(node, env)
	case *ast.BlockStatement:
		return in.evalBlockStatement(node, env)
	case *ast.IfExpression:
		return in.evalIfExpression(node, env)
	case *ast.LetStatement:
		val := in.Eval(node.Value, env)
		if isError(val) {
			return val
		}
		if e := in.charge(bindingSize); e != nil {
			return e
		}
		env.Set(node.Name.Value, val)
	case *ast.Identifier:
		return evalIdentifier(node, env, in.Task)
	case *ast.FunctionLiteral:
		return in.alloc(&value.Function{Parameters: node.Parameters, Env: env, Body: node.Body}, functionSize)
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
		return in.alloc(&value.Array{Elements: elements}, arrayElemSize*int64(len(elements)))
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
	case *ast.ScopeExpression:
		return in.evalScope(node, env)
	case *ast.SpawnExpression:
		return in.evalSpawn(node, env)
	case *ast.AwaitExpression:
		return in.evalAwait(node, env)
	case *ast.ForExpression:
		return in.evalFor(node, env)
	case *ast.MatchExpression:
		return in.evalMatch(node, env)
	}
	// let statements and any node without a value evaluate to nil. Eval
	// never returns Go nil, so builtins and operators need no nil checks.
	return NIL
}

// evalInfixChain evaluates node and the infix expressions down its left
// spine, as in 1 + 2 + 3 + ..., without recursing once per operator. The
// parser does not count such chains as nesting (see parser.MaxNesting), so
// they can be arbitrarily long; recursion could exhaust the Go stack. Each
// spine node still costs one Tick, as if Eval had visited it.
func (in *Interpreter) evalInfixChain(node *ast.InfixExpression, env *value.Environment) value.Object {
	spine := []*ast.InfixExpression{node}
	for {
		next, ok := spine[len(spine)-1].Left.(*ast.InfixExpression)
		if !ok {
			break
		}
		if e := in.tick(); e != nil {
			return e
		}
		spine = append(spine, next)
	}
	left := in.Eval(spine[len(spine)-1].Left, env)
	for i := len(spine) - 1; i >= 0 && !isError(left); i-- {
		right := in.Eval(spine[i].Right, env)
		if isError(right) {
			return right
		}
		// A concatenation is charged before it is built: tasks that wake
		// together must not each build a string the budget will refuse.
		if n, ok := concatSize(spine[i].Operator, left, right); ok {
			if e := in.charge(n); e != nil {
				return e
			}
		}
		left = evalInfixExpression(spine[i].Operator, left, right)
	}
	return left
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
		if e := in.charge(envSize + bindingSize*int64(len(args))); e != nil {
			return e
		}
		in.depth++
		defer func() { in.depth-- }()
		extendedEnv := extendFunctionEnv(fn, args)
		evaluated := in.Eval(fn.Body, extendedEnv)
		return unwrapReturnValue(evaluated)
	case *value.Builtin:
		// Builtins are where effects happen, and the context and the steps
		// budget are otherwise checked only every few hundred nodes: stop
		// here if the scope has ended or its steps or bytes are spent, so
		// nothing is written or printed after its deadline or past a limit.
		if e := in.settleSteps(); e != nil {
			return e
		}
		if err := in.Task.Err(); err != nil {
			return newErrorKind(runtime.KindCancelled, "task %s cancelled: %s", in.Task.ID, err)
		}
		switch fn {
		case putsBuiltin:
			return in.puts(args)
		case askBuiltin:
			return in.ask(args)
		case agentBuiltin:
			return in.agent(args)
		}
		if fn.Effect != "" {
			return in.callEffect(fn, args)
		}
		if fn.Charging != nil {
			return fn.Charging(in.Task, in.chargeBytes, args...)
		}
		result := fn.Fn(in.Task, args...)
		if fn.Shares || isError(result) {
			return result
		}
		return in.alloc(result, allocSize(result))
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

// newError is a runtime error: the program is wrong (types, arguments,
// overflow). Errors with another cause use newErrorKind or errorOf.
func newError(format string, a ...interface{}) *value.Error {
	return newErrorKind(runtime.KindRuntime, format, a...)
}

// newErrorKind is an error of the given kind (a runtime.Kind* constant).
func newErrorKind(kind, format string, a ...interface{}) *value.Error {
	return &value.Error{Message: fmt.Sprintf(format, a...), Kind: kind}
}

// errorOf is err as an Arv error value with err's message, of err's kind
// (runtime.ErrorKind), or fallback if err does not say.
func errorOf(err error, fallback string) *value.Error {
	return &value.Error{Message: err.Error(), Kind: runtime.ErrorKind(err, fallback)}
}

func isError(obj value.Object) bool {
	if obj != nil {
		return obj.Type() == value.ERROR_VAL
	}
	return false
}
