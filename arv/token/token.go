package token

type TokenType string

// Token is one lexeme. Line and Col (both from 1, Col in bytes) are where
// it starts in the source; error messages report them as "line:col".
type Token struct {
	Type    TokenType
	Literal string
	Line    int
	Col     int
}

const (
	ILLEGAL = "ILLEGAL"
	EOF     = "EOF"
	// Identifiers + literals
	IDENT = "IDENT" // add, foobar, x, y, ...
	INT   = "INT"
	// Operators
	ASSIGN   = "="
	PLUS     = "+"
	MINUS    = "-"
	BANG     = "!"
	ASTERISK = "*"
	SLASH    = "/"
	EQ       = "=="
	NOT_EQ   = "!="
	ARROW    = "=>" // a match arm: ok(v) => v
	LT       = "<"
	GT       = ">"
	// Delimiters
	COMMA     = ","
	SEMICOLON = ";"
	LPAREN    = "("
	RPAREN    = ")"
	LBRACE    = "{"
	RBRACE    = "}"
	LBRACKET  = "["
	RBRACKET  = "]"

	// Keywords
	FUNCTION = "FUNCTION"
	LET      = "LET"
	TRUE     = "TRUE"
	FALSE    = "FALSE"
	IF       = "IF"
	ELSE     = "ELSE"
	RETURN   = "RETURN"
	SPAWN    = "SPAWN"
	WITH     = "WITH"
	AWAIT    = "AWAIT"
	SCOPE    = "SCOPE"
	FOR      = "FOR"
	IN       = "IN"
	MATCH    = "MATCH"

	STRING = "STRING"

	COLON = ":" // for hash literals
)
