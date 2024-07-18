package lexer

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/delavalom/arvlang/lang/queue"
	"github.com/delavalom/arvlang/lang/syntax"
	"github.com/delavalom/arvlang/lang/tokens"
)

type char struct {
	Rune   rune
	Size   int
	Line   int
	Column int
}

func NewChar(r rune, size, line, column int) *char {
	return &char{
		Rune:   r,
		Size:   size,
		Line:   line,
		Column: column,
	}
}

// Is checks if the given rune is equal to the rune in the char struct
func (p *char) Is(r rune) bool {
	return p.Rune == r
}

type TokenQueue = *queue.Queue[tokens.Token]
type CharQueue = *queue.Queue[char]

type Lexer struct {
	input      []byte
	tokenQueue TokenQueue
	charQueue  CharQueue
	errors     []string
	line       int
	column     int
	cursor     int
	eof        *tokens.Token
}

func NewLexer(input []byte) *Lexer {
	tokenQueue := queue.NewQueue[tokens.Token]()
	charQueue := queue.NewQueue[char]()
	line := 1
	column := 1
	lexer := &Lexer{
		input:      input,
		tokenQueue: tokenQueue,
		charQueue:  charQueue,
		errors:     []string{},
		line:       line,
		column:     column,
		cursor:     0,
	}

	return lexer
}

func Tokenize(input []byte) (TokenQueue, error) {
	lexer := NewLexer(input)

	for {
		token := lexer.parseNextToken()
		lexer.tokenQueue.Enqueue(token)

		if token.Is(tokens.EOF) {
			break
		}
	}

	return lexer.tokenQueue, lexer.GetError()
}

// TooManyErrors checks if the lexer has too many errors by checking
// the length of the errors slice in the lexer
func (l *Lexer) TooManyErrors() bool {
	return len(l.errors) >= 10
}

// HasError checks if the lexer has errors by checking the length of the errors slice
func (l *Lexer) HasError() bool {
	return len(l.errors) > 0
}

// GetError returns an error if the lexer has errors
func (l *Lexer) GetError() error {
	if l.HasError() {
		return fmt.Errorf("tokenizer errors: \n- %s", strings.Join(l.errors, "\n- "))
	}
	return nil
}

// RegisterError registers an error in the lexer by appending the error message
func (l *Lexer) RegisterError(e string, c *char) {
	if l.TooManyErrors() {
		return
	}

	l.errors = append(l.errors, fmt.Sprintf("%s at %d:%d", e, c.Line, c.Column))

	if l.TooManyErrors() {
		l.errors = append(l.errors, "too many errors, aborting")
	}
}

func (l *Lexer) PeekChar() *char {
	return l.PeekCharN(0)
}

func (l *Lexer) PeekCharN(n int) *char {
	if l.charQueue.Len() <= n {
		l.charQueue.Enqueue(l.parseNextChar())
	}

	return l.charQueue.PeekN(n)
}

// isWhitespace checks if the given rune is a whitespace by comparing
func (l *Lexer) isWhitespace(r rune) bool {
	return r == '\n' || r == '\r' || r == '\t' || r == ' '
}

// isLetter checks if the given rune is a letter by comparing
// the Unicode value of r with the Unicode values of the characters 'aA' and 'zZ' or '_' or '$'.
func (l *Lexer) isLetter(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_' || r == '$'
}

// isDigit checks if the given rune is a digit by comparing
// the Unicode value of r with the Unicode values of the characters '0' and '9'.
func (l *Lexer) isDigit(r rune) bool {
	return r >= '0' && r <= '9'
}

// isEOF checks if the given rune is the end of the file
func (l *Lexer) isEOF(r rune) bool {
	return r == 0
}

// isKeyword checks if the given literal is a keyword
func (l *Lexer) isKeyword(literal string) bool {
	return slices.Contains(syntax.Keywords, literal)
}

// isOperatorKeyword checks if the given literal is an operator keyword
func (l *Lexer) isOperatorKeyword(literal string) bool {
	return slices.Contains(syntax.Operators, literal)
}

// isDoubleOperator checks if the given runes are a double operator
func (l *Lexer) isDoubleOperator(a rune, b rune) bool {
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

// isOperator checks if the given rune is an operator
func (l *Lexer) isOperator(r rune) bool {
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

// isCompositeAssignment checks if the given runes are a composite assignment
func (l *Lexer) isCompositeAssignment(a rune, b rune) bool {
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

// parseNextChar parses the next character given the cursor position in the input
// enqueues the character in the charQueue, and returns the character
func (l *Lexer) parseNextChar() *char {
	for l.cursor < len(l.input) {
		if l.TooManyErrors() {
			return NewChar(0, 0, l.line, l.column)
		}
		r, size := utf8.DecodeRune(l.input[l.cursor:])
		newChar := NewChar(r, size, l.line, l.column)
		l.column++
		if r == utf8.RuneError {
			l.RegisterError("invalid UTF-8 character", newChar)
			l.cursor++
			continue
		}
		if r == '\n' {
			l.line++
			l.column = 1
		}
		l.cursor += size

		return newChar
	}

	return NewChar(0, 0, l.line, l.column)
}

func (l *Lexer) parseComment() {
	l.charQueue.Dequeue()
	for {
		c := l.PeekChar()
		if c.Is('\n') || l.isEOF(c.Rune) {
			break
		}
		l.charQueue.Dequeue()
	}
}

func (l *Lexer) parseWhitespaces() *tokens.Token {
	nl := false
	first := l.PeekChar()
	for {
		currentChar := l.PeekChar()
		if !l.isWhitespace(currentChar.Rune) {
			break
		}
		if currentChar.Is('\n') {
			nl = true
		}
		l.charQueue.Dequeue()
	}
	if nl {
		return tokens.New(tokens.Newline, "\n", first.Line, first.Column)
	}
	return nil
}

func (l *Lexer) parseIdentifier() *tokens.Token {
	var builder strings.Builder

	first := l.charQueue.Dequeue()
	builder.WriteRune(first.Rune)
	for {
		currentChar := l.PeekChar()

		if l.isLetter(currentChar.Rune) || l.isDigit(currentChar.Rune) {
			builder.WriteRune(currentChar.Rune)
		} else {
			break
		}

		l.charQueue.Dequeue()
	}

	literal := builder.String()
	tokenType := tokens.Identifier
	if l.isOperatorKeyword(literal) {
		tokenType = tokens.Operator
	} else if l.isKeyword(literal) {
		tokenType = tokens.Keyword
	}
	return tokens.New(tokens.Type(tokenType), literal, first.Line, first.Column)
}

func (l *Lexer) parseNumber() *tokens.Token {
	var builder strings.Builder
	dot := false
	exp := false

	first := l.PeekChar()
	for {
		c := l.PeekChar()
		if c.Is('.') {
			if dot {
				l.RegisterError("unexpected '.'", c)
				l.charQueue.Dequeue()
				continue
			}
			dot = true
			builder.WriteRune(c.Rune)
		} else if c.Is('e') || c.Is('E') {
			if exp {
				l.RegisterError("unexpected 'e'", c)
				l.charQueue.Dequeue()
				continue
			}
			exp = true
			builder.WriteRune(c.Rune)
			if l.PeekCharN(1).Is('+') || l.PeekCharN(1).Is('-') {
				l.charQueue.Dequeue()
				builder.WriteRune(l.PeekChar().Rune)
			}
		} else if l.isDigit(c.Rune) {
			builder.WriteRune(c.Rune)

		} else {
			break
		}

		l.charQueue.Dequeue()
	}

	literal := builder.String()

	return tokens.New(tokens.Number, literal, first.Line, first.Column)
}

func (l *Lexer) parseString(char rune) *tokens.Token {
	var builder strings.Builder
	escape := false

	first := l.charQueue.Dequeue()
	for {
		currentChar := l.PeekChar()

		if !escape && currentChar.Is('\\') {
			escape = true
			l.charQueue.Dequeue()
			continue

		} else if l.isEOF(currentChar.Rune) || !escape && currentChar.Is(char) {
			break
		} else if currentChar.Is('\n') {
			l.RegisterError("unexpected newline", currentChar)
			l.charQueue.Dequeue()
			continue
		} else if escape && !currentChar.Is(char) {
			escape = false
			r, err := strconv.Unquote(`"\` + string(currentChar.Rune) + `"`)
			if err != nil {
				l.RegisterError(err.Error(), currentChar)
				l.charQueue.Dequeue()
				continue
			}
			currentChar.Rune = rune(r[0])
		}

		builder.WriteRune(currentChar.Rune)
		l.charQueue.Dequeue()
	}

	l.charQueue.Dequeue()
	return tokens.New(tokens.String, builder.String(), first.Line, first.Column)
}

func (l *Lexer) parseEscapedString() *tokens.Token {
	var builder strings.Builder

	first := l.charQueue.Dequeue()
	for {
		currentChar := l.PeekChar()
		nextChar := l.PeekCharN(1)

		if l.isEOF(currentChar.Rune) || currentChar.Is('`') {
			break
		}

		if currentChar.Is('\\') && nextChar.Is('`') {
			l.charQueue.Dequeue()
			currentChar = nextChar
		}

		builder.WriteRune(currentChar.Rune)
		l.charQueue.Dequeue()
	}

	l.charQueue.Dequeue()
	return tokens.New(tokens.String, builder.String(), first.Line, first.Column)
}

func (l *Lexer) parseBacklash() {
	newline := false

	l.charQueue.Dequeue()
	for {
		currentChar := l.PeekChar()

		if !l.isWhitespace(currentChar.Rune) {
			break
		}

		if currentChar.Is('\n') {
			if newline {
				break
			}
			newline = true
		}

		l.charQueue.Dequeue()
	}
}

// parseNextToken parses the next token given the queue
func (l *Lexer) parseNextToken() *tokens.Token {
	if l.eof != nil {
		return l.eof
	}
	var token *tokens.Token

	for {
		var currentChar = l.PeekChar()
		if l.TooManyErrors() || currentChar.Rune == 0 {
			l.eof = tokens.New(tokens.EOF, "", l.line, l.column)
			return l.eof
		}

		currentRune := l.PeekCharN(0).Rune
		nextRune := l.PeekCharN(1).Rune

		switch {
		case currentChar.Is('/') && nextRune == '/':
			l.parseComment()
			continue
		case currentChar.Is('\\'):
			l.parseBacklash()
			continue
		case l.isWhitespace(currentChar.Rune):
			token = l.parseWhitespaces()
			if token == nil {
				continue
			}
		case l.isCompositeAssignment(currentRune, nextRune):
			token = tokens.New(tokens.Assignment, string(currentRune)+string(nextRune), currentChar.Line, currentChar.Column)
			l.charQueue.Dequeue()
			l.charQueue.Dequeue()
		case l.isDoubleOperator(currentRune, nextRune):
			token = tokens.New(tokens.Operator, string(currentRune)+string(nextRune), currentChar.Line, currentChar.Column)
			l.charQueue.Dequeue()
			l.charQueue.Dequeue()
		case l.isOperator(currentRune):
			token = tokens.New(tokens.Operator, string(currentRune), currentChar.Line, currentChar.Column)
			l.charQueue.Dequeue()
		case currentChar.Is('='):
			token = tokens.New(tokens.Assignment, "=", currentChar.Line, currentChar.Column)
			l.charQueue.Dequeue()
		case currentChar.Is(';'):
			token = tokens.New(tokens.Semicolon, ";", currentChar.Line, currentChar.Column)
			l.charQueue.Dequeue()
		case currentChar.Is(','):
			token = tokens.New(tokens.Comma, ",", currentChar.Line, currentChar.Column)
			l.charQueue.Dequeue()
		case currentChar.Is('{'):
			token = tokens.New(tokens.LeftBrace, "{", currentChar.Line, currentChar.Column)
			l.charQueue.Dequeue()
		case currentChar.Is('}'):
			token = tokens.New(tokens.RightBrace, "}", currentChar.Line, currentChar.Column)
			l.charQueue.Dequeue()
		case currentChar.Is('('):
			token = tokens.New(tokens.LeftParentesis, "(", currentChar.Line, currentChar.Column)
			l.charQueue.Dequeue()
		case currentChar.Is(')'):
			token = tokens.New(tokens.RightParentesis, ")", currentChar.Line, currentChar.Column)
			l.charQueue.Dequeue()
		case currentChar.Is('['):
			token = tokens.New(tokens.LeftBracket, "[", currentChar.Line, currentChar.Column)
			l.charQueue.Dequeue()
		case currentChar.Is(']'):
			token = tokens.New(tokens.RightBracket, "]", currentChar.Line, currentChar.Column)
			l.charQueue.Dequeue()
		case currentChar.Is('.') && !l.isDigit(nextRune):
			token = tokens.New(tokens.Dot, ".", currentChar.Line, currentChar.Column)
			l.charQueue.Dequeue()
		case l.isLetter(currentChar.Rune):
			token = l.parseIdentifier()
		case l.isDigit(currentChar.Rune) || currentChar.Is('.') && l.isDigit(l.charQueue.PeekN(1).Rune):
			token = l.parseNumber()
		case currentChar.Is('"'):
			token = l.parseString(currentChar.Rune)
		case currentChar.Is('`'):
			token = l.parseEscapedString()
		default:
			l.RegisterError(fmt.Sprintf("invalid character '%c'", currentChar.Rune), currentChar)
			l.charQueue.Dequeue()
			continue
		}
		break
	}

	return token
}
