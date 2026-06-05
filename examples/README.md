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
- **todo** — a server-rendered **htmx** todo app (`package main`): `net/http`
  handlers over a concurrency-safe in-memory store, pages rendered with
  `html/template`, and templates plus static assets (a vendored `htmx.min.js` and
  `app.css`) served from `embed.FS`. The seed ships the spec (`todo_test.go`,
  which drives the handlers through `httptest`) and the static assets; the agent
  writes `main.go` and the templates. The largest, most Go-idiomatic example —
  multiple endpoints, fragment-vs-page rendering, HTML escaping, method routing —
  so it has real quality variance for `/judge` and is the most likely to span
  passes. After a run, view it: `cd sandbox && go run .`, then open
  http://localhost:8080.

## Cross-pass memory (and why these examples one-shot here)

The Ralph loop exists to carry a task across context resets: state survives only
on the filesystem — the code being written, plus a `PROGRESS.md` the agent keeps
as its plan memory. `calc` is the example meant to *exercise* that resume (it
decomposes into lexer → parser → eval), and the harness grew a `-memory` flag to
ablate it:

    go run ./cmd/example calc                 # memory on (default)
    go run ./cmd/example calc -memory=false   # off: drops the PROGRESS.md guidance
                                              # and wipes the file before each pass

With `-memory=false` the agent must resume from the persisted code alone; the run
log (`logs/runs.jsonl`) records `memory`, `passes`, and `pass_reasons` for an A/B.

**The honest finding.** On the default model (`Qwen3.6-35B-A3B`), `calc` — like
the smaller examples — completes in a **single pass**, so the cross-pass handoff
is never triggered and the memory A/B is null. The model writes a complete,
correct implementation in its first pass (often two turns: read the spec, then
write the whole package), and the end-of-pass probe finishes the run the moment
it verifies. No per-pass budget reliably forces a second pass:

- cut the budget *after* the model finishes → the probe completes it (one pass);
- cut it *before* it writes anything → no progress → the stagnation guard halts;
- the middle — a persisted but *non-verifying* partial — appears only when the
  model *iterates* (write → test → fix), which needs it to be wrong first, and
  that is nondeterministic on clean, fully-specified tasks.

So resume is effectively un-exercisable with clean, deterministic tasks on this
model. The `-memory` ablation and `calc` are kept for the cases that *do* span
passes — a weaker or more-quantized model that makes mistakes and
iterates, or a task genuinely larger than one context window. To reproduce: each
run appends to `logs/runs.jsonl` (gitignored) with `memory`/`passes`/`outcome` —
on this model every row reads `passes: 1`, `outcome: completed`, either memory setting.

## Measuring code quality

Passing the tests means the code is *correct* — not that it is *good*. Those are
different axes, and the harness only gates on the first. The `judge` skill
(`.claude/skills/judge/SKILL.md`) measures the second: it scores the produced
code (Go idiomaticity, simplicity, contract fidelity, readability, robustness,
performance) on a 0–1 scale, scored head-to-head against a real Sonnet solution
to the same contract (Opus referees both, **blind to the spec's tests**), and
appends one row per candidate to `logs/judgments.jsonl`. Run it after a harness
run, best in a fresh Opus session:

    /judge        # grades ./sandbox against the example's PROMPT.md

It is out-of-band measurement, never a gate — the agent never sees it. The `todo`
example exists partly to give it something with real quality variance to grade.
See the skill for the rubric and its caveats.
