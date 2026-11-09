package main

import (
	"fmt"
	"os"

	"github.com/delavalom/arvlang/arv/repl"
)

func main() {
	fmt.Println("Arv REPL. Ctrl-D to exit.")
	repl.Start(os.Stdin, os.Stdout)
}
