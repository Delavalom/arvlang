# Arvlang 

> [!NOTE]
> purpose is to enable intermediate code generation for time-consuming framework development

## Lexer

![lexer explanation of how it works](/assets/image.png)

# Literals

- Let
- Numeral
- Boolean
- String
- Functions
- Returns

# Statements

* Prefix expressions

<prefix operator> <expression>

* Infix expressions

<expression> <infix operator> <expression>

* If Expression

if <condition> { <consequence> } else { <alternative> }

NOTE: Both <consequence> and <alternative> being <block statement>

* Function Expression

fn (<parameter n>, ...) <block statement>

* Call Expression

<expression>(<comma separated expressions>)

* For Expression

for <condition> <block statement>

* Range Expression

for <expression> range <iterator> <block statement>

* Match Expression

match <expression> {
    OK: <consequence>
    ERROR: <alternative>
}