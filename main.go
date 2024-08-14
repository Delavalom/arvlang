package main

import (
	"fmt"
	"os"
	"os/user"

	"github.com/delavalom/arvlang/lang/newlexer"
)

func main() {
	user, err := user.Current()
	if err != nil {
		panic(err)
	}
	fmt.Printf("Hello %s! This is the Arv programming language!\n", user.Username)
	fmt.Printf("Feel free to type in commands\n")
	newlexer.Start(os.Stdin, os.Stdout)
}
