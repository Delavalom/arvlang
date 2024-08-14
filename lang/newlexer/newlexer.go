package newlexer

// Lexer holds the state of the scanner.
type Lexer struct {
	name  string    // used only for error reports
	input string    // the string being scanned
	start int       // start position of this item
	pos   int       // current position in the input
	width int       // width of last rune read from input
	items chan Item // channel of scanned items
}

// StateFn represents the state of the scanner
// as a function that returns the next state.
type StateFn func(*Lexer) StateFn

// run lexes the input by executing state functions until
// the state is nil.
func (l *Lexer) run() {
	for state := lexText; state != nil; {
		state = state(l)
	}
	close(l.items) // No more tokens will be delivered.
}

// emit passes an item back to the client.
func (l *Lexer) emit(t itemType) {
	l.items <- Item{Type: t, Value: l.input[l.start:l.pos]}
	l.start = l.pos
}

// lex creates a new scanner for the input string.
func lex(name, input string) *Lexer {
	l := &Lexer{
		name:  name,
		input: input,
		items: make(chan Item),
	}
	go l.run()
	return l
}
