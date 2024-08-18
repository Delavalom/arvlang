package newlexer

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func _createItem(t itemType, v string) *Item {
	return &Item{
		Type:  t,
		Value: v,
	}
}

func TestTokenizeSeparators(t *testing.T) {
	input := `; , . :`

	expected := []*Item{
		_createItem(itemSeparator, ";"),
		_createItem(itemSeparator, ","),
		_createItem(itemSeparator, "."),
		_createItem(itemSeparator, ":"),
	}

	result := lex("Separators", input)

	for _, item := range expected {
		actualItem := <-result.items
		assert.Equal(t, item.Type, actualItem.Type)
		assert.Equal(t, item.Value, actualItem.Value)
	}
}

func TestTokenizeBlocks(t *testing.T) {
	input := `{ } ( ) [ ]`

	expected := []*Item{
		_createItem(itemLeftBrace, "{"),
		_createItem(itemRightBrace, "}"),
		_createItem(itemLeftParentesis, "("),
		_createItem(itemRightParentesis, ")"),
		_createItem(itemLeftBracket, "["),
		_createItem(itemRightBracket, "]"),
	}

	result := lex("Blocks", input)

	for _, item := range expected {
		actualItem := <-result.items
		assert.Equal(t, item.Type, actualItem.Type)
		assert.Equal(t, item.Value, actualItem.Value)
	}
}

func TestTokenizeOperators(t *testing.T) {
	input := `+ - * / % ** < <= > >= == !=`
	expected := []*Item{
		_createItem(itemOperator, "+"),
		_createItem(itemOperator, "-"),
		_createItem(itemOperator, "*"),
		_createItem(itemOperator, "/"),
		_createItem(itemOperator, "%"),
		_createItem(itemOperator, "**"),
		_createItem(itemOperator, "<"),
		_createItem(itemOperator, "<="),
		_createItem(itemOperator, ">"),
		_createItem(itemOperator, ">="),
		_createItem(itemOperator, "=="),
		_createItem(itemOperator, "!="),
	}
	result := lex("Operators", input)

	for _, item := range expected {
		actualToken := <-result.items
		assert.Equal(t, item.Type, actualToken.Type)
		assert.Equal(t, item.Value, actualToken.Value)
	}
}

func TestTokenizeAssignments(t *testing.T) {
	input := `= += -= *= /=`

	expected := []*Item{
		_createItem(itemAssisgnment, "="),
		_createItem(itemAssisgnment, "+="),
		_createItem(itemAssisgnment, "-="),
		_createItem(itemAssisgnment, "*="),
		_createItem(itemAssisgnment, "/="),
	}

	result := lex("Assignment", input)

	for _, item := range expected {
		actualItem := <-result.items
		assert.Equal(t, item.Type, actualItem.Type)
		assert.Equal(t, item.Value, actualItem.Value)
	}
}

func TestTokenizeIdentifier(t *testing.T) {
	input := `add foo bar`

	expected := []*Item{
		_createItem(itemIdentifier, "add"),
		_createItem(itemIdentifier, "foo"),
		_createItem(itemIdentifier, "bar"),
	}

	result := lex("Identifiers", input)

	for _, item := range expected {
		actualItem := <-result.items
		assert.Equal(t, item.Type, actualItem.Type)
		assert.Equal(t, item.Value, actualItem.Value)
	}
}

func TestTokenizeKeywords(t *testing.T) {
	input := `var fn module return if elseif else range match for break continue true false`

	expected := []*Item{
		_createItem(itemKeyword, "var"),
		_createItem(itemKeyword, "fn"),
		_createItem(itemKeyword, "module"),
		_createItem(itemKeyword, "return"),
		_createItem(itemKeyword, "if"),
		_createItem(itemKeyword, "elseif"),
		_createItem(itemKeyword, "else"),
		_createItem(itemKeyword, "range"),
		_createItem(itemKeyword, "match"),
		_createItem(itemKeyword, "for"),
		_createItem(itemKeyword, "break"),
		_createItem(itemKeyword, "continue"),
		_createItem(itemKeyword, "true"),
		_createItem(itemKeyword, "false"),
	}

	result := lex("Keywords", input)

	for _, item := range expected {
		actualItem := <-result.items
		assert.Equal(t, item.Type, actualItem.Type)
		assert.Equal(t, item.Value, actualItem.Value)
	}
}

func TestTokenizeString(t *testing.T) {
	input := `"A string with <123123 ☀ ☃ ☂ ☁> \n characters"`
	expected := []*Item{
		_createItem(itemString, `"A string with <123123 ☀ ☃ ☂ ☁> \n characters"`),
	}
	result := lex("Strings", input)
	for _, item := range expected {
		actualToken := <-result.items
		assert.Equal(t, item.Type, actualToken.Type)
		assert.Equal(t, item.Value, actualToken.Value)
	}
}

func TestTokenizeEscapedString(t *testing.T) {
	input := "`A string with <123123 ☀ ☃ ☂ ☁> \n characters`"
	expected := []*Item{
		_createItem(itemRawString, "`A string with <123123 ☀ ☃ ☂ ☁> \n characters`"),
	}
	result := lex("strings", input)

	for _, item := range expected {
		actualItem := <-result.items
		assert.Equal(t, item.Type, actualItem.Type)
		assert.Equal(t, item.Value, actualItem.Value)
	}
}

func TestTokenizeNumbers(t *testing.T) {
	input := `123 1e321 0.12 -21e-123`
	expected := []*Item{
		_createItem(itemNumber, "123"),
		_createItem(itemNumber, "1e321"),
		_createItem(itemNumber, "0.12"),
		_createItem(itemOperator, "-"),
		_createItem(itemNumber, "21e-123"),
	}
	result := lex("Numbers", input)

	for _, item := range expected {
		actualItem := <-result.items
		assert.Equal(t, item.Type, actualItem.Type)
		assert.Equal(t, item.Value, actualItem.Value)
	}
}

func TestTokenizeComments(t *testing.T) {
	input := `// is a comment!`
	expected := []*Item{
		_createItem(itemComment, "// is a comment!"),
	}
	result := lex("Comments", input)

	for _, item := range expected {
		actualItem := <-result.items
		assert.Equal(t, item.Type, actualItem.Type)
		assert.Equal(t, item.Value, actualItem.Value)
	}
}

func TestTokenizeSpaces(t *testing.T) {
	input := `. 
	 .           .

	.
	`

	expected := []*Item{
		_createItem(itemSeparator, "."),
		_createItem(itemSeparator, "."),
		_createItem(itemSeparator, "."),
		_createItem(itemSeparator, "."),
	}

	result := lex("Spaces", input)

	for _, item := range expected {
		actualItem := <-result.items
		assert.Equal(t, item.Type, actualItem.Type)
		assert.Equal(t, item.Value, actualItem.Value)
	}
}

func TestInvalidCharacter(t *testing.T) {
	input := `☂`
	result := lex("Invalid Character", input)
	item := _createItem(itemError, "unexpected character: '☂'")
	actualItem := <-result.items

	assert.Equal(t, actualItem.Type, item.Type)
	assert.Equal(t, actualItem.Value, item.Value)
}
