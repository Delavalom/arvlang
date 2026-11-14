# Phase 5E: ergonomics (as built)

Built from `pE-plan-1.md` to `pE-plan-8.md`; their decisions and deviations are in `pE-plan-1.md`. Design: the "If 1-5 hold but 6 doesn't" paragraph of `12-phase5-gate.md`, and the owner's decision under criterion 6 in `gate-report.md` (yes to 6, the ergonomics items as a parallel phase, no re-run of the gate).

## What each form means

The parser desugars every new form into node types that already existed. **No new AST node type was added.**

| Written | Means | Built as |
|---|---|---|
| `r.up` | `r["up"]` | `IndexExpression` (token: the dot), key a `StringLiteral` placed at the name |
| `a <= b`, `a >= b` | integer comparison | `InfixExpression`, operator `<=` / `>=` |
| `a && b` | `if (a) { b } else { false }` | `IfExpression`, `Token.Type == token.AND`, placed where `a` starts |
| `a \|\| b` | `if (a) { true } else { b }` | `IfExpression`, `Token.Type == token.OR` |
| `"a ${x} b"` | `"a " + str(x) + " b"` | `InfixExpression` `+` chain of `StringLiteral` pieces and `CallExpression`s of the identifier `$str` |

**For code that walks the AST** (Phase 8.1 `arv check`, Phase 6 effect recording, Phase 8.4 speed work):
- `InfixExpression.Operator` can be `<=` or `>=`; the evaluator compares integers only, as for `<` and `>`.
- An `IfExpression` whose token is `&&` or `||` came from those operators. Its blocks hold one `ExpressionStatement` each; evaluate it as any `if`. It prints as `(a && b)`.
- An interpolated string calls `$str` (`ast.StrBuiltin`), registered in the evaluator's `builtins` map beside `str` (same `*value.Builtin`). It is pure: no effect, no capability. `arv check` should know it as a builtin, and an interpolated argument is not a literal: `http_get("http://${host}/")` is an infix expression, "not statically known".
- A literal whose only `${` are escaped (`"\${x}"`) stays one `StringLiteral`, but it is the first whose `Token.Literal` (the source text, `\${x}`, which `TokenLiteral()` and `String()` show) differs from its `Value` (`${x}`, what runs). Read `Value` for what the program uses, for example a literal path or host.
- New tokens: `LT_EQ`, `GT_EQ`, `AND`, `OR`, `DOT`. New constructor `lexer.NewAt(input, line, col)`.

## Interpolation: the syntax and its escape

- **`${expr}`** inside any string literal; **`\${` is a literal `${`**. Nothing else in a string changes: `$`, `{`, `$x` and `$ {` are literal, and a backslash still means itself everywhere else (strings have no escapes).
- **Every existing string keeps its meaning.** On the base (860a158), `git grep -n -F '${' -- '*.go' '*.arv' README.md` printed nothing: no string literal in any Go test, e2e case, example or the README held `${` (so none held `\${` either). Only the bench shell scripts and the plan docs contain `${`, and they are not Arv.
- **Why not `$${` as the escape** (Terraform's): `"$${amount}"` reads as a dollar sign and a value, but would print `${amount}`.
- **The expression** ends at the `}` that balances the `${`. It cannot hold a string literal: the lexer is unchanged and ends the string at its first quote, so `"${f("x")}"` is `unterminated ${ in string`. Bind the value first, or use `.field`. A backslash right before a value is written `"\" + "${v}"`. An empty `${}` and a `${x; y}` are parse errors. A `//` comment inside `${...}` runs to the end of its line, and a brace in it does not count (`"${ x // }\n}"` is `$str(x)`).
- **Rendering matches `puts`** through the new builtin `str(v)`: a string is itself; any other value is its `Inspect` text (`-42`, `true`, `nil`, `[1, x, [true]]`, `{k: [1]}`, a function's source). A hash's pairs come in Go map order, as `puts` prints them, until Phase 6 makes `Inspect` deterministic.
- **`$str` cannot be rebound.** The lexer cannot produce `$` in an identifier, so `let str = fn(x) { "mine" }` changes `str(1)` but not `"${1}"`.
- **Cost.** `"a ${x} b"` is charged exactly what `"a " + str(x) + " b"` is (the trees differ only in the name): the same steps, and the same bytes: `str` of a string costs nothing (it is shared), of any other value its text's length, charged as it is built (64 KiB at a time) and once in total; each concatenation then costs its result, charged before it is built. The equivalent form calls `str` even on a string value, so `"x ${s} y"` takes 2 steps more than `"x " + s + " y"` (11 against 9 for `let s = "abc";`: the `$str` name and its call), with the same bytes; the parser cannot know a value's type. Tested with `fit` and `sameCost` (`arv/evaluator/fit_test.go`), and exactly: `let x = 12345; "n=${x}!"` costs 48 + 5 + 7 + 8 bytes.
- **Positions.** The expression is lexed in place (`lexer.NewAt`), so an error inside `${...}` is at its node's real `line:col`, across lines too. A value that cannot be rendered (`alloc_mb` runs out in `str`) is reported at the `$` of its `${`. A concatenation that runs out is reported at its leftmost piece, as the hand-written form's infix error is at its leftmost operand: where the string starts (its opening quote) when it starts with text, and at the `$` of the first `${` when it starts with one (`"${s}${s}"` at `2:2`, like `str(s) + str(s)` at its `str`).

## `&&` and `||`

- They mean the `if` the gate's author wrote by hand. As in `if`, only `true` counts as true, and the result is the right side's value when the right side runs: `1 && true` is `false`, `true && 5` is `5`, and `5 || 6` is `6`. **`||` is not a default operator**: `r.name || "anon"` is `"anon"` even when `r.name` is `"bob"`, because only `true` decides. Write the `if` for a default.
- Precedence: `||` below `&&` below `==` / `!=` below `<` `>` `<=` `>=`.
- **Short circuit.** When the left side decides, the right side is never evaluated: no call, no effect (an `http_get` there is not attempted and is not denied), no steps and no bytes for it. The constant branch costs 3 steps (block, statement, literal), as in the hand-written `if`. Tested: `false && <big>` costs what `false && 1` costs, and each form costs what its `if` costs.
- **Nesting.** Each `&&` and `||` counts one level toward `parser.MaxNesting` (10000), since each nests the AST through the next one's condition: a chain of more than 10000 is the parse error `nesting deeper than 10000 levels`. A `+` chain is still unlimited.

## `.field` and `<=` / `>=`

- After the dot, any identifier or keyword names the field (`r.in`, `r.match`). Values, missing keys (`nil`), errors and their positions (the start of `r`), steps and bytes are those of `r["field"]` (tested pair by pair). A field chain counts toward `MaxNesting` like an index chain.
- `<=` and `>=` compare integers. Strings give `unknown operator: STRING <= STRING`, mixed types `type mismatch`, as `<` and `>` do.

## Changed behaviour of programs that did not parse or failed before

- `1.5` is now `expected a field name after ., got INT instead (at 1:3)`; before, `no prefix parse function for ILLEGAL found`. A lone `&` or `|` is still `ILLEGAL`.
- A program that used `str` without binding it now calls the builtin instead of failing with `identifier not found: str`, as with any new builtin.

## Known limits

- **`str` of a large non-string value allocates about 6.8 times its text while it runs**: the `strings.Builder` inside `value.InspectChecked` grows by a quarter at a time, then the result is copied to its exact size. Measured (darwin/arm64, Go 1.26.2): a 3.67 MB text allocated 24.8 MB; a 229 KB text 1.42 MB (6.2x); under a 1 MiB `alloc_mb` a 14 MiB render stops after allocating 5.0 MiB. The charge is exactly the text, and the overhead is a constant factor per call, so this is Minor under the `alloc_mb` rule. `puts` has the same builder. A chunked writer like `json`'s would bring it to about 2x.
- No string literal inside `${...}` (above).
- A hash interpolates in random pair order until Phase 6.
- **A `return` that ends the left side of `&&` or `||`, or a `${...}`, is dropped** (a base bug, not this phase's). `evalIfExpression` neither unwraps nor passes on a `ReturnValue` condition, so `fn() { (if (true) { return 5 } else { 0 }) && 2 }()` is `false`, and `"${if (true) { return 5 } else { 0 }}"` renders `5` without returning. The hand-written equivalents behave the same on the base (`if (if (true) { return 5 } else { 0 }) { 2 } else { false }` is `false`), so the equivalence holds; the new forms only make the bug easier to reach (`+` reports `type mismatch: RETURN_VALUE + INTEGER`). Recorded as the skipped test `TestReturnInConditionIsKept`; the fix belongs in `evalIfExpression`, for every `if`. **Fixed in the final fixer round** ([`16zg`](16zg-final-fixes.md)) for a condition, `&&` and `||`, in both engines; a `return` inside `${...}` is still a known limit there.

## Tests added

| Guarantee | Tests |
|---|---|
| G1 existing meaning | the whole suite, unchanged; the grep above |
| G2 short circuit | `TestAndOrShortCircuit`, `TestOrIsNotADefault` |
| G3 `.field` is `["field"]` | `TestParseFieldIsIndex`, `TestFieldNodeShape`, `TestFieldAccess`, `TestFieldErrorsMatchIndexErrors`, `TestFieldCostsWhatIndexCosts` |
| G4 rendering and cost | `TestStrRendersAsPuts`, `TestStrChargesItsText`, `TestInterpolationRendersAsPuts`, `TestInterpolationCostsWhatConcatenationCosts`, `TestAndOrCostWhatIfCosts` |
| G5 positions | `TestInterpolationPositions`, `TestInterpolationSyntaxErrors`, `TestInterpolationErrorPositions`, `TestAndOrNodeShape`, `TestAndOrErrorPositions`, `TestFieldSyntaxErrors`, `TestInterpolationConcatErrorAtLeftmostPiece`, `TestInterpolationCommentBraces`, `TestInterpolationCommentBraceIsSkipped` |
| G6 budgets and capabilities | `TestAndOrRightSideIsCapabilityChecked`, `TestInterpolatingAStringCostsTheStrCall`, `TestStrStopsAtTheLimitAndWhenCancelled`; the nesting cases (`field chain`, `and chain`, `or chain`, `interpolation`) in both `TestNestingLimit` and `TestNestingAtLimitEvaluates` |
| the example | `TestUptimeErgonomicMatchesUptime`, `TestUptimeErgonomicIsShort` |

## Review round 1

Eight Minor findings, no Critical or Important ones.

| Finding | Disposition |
|---|---|
| The concatenation's error position when the string starts with `${` (two findings) | Docs were wrong, code right (it matches `str(s) + str(s)`). Corrected above and in `pE-plan-6.md`; tested. |
| `\|\|` as a default returns the default for any non-true left side | As planned (decision 4). Example reworded here, in `pE-plan-1.md` and in the README; tested. `arv check` (Phase 8.1) could flag a `\|\|` whose left side is not boolean. |
| No test that a right side that runs is capability-checked | Added (denied at the call, `1:9`, `2:3`, `1:14`). |
| A `${}` of a string costs 2 steps more than `+` | Accepted and documented under Cost; tested. |
| A brace in a `//` comment inside `${...}` | Fixed: `closingBrace` skips comments as the lexer does; tested. |
| An escaped-only literal's `Token.Literal` differs from its `Value` | Documented in the AST-walker notes. |
| A `return` in a condition is dropped | Base bug; fixed in the final fixer round (`16zg`). |

## E2E and hardening

E2E cases, fuzz targets with their run results, the desugaring property and the leak checks: [`16pe-hardening.md`](16pe-hardening.md). Mutation check round 1 (41 mutations, 40 caught): [`16pe-mutations.md`](16pe-mutations.md).

## Open item found while planning (not Phase 5E's)

`FuzzMatchSyntax` (`arv/parser/fuzz_p4_test.go`) fails within a minute of fuzzing, on the base as well. Inputs: subject `0`, ok name `"A)\x00"`, bodies `0`, err name `A`, errFirst and comma true. `isName("A)\x00", ")")` lexes `A)\x00)`, where the NUL ends the input, so it sees `A` then `)` and calls the name valid; the composed program then ends at the NUL and fails with `expected next token to be =>, got EOF instead (at 6:6)`. The harness, not the parser, is wrong: `isName` (and `alone`) should refuse a part that holds a NUL. The failing corpus entry was not committed.
