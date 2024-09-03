package evaluator

import (
	"fmt"

	"github.com/delavalom/arvlang/lang/monkeylexer/value"
)

var builtins = map[string]*value.Builtin{
	"len": {
		Fn: func(args ...value.Object) value.Object {
			if len(args) != 1 {
				return newError("wrong number of arguments. got=%d, want=1", len(args))
			}
			switch arg := args[0].(type) {
			case *value.String:
				return &value.Integer{Value: int64(len(arg.Value))}
			default:
				return newError("argument to `len` not supported, got %s", args[0].Type())
			}
		},
	},
	"last": {
		Fn: func(args ...value.Object) value.Object {
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
		Fn: func(args ...value.Object) value.Object {
			if len(args) != 2 {
				return newError("wrong number of arguments. got=%d, want=2",
					len(args))
			}
			if args[0].Type() != value.ARRAY_VAL {
				return newError("argument to `push` must be ARRAY, got %s", args[0].Type())
			}
			arr := args[0].(*value.Array)
			length := len(arr.Elements)
			newElements := make([]value.Object, length+1)
			copy(newElements, arr.Elements)
			newElements[length] = args[1]
			return &value.Array{Elements: newElements}
		},
	},
	"puts": {
		Fn: func(args ...value.Object) value.Object {
			for _, arg := range args {
				fmt.Println(arg.Inspect())
			}
			return NIL
		},
	},
}
