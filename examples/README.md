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
  writes `main.go` and the templates. The largest *web* example and the most
  Go-idiomatic — multiple endpoints, fragment-vs-page rendering, HTML escaping,
  method routing — so it has real quality variance for `/judge`. After a run,
  view it: `cd sandbox && go run .`, then open http://localhost:8080.
- **graphkit** — a small **graph algorithms library** (module `graphkit`, six
  packages): a directed weighted `graph` core, then `traverse` (BFS/DFS),
  `paths` (Dijkstra, BFS shortest path, distances), `toposort` (Kahn + cycle
  detection), `components` (undirected components + Tarjan SCC), and `spanning`
  (Kruskal MST). ~15 functions across six packages, with the `graph` core as the
  one build-first dependency. This is the **largest** example and the substrate
  for measuring cross-pass resume — though on the 35B model it still completes in
  a single pass (see below). Each package
  ships its own spec file, so the model gets granular feedback
  (`go test ./graph/...`) while the done-gate stays the full `go test ./...`. The
  spec is deterministic by construction — all node/edge iteration in ascending
  label order, components and paths in canonical form — so "verification passed"
  means the algorithms are correct, not that the model matched a map order.
- **apikit** — a **modular JSON HTTP API** (module `apikit`, five packages): a
  `health` liveness check, three independent CRUD resources (`users`, with
  `409`-on-duplicate-email; `tasks`, with a boolean field; `notes`, with an
  optional field), and an `api` package that composes them behind one mux with a
  catch-all `404`. The feature packages share no code and do not import each other
  — only `api` imports them — so the build can accrete **one self-contained package
  at a time**. This is the substrate for the **long-run / per-pass-budget**
  question: each feature is a small independent increment, so a run can make
  progress across many passes without holding the whole service in context. Each
  package ships its own spec (`*_test.go` driving the handlers through `httptest`)
  with uniform conventions (REST verbs, JSON shapes, status codes) stated in
  `PROMPT.md`.
- **datakit** — five **independent** container packages (`stack`, `queue`, `set`,
  `heap`, `ring`; module `datakit`, ~12.4 KB of specs), and **no package imports
  another** — deliberately *flat*, the control substrate for the stagnation
  study. Where `apikit`/`graphkit` end in a costly *composing* increment (the
  shape that creates a hard per-pass-budget floor), `datakit` by construction has
  none, and that contrast is its job: it bounds the spec-elision lever — the
  byte-reduction generalises here, the completion benefit does not, because there
  is no floor to clear ([docs/stagnation.md](../docs/stagnation.md) Part 8).
  Validated satisfiable and deterministic (93 test-passes under `-count=3`)
  before use.
- **pipeline** — a context-aware **concurrent fan-out/fan-in** pipeline (module
  `example/pipeline`, one function): `Pipeline(ctx, in, workers, fn)` runs `fn`
  over a slice across a bounded pool of `workers` goroutines and returns the
  results in input order, honouring `ctx` cancellation. The **first example
  whose contract is the concurrency itself** — the substrate for the judge's
  `concurrency_safety` dimension, which otherwise ties on every non-web example.
  The split is the point: because the done-gate runs `go test` but never
  `-race` (the harness pins `CGO_ENABLED=0`), the spec gates only what a
  race-free run can prove deterministically — correct in-order output, real
  **≥`workers`** fan-out (an injected `fn` parks until the pool fills, so a
  sequential solution hangs and an in-test timeout fails it), clean termination,
  and prompt cancellation (`ctx.Err()` with partial results discarded).
  Race-freedom, goroutine-leak freedom, and the exact-`workers` *upper* bound
  (an unbounded one-goroutine-per-item solution passes the gate) are
  deliberately left to the out-of-loop judge, which reads them off the code — so
  a model can pass the gate with racy or leaky code, exactly the gate-vs-judge
  gap the project exists to measure. Validated before use: a correct pool is
  green under `-count=3`, a sequential solution fails only the fan-out test via a
  clean timeout, and an unbounded solution passes.

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

**The larger task — and the honest result.** `graphkit` is the largest example:
six packages, ~15 functions, ~730 lines of implementation against a 518-line
spec. It was built to force the multi-pass case `calc` cannot. **It does not, on
the 35B model.** A measured run completes in a **single pass** (23 steps, ~4 min,
`outcome: completed`, `passes: 1`): the model implements all six packages, tests
each, fixes the one that fails, runs the full suite, and calls `done` — without
tripping the per-call context ceiling. ~730 lines simply is not enough *tokens* to
overflow a 52k-token pass: the model's window is 64k+, and oMLX prompt-caching
keeps accumulation cheap (that run reused ~79% of its prompt tokens from cache,
and emitted only ~8.4k completion tokens total). The earlier extrapolation from
`todo` was wrong — `todo`'s two passes were a premature `model_stop`, not context
exhaustion, so nothing here has ever actually overflowed a window.

To genuinely exercise cross-pass resume on this model you must **cap the budget
below the task's single-pass peak**, go **dramatically larger** (multiple thousands
of co-resident lines), or use a **weaker model** — and a weaker model alone is not
enough: a measured **27B-oQ4 run at `-ctx-limit 16384` also one-shot** (`passes: 1`,
~13 min, 701 lines), because the peak working context sat *just under* that cap,
~15–16k tokens. Three things keep a pass that small: the reasoning trace is
stripped from history (that run emitted only ~7.4k completion tokens total), the
writes are compact, and ~87% of the prompt was served from cache. So the cap has to sit *below* the peak — and **two measured `graphkit` runs on the
oQ4 default bracket the window**, with a result more nuanced than the clean resume
first guessed:

- **`-ctx-limit 11000` → stagnates at 4/6 packages** (12 passes, all `context`,
  ~9.5 min). This is the closest thing to *real* incremental resume the harness has
  shown: `graph`, `traverse`, `paths` (including a cross-pass fix to its Dijkstra
  heap) and `toposort` are written and survive across resets — but `components` and
  `spanning` never get written. The model writes `PROGRESS.md` *once* and never
  updates it, so each fresh pass re-derives state by re-reading and re-testing the
  existing packages, which alone exhausts the 11k budget before any new code is
  written; three unchanged passes trip the stagnation guard. With the plan-memory
  neglected, `-memory=true` quietly degrades to `-memory=false`. So ~10–12k is *too
  low* — it drives multi-pass operation but starves completion.
- **`-ctx-limit 16000` → completes in 2 passes** (`context` → `completed`, ~4 min) —
  the first run to finish across a context reset, but a *thin* one. Pass 1 writes
  all six packages and is cut off by the budget one step before it can run the full
  suite or call `done`; pass 2 resumes from the persisted files with a fresh
  context, re-verifies, updates `PROGRESS.md` (it maintains it this time —
  maintaining the memory is itself budget-dependent), and calls `done` — writing no
  new implementation. So the filesystem hand-off demonstrably carries a run to
  completion across a reset, but the strong case — implementation genuinely *split*
  across passes — still hasn't landed on this model: it builds fast, so the
  bottleneck is verification, not generation, and the band between "stagnates" and
  "one-shots" is narrow.

**The purpose-built substrate — `apikit`, measured.** `apikit` (five independent
packages, built for exactly this question) was run at both ends of the budget, and
it lands the same way `graphkit` did. At **`-ctx-limit 26000` it one-shots** (1 pass,
~3.7 min, **5/5** packages, ~8.6k completion tokens, ~90% of prompt from cache) — a
five-package HTTP service still inside a single pass once the budget clears its peak.
At **`-ctx-limit 11000` it never completes: 8 runs, 0 completions**, every one
stagnating at **3–4 of 5** packages — always `health`, `tasks`, `users`, with `notes`
the swing package and `api` (the composition that must hold the other four in
context) reached by *none*. Same lever as `graphkit`: per-pass budget, not task
structure, decides completion.

`apikit` also carried a **replicated `-memory` A/B** (n=4 each at 11k) that puts the
earlier "memory=true ≈ memory=false" result on *outcome* rather than read-counts — and
it holds. Packages reached: **memory=true `{4,3,4,4}`** (mean 3.75, 4/5 in 3 of 4) vs
**memory=false `{3,3,3,4}`** (mean 3.25, 4/5 in 1 of 4). The distributions overlap —
both span 3–4 — and a Fisher's exact test on "reached 4" (3/4 vs 1/4) gives **p ≈ 0.5**:
no establishable effect, a faint non-significant lean at most. The sting is in the
n=1: the *first single pair* (memory=true 4/5 vs memory=false 3/5) read as a clean
memory win, and replication dissolved it — exactly the trap the "single-digit samples,
ordinal" caveat is for. Cross-pass memory does not lift the budget floor; it only,
maybe, nudges the frontier half a package.

`graphkit` and `apikit` are the substrates built for this; `calc` was too small to even be a
candidate. The completing `graphkit` run also exercised the spec's
*algorithm-independence* —
the model solved SCC with Kosaraju where the reference uses Tarjan, and the
canonical-form contract accepted it — **but with a sting in the tail.** That same
`components` package is **flaky**: its Kosaraju mispartitions on ~8% of runs (it
splits a genuinely strongly-connected pair, `{d,e}`), depending on Go's
map-iteration order. at the time, the done-gate ran `go test -json -count=1`, which defeated the
test *cache* but sampled a *single* execution — so a non-deterministic
implementation passed on a lucky roll, and this run was certified `completed` on
exactly such a roll (the end-of-pass probe drew a *failing* one after pass 1, the
only reason the run took two passes rather than completing via the probe in one).
`-count=1` closed the accidental exit-0/no-test path (see `CLAUDE.md`), **not**
non-determinism: a flaky test is a non-adversarial way past a falsely-green gate,
distinct from the documented adversarial forge-the-markers hole. The gate has since
been hardened to `-count=3` — three independent rolls per check — turning this from
an unbounded surprise into the bounded, documented caveat now in `CLAUDE.md`; at an
~8% flake rate it stays probabilistic, not a proof. So "verification passed" tracks
correctness only as far as those executions can — for a deterministic spec, easily
enough; for a non-deterministic *implementation* of it, not.

## Measuring code quality

Passing the tests means the code is *correct* — not that it is *good*. Those are
different axes, and the harness only gates on the first. The `judge` skill
(`.claude/skills/judge/SKILL.md`) measures the second: it scores the produced
code (Go idiomaticity, simplicity, contract fidelity, readability, robustness,
security, concurrency safety, performance) on a 0–1 scale, scored head-to-head
against a real Sonnet-medium
solution to the same contract (Opus referees both, **blind to the spec's
tests**), and appends one row per candidate to `logs/judgments.jsonl` — each row
also carrying a deterministic `modernize`-finding count and an optional paired
`--fix` uplift, a reproducible idiomaticity signal beside the subjective scores.
Run it after a harness run, best in a fresh Opus session:

    /judge        # grades ./sandbox against the example's PROMPT.md

It is out-of-band measurement, never a gate — the agent never sees it. The `todo`
example exists partly to give it something with real quality variance to grade.
See the skill for the rubric and its caveats.
