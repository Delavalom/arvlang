package lexer

import (
	"testing"

	"github.com/delavalom/arvlang/lang/tokens"
	"github.com/stretchr/testify/assert"
)

func _createToken(t tokens.Type, l string) *tokens.Token {
	return &tokens.Token{
		Type:    t,
		Literal: l,
	}
}

func TestTokenizeSymbols(t *testing.T) {
	input := `; , . { } ( ) [ ]`

	expected := []*tokens.Token{
		_createToken(tokens.Semicolon, ";"),
		_createToken(tokens.Comma, ","),
		_createToken(tokens.Dot, "."),
		_createToken(tokens.LeftBrace, "{"),
		_createToken(tokens.RightBrace, "}"),
		_createToken(tokens.LeftParentesis, "("),
		_createToken(tokens.RightParentesis, ")"),
		_createToken(tokens.LeftBracket, "["),
		_createToken(tokens.RightBracket, "]"),
	}

	result, err := Tokenize([]byte(input))

	assert.Equal(t, err, nil)
	for _, token := range expected {
		actualToken := result.Dequeue()
		assert.Equal(t, token.Type, actualToken.Type)
		assert.Equal(t, token.Literal, actualToken.Literal)
	}
}

func TestTokenizeOperators(t *testing.T) {
	input := `+ - * / % ** < <= > >= == !=`

	expected := []*tokens.Token{
		_createToken(tokens.Operator, "+"),
		_createToken(tokens.Operator, "-"),
		_createToken(tokens.Operator, "*"),
		_createToken(tokens.Operator, "/"),
		_createToken(tokens.Operator, "%"),
		_createToken(tokens.Operator, "**"),
		_createToken(tokens.Operator, "<"),
		_createToken(tokens.Operator, "<="),
		_createToken(tokens.Operator, ">"),
		_createToken(tokens.Operator, ">="),
		_createToken(tokens.Operator, "=="),
		_createToken(tokens.Operator, "!="),
	}

	result, err := Tokenize([]byte(input))

	assert.Equal(t, err, nil)
	for _, token := range expected {
		actualToken := result.Dequeue()
		assert.Equal(t, token.Type, actualToken.Type)
		assert.Equal(t, token.Literal, actualToken.Literal)
	}
}

func TestTokenizeAssignment(t *testing.T) {
	input := `= += -= *= /=`

	expected := []*tokens.Token{
		_createToken(tokens.Assignment, "="),
		_createToken(tokens.Assignment, "+="),
		_createToken(tokens.Assignment, "-="),
		_createToken(tokens.Assignment, "*="),
		_createToken(tokens.Assignment, "/="),
	}

	result, err := Tokenize([]byte(input))

	assert.Equal(t, err, nil)
	for _, token := range expected {
		actualToken := result.Dequeue()
		assert.Equal(t, token.Type, actualToken.Type)
		assert.Equal(t, token.Literal, actualToken.Literal)
	}
}

func TestTokenizeSpaces(t *testing.T) {
	input := `. \
	 .           .



	.
	`

	expected := []*tokens.Token{
		_createToken(tokens.Dot, "."),
		_createToken(tokens.Dot, "."),
		_createToken(tokens.Dot, "."),
		_createToken(tokens.Newline, "\n"),
		_createToken(tokens.Dot, "."),
		_createToken(tokens.Newline, "\n"),
	}

	result, err := Tokenize([]byte(input))

	assert.Equal(t, err, nil)
	for _, token := range expected {
		actualToken := result.Dequeue()
		assert.Equal(t, token.Type, actualToken.Type)
		assert.Equal(t, token.Literal, actualToken.Literal)
	}
}

func TestTokenizeIdentifier(t *testing.T) {
	input := `fn module return`

	expected := []*tokens.Token{
		_createToken(tokens.Keyword, "fn"),
		_createToken(tokens.Keyword, "module"),
		_createToken(tokens.Keyword, "return"),
	}

	result, err := Tokenize([]byte(input))

	assert.Equal(t, err, nil)
	for _, token := range expected {
		actualToken := result.Dequeue()
		assert.Equal(t, token.Type, actualToken.Type)
		assert.Equal(t, token.Literal, actualToken.Literal)
	}
}

func TestTokenizeStrings(t *testing.T) {
	input := `"A string with <123123 ☀ ☃ ☂ ☁> \n characters"`

	expected := []*tokens.Token{
		_createToken(tokens.String, "A string with <123123 ☀ ☃ ☂ ☁> \n characters"),
	}

	result, err := Tokenize([]byte(input))

	assert.Equal(t, err, nil)
	for _, token := range expected {
		actualToken := result.Dequeue()
		assert.Equal(t, token.Type, actualToken.Type)
		assert.Equal(t, token.Literal, actualToken.Literal)
	}
}

func TestTokenizeNumbers(t *testing.T) {
	input := `123 1e321 .12 -21e-123`

	expected := []*tokens.Token{
		_createToken(tokens.Number, "123"),
		_createToken(tokens.Number, "1e321"),
		_createToken(tokens.Number, ".12"),
		_createToken(tokens.Operator, "-"),
		_createToken(tokens.Number, "21e-123"),
	}

	result, err := Tokenize([]byte(input))

	assert.Equal(t, err, nil)
	for _, token := range expected {
		actualToken := result.Dequeue()
		assert.Equal(t, token.Type, actualToken.Type)
		assert.Equal(t, token.Literal, actualToken.Literal)
	}
}

func TestTokenizeComments(t *testing.T) {
	input := `// is a comment!`

	expected := []*tokens.Token{
		_createToken(tokens.EOF, ""),
	}

	result, err := Tokenize([]byte(input))

	assert.Equal(t, err, nil)
	for _, token := range expected {
		actualToken := result.Dequeue()
		assert.Equal(t, token.Type, actualToken.Type)
		assert.Equal(t, token.Literal, actualToken.Literal)
	}
}

func TestTokenizeEscapedString(t *testing.T) {
	input := "`A string with <123123 ☀ ☃ ☂ ☁> \n characters`"

	expected := []*tokens.Token{
		_createToken(tokens.String, "A string with <123123 ☀ ☃ ☂ ☁> \n characters"),
	}

	result, err := Tokenize([]byte(input))

	assert.Equal(t, err, nil)
	for _, token := range expected {
		actualToken := result.Dequeue()
		assert.Equal(t, token.Type, actualToken.Type)
		assert.Equal(t, token.Literal, actualToken.Literal)
	}
}

func TestInvalidToken(t *testing.T) {
	input := `�������`
	_, err := Tokenize([]byte(input))
	assert.NotEqual(t, err, nil)
}

func TestInvalidCharacter(t *testing.T) {
	input := `☂`
	_, err := Tokenize([]byte(input))
	assert.NotEqual(t, err, nil)
}
