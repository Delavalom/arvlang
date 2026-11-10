package evaluator

import (
	"errors"
	"fmt"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

var builtins = map[string]*value.Builtin{
	"len": {
		Fn: func(t *runtime.Task, args ...value.Object) value.Object {
			if len(args) != 1 {
				return newError("wrong number of arguments. got=%d, want=1", len(args))
			}
			switch arg := args[0].(type) {
			case *value.String:
				return &value.Integer{Value: int64(len(arg.Value))}
			case *value.Array:
				return &value.Integer{Value: int64(len(arg.Elements))}
			default:
				return newError("argument to `len` not supported, got %s", args[0].Type())
			}
		},
	},
	"last": {
		Shares: true,
		Fn: func(t *runtime.Task, args ...value.Object) value.Object {
			if len(args) != 1 {
				return newError("wrong number of arguments. got=%d, want=1", len(args))
			}
			if args[0].Type() != value.ARRAY_VAL {
				return newError("argument to `last` must be ARRAY, got %s", args[0].Type())
			}
			arr := args[0].(*value.Array)
			length := len(arr.Elements)
			if length > 0 {
				return arr.Elements[length-1]
			}
			return NIL
		},
	},
	"push": {
		// push charges its copy before it makes it, as a concatenation
		// does (concatSize), so a refused push builds nothing.
		Charging: func(t *runtime.Task, charge func(int64) error, args ...value.Object) value.Object {
			if len(args) != 2 {
				return newError("wrong number of arguments. got=%d, want=2",
					len(args))
			}
			if args[0].Type() != value.ARRAY_VAL {
				return newError("argument to `push` must be ARRAY, got %s", args[0].Type())
			}
			arr := args[0].(*value.Array)
			length := len(arr.Elements)
			if err := charge(arrayElemSize * int64(length+1)); err != nil {
				return newError("%s", err)
			}
			newElements := make([]value.Object, length+1)
			copy(newElements, arr.Elements)
			newElements[length] = args[1]
			return &value.Array{Elements: newElements}
		},
	},
	"puts": {
		// Under an interpreter, applyFunction calls Interpreter.puts
		// instead, which renders within the scope's budgets.
		Fn: func(t *runtime.Task, args ...value.Object) value.Object {
			for _, arg := range args {
				fmt.Fprintln(t.Out, arg.Inspect())
			}
			return NIL
		},
	},
}

// putsBuiltin is the builtin that applyFunction runs as Interpreter.puts.
var putsBuiltin = builtins["puts"]

// puts prints each argument on its own line. A string prints as it is.
// Arrays and hashes can print far larger than they are, so their text is
// charged to the allocation budget as it is built, and building stops
// once the task's scope has ended or its deadline has passed: an
// oversized value is an Arv error, not gigabytes of host memory.
func (in *Interpreter) puts(args []value.Object) value.Object {
	check := func(n int64) error {
		if err := in.Task.Err(); err != nil {
			return fmt.Errorf("task %s cancelled: %w", in.Task.ID, err)
		}
		if e := in.charge(n); e != nil {
			return errors.New(e.Message)
		}
		return nil
	}
	for _, arg := range args {
		if s, ok := arg.(*value.String); ok { // already charged; no copy
			fmt.Fprintln(in.Task.Out, s.Value)
			continue
		}
		text, err := value.InspectChecked(arg, check)
		if err != nil {
			return newError("%s", err)
		}
		fmt.Fprintln(in.Task.Out, text)
	}
	return NIL
}
