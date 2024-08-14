package newlexer

const (
	ILEGAL  = "ilegal"
	EOF     = -1
	NEWLINE = "\n"

	IDENT = "IDENT" // add, foobar, x, y, ...

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

	// Keywords
	FUNCTION = "fn"
	LET      = "var"
	TRUE     = "true"
	FALSE    = "false"
	IF       = "if"
	ELSE     = "else"
	RETURN   = "return"
)

var keywords = map[string]string{
	"fn":     FUNCTION,
	"var":    LET,
	"true":   TRUE,
	"false":  FALSE,
	"if":     IF,
	"else":   ELSE,
	"return": RETURN,
}
