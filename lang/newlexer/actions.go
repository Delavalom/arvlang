package newlexer

// lexInsideAction takes the next character and compares its value with either a number, string, keyword, operator, or identifier.
// Spaces are used to separate and are ignored.
// It returns the stateFn to parse the next token.
func lexInsideAction(l *Lexer) StateFn {
	for {
		switch r := l.next(); {
		case r == EOF:
			l.emit(itemEOF)
			return nil
		case r == '/' && l.peek() == '/':
			return lexComment
		case isSpace(r) || r == '\n':
			l.ignore()
			continue
		case isBlock(r):
			l.backup()
			return lexBlock
		case isCompositeAssignment(r, l.peek()):
			l.next()
			return lexCompositeAssignment
		case isDoubleOperator(r, l.peek()):
			l.next()
			return lexOperator
		case isOperator(r):
			return lexOperator
		case r == '=':
			return lexAssignment
		case r == '+' || r == '-' || '0' <= r && r <= '9':
			l.backup()
			return lexNumber
		case r == '"':
			return lexQuote
		case r == '`':
			return lexRawQuote
		case isSeparator(r):
			return lexSeparator
		case isLetter(r):
			l.backup()
			return lexIdentifier
		default:
			l.errorf("unexpected character: %q", r)
			return nil
		}
	}
}
