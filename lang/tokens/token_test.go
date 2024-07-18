package tokens

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewToken(t *testing.T) {
	tp := Type(Operator)
	literal := "+"
	line := 1
	column := 1

	want := tokenStub()

	got := New(tp, literal, line, column)
	assert.Equal(t, want, got)
}

func TestTokenString(t *testing.T) {
	token := tokenStub()
	want := "+"
	got := token.String()

	assert.Equal(t, want, got)
}

func TestTokenPretty(t *testing.T) {
	token := tokenStub()
	want := `<operator@1,1:"+">`
	got := token.Pretty()

	assert.Equal(t, want, got)
}

func TestTokenIs(t *testing.T) {
	token := tokenStub()
	want := true
	got := token.Is(token.Type)

	assert.Equal(t, want, got)
}

func tokenStub() *Token {
	return New(Type(Operator), "+", 1, 1)
}
