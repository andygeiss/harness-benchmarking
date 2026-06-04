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
- **numkit** — implement five independent numeric helpers (`GCD`, `IsPrime`,
  `DigitSum`, `Factorial`, `Fib`) against a provided test. A small *decomposable
  checklist*: every function is trivial and independent, so PROGRESS.md is a
  natural per-function checklist. Added as a minimal cross-pass-resume fixture —
  but read "Cross-pass memory" below: this model one-shots it.
- **textkit** — implement eight independent string helpers (`WordCount`,
  `CountVowels`, `IsPalindrome`, `Acronym`, `RunLengthEncode`/`RunLengthDecode`,
  `Rotate`, `Title`). The same idea as `numkit`, one size up.

## Cross-pass memory (and why these examples one-shot here)

The Ralph loop exists to carry a task across context resets: state survives only
on the filesystem — the code being written, plus a `PROGRESS.md` the agent keeps
as its plan memory. `numkit` and `textkit` were added as decomposable checklists
to *exercise* that resume, and the harness grew a `-memory` flag to ablate it:

    go run ./cmd/example textkit                 # memory on (default)
    go run ./cmd/example textkit -memory=false   # off: drops the PROGRESS.md guidance
                                                 # and wipes the file before each pass

With `-memory=false` the agent must resume from the persisted code alone; the run
log (`logs/runs.jsonl`) records `memory`, `passes`, and `pass_reasons` for an A/B.

**The honest finding.** On the default model (`Qwen3.6-35B-A3B`), these tasks —
and the staged `calc` — complete in a **single pass**, so the cross-pass handoff
is never triggered and the memory A/B is null. The model writes a complete,
correct implementation in its first pass (often two turns: read the spec, then
write the whole package), and the end-of-pass probe finishes the run the moment
it verifies. No per-pass budget reliably forces a second pass:

- cut the budget *after* the model finishes → the probe completes it (one pass);
- cut it *before* it writes anything → no progress → the stagnation guard halts;
- the middle — a persisted but *non-verifying* partial — appears only when the
  model *iterates* (write → test → fix), which needs it to be wrong first. That
  is nondeterministic on clean, fully-specified tasks, and a pile of trivial
  units doesn't induce it (the model batches them into one `write_file`).

So resume is effectively un-exercisable with clean, small, deterministic tasks on
this model. The `-memory` ablation and these fixtures are kept for the cases that
*do* span passes — a weaker or more-quantized model that makes mistakes and
iterates, or a task genuinely larger than one context window. To reproduce: each
run appends to `logs/runs.jsonl` (gitignored) with `memory`/`passes`/`outcome` —
on this model every row reads `passes: 1`, `outcome: completed`, either memory setting.
