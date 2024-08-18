package newlexer

type itemType int

const (
	itemError           itemType = iota // error occurred; value is text of error
	itemEOF                             // end of file
	itemSymbol                          // a symbol
	itemOperator                        // an operator
	itemIdentifier                      // an identifier
	itemAssisgnment                     // an assignment
	itemSeparator                       // a separator
	itemNumber                          // a number
	itemString                          // a string
	itemKeyword                         // a keyword
	itemRange                           // a range
	itemRawString                       // a raw string
	itemComment                         // a comment
	itemIf                              // if keyword
	itemElseIf                          // elseif keyword
	itemElse                            // else keyword
	itemLeftBrace                       // left brace
	itemRightBrace                      // right brace
	itemLeftParentesis                  // left parentesis
	itemRightParentesis                 // right parentesis
	itemLeftBracket                     // left bracket
	itemRightBracket                    // right bracket
)

type Item struct {
	Type  itemType
	Value string
}

// func (i Item) String() string {
// 	switch i.Type {
// 	case itemEOF:
// 		return "EOF"
// 	case itemError:
// 		return i.Value
// 	}
// 	// truncate if item is too large
// 	if len(i.Value) > 10 {
// 		return fmt.Sprintf("%.10q...", i.Value)
// 	}
// 	return fmt.Sprintf("%q", i.Value)
// }
