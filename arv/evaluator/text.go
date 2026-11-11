package evaluator

import (
	"strings"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// pieceSize is what one piece of split or lines costs: its array slot and
// a new String header. The piece's bytes are not copied: it shares the
// input string, which is already charged.
const pieceSize = arrayElemSize + 16

func init() {
	builtins["split"] = &value.Builtin{Charging: split}
	builtins["lines"] = &value.Builtin{Charging: lines}
}

// split(s, sep) is the array of the pieces of s between each sep, as Go's
// strings.Split: split("a,b,", ",") is ["a", "b", ""]. sep must not be
// empty. It charges every piece before it builds the array.
func split(t *runtime.Task, charge func(int64) error, args ...value.Object) value.Object {
	if len(args) != 2 {
		return newError("split: want 2 arguments, got %d", len(args))
	}
	s, ok1 := args[0].(*value.String)
	sep, ok2 := args[1].(*value.String)
	if !ok1 || !ok2 {
		return newError("split: want (STRING, STRING), got (%s, %s)", args[0].Type(), args[1].Type())
	}
	if sep.Value == "" {
		return newError("split: separator is empty")
	}
	n := strings.Count(s.Value, sep.Value) + 1
	if err := charge(pieceSize * int64(n)); err != nil {
		return errorOf(err, runtime.KindBudget)
	}
	out := make([]value.Object, 0, n)
	rest := s.Value
	for {
		i := strings.Index(rest, sep.Value)
		if i < 0 {
			break
		}
		out = append(out, &value.String{Value: rest[:i]})
		rest = rest[i+len(sep.Value):]
	}
	out = append(out, &value.String{Value: rest})
	return &value.Array{Elements: out}
}

// lines(s) is the array of the lines of s with surrounding white space
// trimmed, leaving out blank lines, so a file's trailing newline or a
// "\r\n" ending adds nothing. It charges the most pieces s can have
// before it builds the array.
func lines(t *runtime.Task, charge func(int64) error, args ...value.Object) value.Object {
	text, errObj := oneString("lines", args)
	if errObj != nil {
		return errObj
	}
	n := strings.Count(text, "\n") + 1
	if err := charge(pieceSize * int64(n)); err != nil {
		return errorOf(err, runtime.KindBudget)
	}
	out := make([]value.Object, 0, n)
	for len(text) > 0 {
		line := text
		if i := strings.IndexByte(text, '\n'); i >= 0 {
			line, text = text[:i], text[i+1:]
		} else {
			text = ""
		}
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, &value.String{Value: line})
		}
	}
	return &value.Array{Elements: out}
}
