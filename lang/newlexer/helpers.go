package newlexer

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// next returns the next rune in the input.
func (l *Lexer) next() (r rune) {
	if l.pos >= len(l.input) {
		l.width = 0
		return EOF
	}
	r, l.width = utf8.DecodeRuneInString(l.input[l.pos:])
	l.pos += l.width
	return r
}

// ignore skips over the pending input before this point.
func (l *Lexer) ignore() {
	l.start = l.pos
}

// backup steps back one rune.
// Can be called only once per call of next.
func (l *Lexer) backup() {
	l.pos -= l.width
}

// peek returns but does not consume
// the next rune in the input.
func (l *Lexer) peek() rune {
	r := l.next()
	l.backup()
	return r
}

// accept consumes the next rune
// if it's from the valid set.
func (l *Lexer) accept(valid string) bool {
	if strings.ContainsRune(valid, l.next()) {
		return true
	}
	l.backup()
	return false
}

// acceptRun consumes a run of runes from the valid set.
func (l *Lexer) acceptRun(valid string) {
	for strings.ContainsRune(valid, l.next()) {
	}
	l.backup()
}

func isBlock(r rune) bool {
	return r == '{' || r == '}' || r == '[' || r == ']' || r == '(' || r == ')'
}

// isKeyword checks if the given literal is a keyword
func isKeyword(literal string) bool {
	_, ok := Keywords[literal]
	return ok
}

// isOperator checks if the given rune is an operator
func isOperator(r rune) bool {
	switch {
	case r == '+',
		r == '-',
		r == '*',
		r == '/',
		r == '%',
		r == '<',
		r == '>':
		return true
	default:
		return false
	}
}

func isDoubleOperator(a rune, b rune) bool {
	switch {
	case a == '*' && b == '*',
		a == '<' && b == '=',
		a == '>' && b == '=',
		a == '=' && b == '=',
		a == '!' && b == '=':
		return true
	default:
		return false
	}
}

func isSeparator(r rune) bool {
	return r == '.' || r == ';' || r == ',' || r == ':'
}

func isAlphaNumeric(r rune) bool {
	return r == '_' || '0' <= r && r <= '9' || 'a' <= r && r <= 'z' || 'A' <= r && r <= 'Z'
}

func isSpace(r rune) bool {
	return r == ' ' || r == '\t' || r == '\n' || r == '\r'
}

func isLetter(r rune) bool {
	return 'a' <= r && r <= 'z' || 'A' <= r && r <= 'Z' || r == '_'
}

// isCompositeAssignment checks if the given runes are a composite assignment
func isCompositeAssignment(a rune, b rune) bool {
	switch {
	case a == '+' && b == '=',
		a == '-' && b == '=',
		a == '*' && b == '=',
		a == '/' && b == '=':
		return true
	default:
		return false
	}
}

// errorf returns an error token and terminates the scan by passing
// back a nil pointer that will be the next state, terminating l.run.
func (l *Lexer) errorf(format string, args ...interface{}) StateFn {
	l.items <- Item{itemError, fmt.Sprintf(format, args...)}
	return nil
}
