package main

import (
	"os"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/lexer"
	"github.com/delavalom/arvlang/arv/parser"
)

// The example parses and uses the Phase 3 forms: for, await on an array,
// and scope options.
func TestFanoutExample(t *testing.T) {
	src, err := os.ReadFile("../../examples/fanout.arv")
	if err != nil {
		t.Fatal(err)
	}
	p := parser.New(lexer.New(string(src)))
	p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}
	for _, want := range []string{"scope (tasks: 20", "await for url in urls"} {
		if !strings.Contains(string(src), want) {
			t.Errorf("example lacks %q", want)
		}
	}
	if strings.Contains(string(src), "startAll") {
		t.Error("example still walks arrays with recursion")
	}
}
