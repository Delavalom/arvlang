package main

import (
	"fmt"
	"io"
	"os"

	"github.com/delavalom/arvlang/arv"
	"github.com/delavalom/arvlang/arv/repl"
	"github.com/delavalom/arvlang/arv/runtime"
)

const usage = `usage:
  arv                                  start the REPL (no capabilities)
  arv run [--allow kind=scope]... FILE run a program

kinds: net=HOST (or *.domain, *), fs_read=PATH, fs_write=PATH, env=NAME`

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Arv REPL. Ctrl-D to exit.")
		repl.Start(os.Stdin, os.Stdout)
		return
	}
	os.Exit(runCLI(os.Args[1:], os.Stdout, os.Stderr))
}

// runCLI implements every non-REPL command and returns the exit code.
func runCLI(args []string, stdout, stderr io.Writer) int {
	if args[0] != "run" {
		fmt.Fprintln(stderr, usage)
		return 2
	}
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	var caps []runtime.Capability
	rest := args[1:]
	for len(rest) >= 2 && rest[0] == "--allow" {
		c, err := runtime.Parse(rest[1], cwd)
		if err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 2
		}
		caps = append(caps, c)
		rest = rest[2:]
	}
	if len(rest) != 1 {
		fmt.Fprintln(stderr, usage)
		return 2
	}
	src, err := os.ReadFile(rest[0])
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	if _, err := arv.Run(string(src), arv.Options{Caps: caps, Out: stdout}); err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	return 0
}
