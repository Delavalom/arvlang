package repl

import (
	"bufio"
	"context"
	"io"

	"github.com/delavalom/arvlang/arv/ast"
	"github.com/delavalom/arvlang/arv/evaluator"
	"github.com/delavalom/arvlang/arv/lexer"
	"github.com/delavalom/arvlang/arv/parser"
	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

const PROMPT = ">> "

// Start reads lines from in and evaluates each one, writing prompts,
// results and puts output to out. The whole session is one root task with
// no capabilities, so a task spawned on one line can be awaited on a later
// one (only its spawner may await it). Each line is still its own root
// scope: it does not return until the tasks it spawned have finished.
func Start(in io.Reader, out io.Writer) {
	scanner := bufio.NewScanner(in)
	env := value.NewEnvironment()
	interp := evaluator.New(runtime.NewRoot(context.Background(), runtime.NewCapSet(), out))
	for {
		io.WriteString(out, PROMPT)
		scanned := scanner.Scan()
		if !scanned {
			return
		}
		line := scanner.Text()
		l := lexer.New(line)
		p := parser.New(l)
		program := p.ParseProgram()
		if len(p.Errors()) != 0 {
			printParserErrors(out, p.Errors())
			continue
		}
		evaluated := interp.Run(program, env)
		if evaluated == evaluator.NIL && quiet(program) {
			continue
		}
		io.WriteString(out, evaluated.Inspect())
		io.WriteString(out, "\n")
	}
}

// quiet reports whether a nil result goes unprinted: an empty line or one
// ending in a let statement. That is what the REPL printed nothing for
// when such lines evaluated to Go nil.
func quiet(program *ast.Program) bool {
	n := len(program.Statements)
	if n == 0 {
		return true
	}
	_, isLet := program.Statements[n-1].(*ast.LetStatement)
	return isLet
}

func printParserErrors(out io.Writer, errors []string) {
	for _, msg := range errors {
		io.WriteString(out, "\t"+msg+"\n")
	}
}
