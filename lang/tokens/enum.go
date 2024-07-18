package tokens

import "strings"

type Type string

const (
	_ Type = ""

	// Spacing
	EOF     = "eof"
	Newline = "newline" // "\n"

	// Variable-related
	Keyword    = "keyword"
	Identifier = "identifier" // [a-zA-Z_$][a-zA-Z0-9_$]*
	Number     = "number"     // 123, 123.456, 123e456, -.2
	String     = "string"     // '.*'

	// Operators
	Operator   = "operator"   // +, -, *, /, %, **, <, <=, >, >=, ==, !=
	Assignment = "assignment" // =, +=, -=, *=, /=,

	// Separators
	Semicolon = "semicolon" // ";"
	Comma     = "comma"     // ","
	Colon     = "colon"     // ":"
	Dot       = "dot"       // "."

	// Special
	Comment = "comment" // "//"

	// Blocks
	LeftBrace       = "leftBrace"       // "{"
	RightBrace      = "rightBrace"      // "}"
	LeftParentesis  = "leftParentesis"  // "("
	RightParentesis = "rightParentesis" // ")"
	LeftBracket     = "leftBracket"     // "["
	RightBracket    = "rightBracket"    // "]"
)

func JoinTypes(types ...Type) string {
	var builder strings.Builder
	for _, t := range types {
		builder.WriteString(string(t) + ", ")
	}
	return builder.String()[:builder.Len()-2]
}
