package newlexer

const (
	ILEGAL  = "ilegal"
	EOF     = -1
	NEWLINE = "\n"

	IDENTIFIER = "IDENTIFIER" // add, foobar, x, y, ...

	// Blocks
	LEFT_BRACE       = "{"
	RIGHT_BRACE      = "}"
	LEFT_PARENTESIS  = "("
	RIGHT_PARENTESIS = ")"
	LEFT_BRACKET     = "["
	RIGHT_BRACKET    = "]"

	// Special
	COMMENT = "//"

	// Separators
	SEMICOLON = ";"
	COMMA     = ","
	COLON     = ":"
	DOT       = "."

	// Operators
	ASSIGN   = "="
	PLUS     = "+"
	MINUS    = "-"
	BANG     = "!"
	ASTERISK = "*"
	SLASH    = "/"
	EQ       = "=="
	NOT_EQ   = "!="

	// Assignment = "assignment" // =, +=, -=, *=, /=,

	// Comparision Operators
	AND = "&&"
	OR  = "||"

	// Keywords
	FUNCTION = "fn"
	VAR      = "var"
	MODULE   = "module"
	MATCH    = "match"
	TRUE     = "true"
	FALSE    = "false"
	IF       = "if"
	ELSE     = "else"
	RETURN   = "return"
	ELSEIF   = "elseif"
	RANGE    = "range"
	FOR      = "for"
	BREAK    = "break"
	CONTINUE = "continue"
)

var Keywords = map[string]string{
	"fn":       FUNCTION,
	"var":      VAR,
	"module":   MODULE,
	"match":    MATCH,
	"true":     TRUE,
	"false":    FALSE,
	"if":       IF,
	"else":     ELSE,
	"return":   RETURN,
	"elseif":   ELSEIF,
	"range":    RANGE,
	"for":      FOR,
	"break":    BREAK,
	"continue": CONTINUE,
}
