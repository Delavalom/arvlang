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
