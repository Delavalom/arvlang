package evaluator

import (
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
				return errorOf(err, runtime.KindBudget)
			}
			newElements := make([]value.Object, length+1)
			copy(newElements, arr.Elements)
			newElements[length] = args[1]
			return &value.Array{Elements: newElements}
		},
	},
	"error": {
		// error(msg) is the program's own failure, of kind user. Like any
		// error it stops the block it is in, unless match catches it. The
		// message is msg itself, not a copy: it is already charged.
		Fn: func(t *runtime.Task, args ...value.Object) value.Object {
			if len(args) != 1 {
				return newError("error: want 1 argument, got %d", len(args))
			}
			msg, ok := args[0].(*value.String)
			if !ok {
				return newError("error: message must be STRING, got %s", args[0].Type())
			}
			return &value.Error{Message: msg.Value, Kind: runtime.KindUser}
		},
	},
	"puts": {
		// Under an interpreter, applyFunction calls Interpreter.puts
		// instead, which renders within the scope's budgets.
		Effect: "puts",
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

// puts prints each argument on its own line. A string prints as it is,
// without a copy.
// Arrays and hashes can print far larger than they are, so their text is
// charged to the allocation budget as it is built, and building stops
// once the task's scope has ended or its deadline has passed: an
// oversized value is an Arv error, not gigabytes of host memory.
func (in *Interpreter) puts(args []value.Object) value.Object {
	check := func(n int64) error {
		if err := in.Task.Err(); err != nil {
			return &failure{newErrorKind(runtime.KindCancelled, "task %s cancelled: %s", in.Task.ID, err)}
		}
		if e := in.charge(n); e != nil {
			return &failure{e}
		}
		return nil
	}
	if in.Task.Log() != nil {
		return in.putsLogged(args, check)
	}
	for _, arg := range args {
		// A string is already charged, and the text is charged as it is
		// built: neither is copied again (fmt.Fprintln copied each into
		// its buffer, uncharged).
		if s, ok := arg.(*value.String); ok {
			runtime.WriteLine(in.Task.Out, s.Value)
			continue
		}
		text, err := value.InspectChecked(arg, check)
		if err != nil {
			return errorValue(err)
		}
		runtime.WriteLine(in.Task.Out, text)
	}
	return NIL
}

// putsLogged is puts in a run with an effect log: it renders the lines
// first, then prints them as one effect (runtime.Task.Print), so a replay
// checks them against the log and prints them in the recorded order. If
// rendering one fails (alloc_mb, a cancellation), the lines before it are
// printed and the failure returned, as an unlogged puts prints each line
// as it goes: recording does not change what a program prints.
func (in *Interpreter) putsLogged(args []value.Object, check func(int64) error) value.Object {
	lines := make([]string, 0, len(args))
	req := make([]value.Object, 0, len(args))
	var failed value.Object
	for _, arg := range args {
		s, ok := arg.(*value.String)
		if !ok {
			text, err := value.InspectChecked(arg, check)
			if err != nil {
				failed = errorValue(err)
				break
			}
			s = &value.String{Value: text}
		}
		lines, req = append(lines, s.Value), append(req, s)
	}
	if failed != nil && len(lines) == 0 {
		return failed
	}
	if err := in.Task.Print(value.EncodeRequest(req), lines); err != nil {
		return errorOf(err, runtime.KindReplay)
	}
	if failed != nil {
		return failed
	}
	return NIL
}
