# The Arv language

A tour of the syntax and builtins; the [README](../README.md) covers grants, budgets, agents and the command line.

Literals are numbers, booleans, strings, arrays, maps and functions. There are `let` bindings, `if`/`else` expressions, closures and recursion. `return v` leaves the enclosing function with `v` from wherever it runs, even from a block inside a `let` value, an argument, an operand or a `${...}`, and nothing after it runs; at top level it ends the program. `for x in xs { ... }` evaluates to the array of its body's values, so `await for url in urls { spawn check(url) }` starts one task per URL and collects their results; `await [t1, t2]` awaits each task in order. Strings compare with `==` and `!=`. Builtins include `len`, `push`, `last`, `puts`, `error`, `http_get`, `read_file`, `write_file`, `getenv`, `sleep`, `ask` and `agent`, plus `lines(s)` (the non-blank lines of `s`, trimmed), `split(s, sep)`, `json(v)` (compact JSON, hash keys in order), `str(v)` (`v` as `puts` prints it) and `clock_ms()` (a monotonic clock, for latencies).

`r.up` is `r["up"]`. `"${n} of ${len(xs)} down"` interpolates: each `${...}` holds an expression (no string literals inside it) and prints as `puts` would; `\${` is a literal `${`. Integers compare with `<`, `>`, `<=` and `>=`. `a && b` and `a || b` short-circuit: the right side runs only when the left does not decide, and, as in `if`, only `true` counts as true, so `||` is not a default operator (`r.name || "anon"` is `"anon"` whatever the name).

`examples/uptime.arv` is the Phase 5 gate scenario: check 200 URLs, 20 at a time, with a timeout and restarts, and write a report. `bench/` compares it with an idiomatic Go version against a local farm (`make bench`) and probes both for a key-stealing change (`make escape`).

`examples/uptime-ergonomic.arv` is the same scenario written with the syntax above. It takes the same grants (`--allow fs_read=targets.txt --allow fs_write=out --allow net=127.0.0.1:PORT`) and writes the same report.

Arv started as an interpreter written while reading *Writing an Interpreter in Go*. The earlier lexer and parser experiments are at tag `v0-research`.
