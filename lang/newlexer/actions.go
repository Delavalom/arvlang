package newlexer

import "strings"

// lexInsideAction takes the next character and compares its value with either a number, string, keyword, operator, or identifier.
// Spaces are used to separate and are ignored.
// It returns the stateFn to parse the next token.
func lexInsideAction(l *Lexer) StateFn {
	for {
		if strings.HasPrefix(l.input[l.pos:], RIGHT_BRACE) {
			return lexRightBrace
		}
		switch r := l.next(); {
		case r == EOF || r == '\n':
			return l.errorf("unclosed action")
		case isSpace(r):
			l.ignore()
		case r == '"':
			return lexQuote
		case r == '`':
			return lexRawQuote
		case r == '+' || r == '-' || '0' <= r && r <= '9':
			l.backup()
			return lexNumber
		case isLetter(r):
			l.backup()
		case isAlphaNumeric(r):
			l.backup()
			return lexIdentifier
		}
	}
}
