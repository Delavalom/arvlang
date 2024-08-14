package newlexer

import "strings"

func lexLeftBrace(l *Lexer) StateFn {
	l.pos += len(LEFT_BRACE)
	l.emit(itemLeftBrace)
	return lexInsideAction // Now inside { }.
}

func lexRightBrace(l *Lexer) StateFn {
	l.pos += len(LEFT_BRACE)
	l.emit(itemLeftBrace)
	return lexInsideAction // Now inside { }.
}

func lexText(l *Lexer) StateFn {
	for {
		if strings.HasPrefix(l.input[l.pos:], LEFT_BRACE) {
			if l.pos > l.start {
				l.emit(itemSymbol)
			}
			return lexLeftBrace
		}
		if l.next() == EOF {
			break
		}
	}
	// Correctly reached EOF.
	if l.pos > l.start {
		l.emit(itemSymbol)
	}
	l.emit(itemEOF) // Useful to make EOF a token.
	return nil      // Stop the run loop.
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

func lexKeyword(l *Lexer) StateFn {
	l.acceptRun("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")
	if isSpace(l.peek()) {
		l.emit(itemKeyword)
		return lexInsideAction
	}
	return lexInsideAction
}

func lexIdentifier(l *Lexer) StateFn {
	l.acceptRun("_abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")
	if isSpace(l.peek()) {
		l.emit(itemIdentifier)
		return lexInsideAction
	}
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
