package newlexer

import (
	"strings"
)

func lexBlock(l *Lexer) StateFn {
	switch l.next() {
	case '{':
		l.emit(itemLeftBrace)
		return lexInsideAction
	case '}':
		l.emit(itemRightBrace)
		return lexInsideAction
	case '(':
		l.emit(itemLeftParentesis)
		return lexInsideAction
	case ')':
		l.emit(itemRightParentesis)
		return lexInsideAction
	case '[':
		l.emit(itemLeftBracket)
		return lexInsideAction
	case ']':
		l.emit(itemRightBracket)
		return lexInsideAction
	}
	return l.errorf("unexpected block")
}

func lexNumber(l *Lexer) StateFn {
	// Optional leading sign.
	l.accept("+-")
	// Is it hex?
	digits := "0123456789"
	if l.accept("0") && l.accept("xX") {
		digits = "0123456789abcdefABCDEF"
	}
	l.acceptRun(digits)

	if l.accept(".") {
		l.acceptRun(digits)
	}
	if l.accept("eE") {
		l.accept("+-")
		l.acceptRun("0123456789")
	}
	// Is it imaginary?
	l.accept("i")
	// Next thing mustn't be alphanumeric.
	if isAlphaNumeric(l.peek()) {
		l.next()
		return l.errorf("bad number syntax: %q", l.input[l.start:l.pos])
	}
	l.emit(itemNumber)
	return lexInsideAction
}

func lexQuote(l *Lexer) StateFn {
	for {
		switch l.next() {
		case '\\':
			l.next()
		case '"':
			l.emit(itemString)
			return lexInsideAction
		case EOF:
			return l.errorf("unterminated string")
		}
	}
}

func lexRawQuote(l *Lexer) StateFn {
	for {
		switch l.next() {
		case '`':
			l.emit(itemRawString)
			return lexInsideAction
		case EOF:
			return l.errorf("unterminated raw string")
		}
	}
}

func lexOperator(l *Lexer) StateFn {
	l.emit(itemOperator)
	return lexInsideAction
}

func lexSeparator(l *Lexer) StateFn {
	l.emit(itemSeparator)
	return lexInsideAction
}

func lexCompositeAssignment(l *Lexer) StateFn {
	l.emit(itemAssisgnment)
	return lexInsideAction
}

func lexAssignment(l *Lexer) StateFn {
	l.emit(itemAssisgnment)
	return lexInsideAction
}

func lexIdentifier(l *Lexer) StateFn {
	var builder strings.Builder

	for {
		r := l.next()

		if isAlphaNumeric(r) {
			builder.WriteRune(r)
		} else {
			break
		}
	}
	l.backup()
	literal := builder.String()

	if isKeyword(literal) {
		l.emit(itemKeyword)
	} else {
		l.emit(itemIdentifier)
	}
	return lexInsideAction
}

func lexComment(l *Lexer) StateFn {
	for {
		switch l.next() {
		case '\n':
			l.emit(itemComment)
			return lexInsideAction
		case EOF:
			l.emit(itemComment)
			return nil
		}
	}
}
