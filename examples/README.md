# Examples

Each example is a self-contained task for the harness: a `PROMPT.md` describing
the work and a `workspace/` seed the agent operates on. The seed ships with the
spec (a test) but no implementation; a run succeeds when the agent makes the
verification command (`go test ./...`) pass. Because the test is fixed and the
agent only writes the implementation, "verification passed" means the task was
done correctly — not that the agent graded its own homework.

Each seed is its own Go module, so the repo's own `go test ./...` skips it — a
deliberately-unimplemented seed never turns the harness build red.

## Running one

From the repository root:

    go run ./cmd/example reverse

That wipes `./sandbox`, copies the example's `workspace/` into it, and runs the
harness against it. Any extra flags are forwarded to the harness:

    go run ./cmd/example reverse -stream -debug

(The harness expects an oMLX server at http://localhost:1234/v1.)

## Catalogue

- **reverse** — implement `stringutil.Reverse` (rune-aware string reversal)
  against a provided test. The smallest end-to-end check of the loop: read the
  test, write the implementation, verify, done. Should finish in one pass.
- **calc** — implement an arithmetic expression evaluator (`calc.Eval`): a
  lexer, a recursive-descent parser, and evaluation, against a provided test
  suite. Large enough to span several Ralph passes, so it exercises the outer
  loop and the `PROGRESS.md` hand-off across context resets. To force/observe
  the cross-pass resume even on a fast model, cap the work per pass, e.g.
  `go run ./cmd/example calc -max-steps 15`.
- **stuck** — an *adversarial* fixture: a test that asserts the year is 1999,
  which no code can satisfy. The model has nothing productive to write, so the
  workspace stops changing and the **stagnation guard** halts the run early
  instead of looping to the iteration limit. Validates the guard end-to-end:
  `go run ./cmd/example stuck` stops around iter 4 with the default
  `-max-stale 3`; lower it (e.g. `-max-stale 2`) to halt sooner.
