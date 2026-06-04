# Task: implement an arithmetic expression evaluator

Implement package `calc` so that the provided tests (`calc/calc_test.go`) pass.
The build currently fails because the package has no implementation.

## Contract

    func Eval(expr string) (float64, error)

`Eval` parses and evaluates an arithmetic expression and returns its value.

Support:
- `+`, `-`, `*`, `/` with the usual precedence (`*` and `/` bind tighter than
  `+` and `-`) and left associativity.
- Parentheses for grouping, nested to any depth.
- Unary minus (e.g. `-5`, `2 * -3`, `-(1 + 2)`).
- Integer and decimal literals (e.g. `42`, `3.5`).
- Arbitrary surrounding and internal whitespace.

Return a non-nil error for malformed input (empty or blank, missing operands,
unbalanced parentheses, trailing tokens, unknown characters) and for division
by zero. The tests compare floats with a small tolerance, so ordinary
floating-point rounding is fine.

## How to approach it (this will take several passes)

Your context is reset between passes, so build this in stages and track them in
PROGRESS.md. A clean decomposition:

1. A lexer that turns the input string into a token stream.
2. A recursive-descent parser for the grammar (expr -> term -> factor).
3. Evaluation, including the error cases.

After each stage run `go test ./...` to see how far you have gotten, record the
state in PROGRESS.md, and continue. Read PROGRESS.md first on every pass — it is
your only memory across resets.

## Rules

- Put the implementation in package `calc` (you may split it across files).
- Do not modify `calc_test.go`.
- Use only the Go standard library.
- You are done when `go test ./...` passes.
