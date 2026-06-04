---
name: judge
description: Score the Go code from a harness run (in ./sandbox or an example workspace) for quality on a 0–1 scale anchored to Opus — contract fidelity, simplicity/no-bloat, Go idiomaticity, readability, robustness, performance — and append a structured judgment to logs/judgments.jsonl. Use after a run to measure code quality; it never gates completion. Best run under Opus in a fresh session.
---

# judge — Opus-as-a-judge for harness output

Grade the **quality** of the Go code a harness run produced, on a 0.0–1.0 scale
where **1.0 = a careful Opus reference** (top-tier idiomatic Go) and **0.0 =
compiles and barely passes but you would reject it in review**.

## What this is — and is not

This is a **measurement instrument, never a gate.** The harness's `done` gate
(`go test`) decides *correctness*, deterministically and anti-gameably. This
skill measures the orthogonal axis — *how good is the code, given it is correct*
— and must never feed back into a harness run. The local model never sees these
scores; that is exactly what keeps them honest (nothing to optimize against).

Treat the scalar as **ordinal, not cardinal**: trust it to *rank* (run A vs B,
model vs model, config vs config), not as proof that code is "0.78 good."

## Before you start

- **Run under Opus.** The 1.0 anchor is Opus-quality. If the current session
  model is not an Opus model, say so and record it — a Sonnet-judged row is not
  comparable to an Opus-judged one.
- **Judge from files, not memory.** Evaluate only the on-disk artifacts. Ignore
  any prior conversation about this code; ideally run this in a fresh session.
- **Stay blind to the spec's tests.** Do **not** open `*_test.go` and do **not**
  run `go test`. The point is to judge whether the code satisfies the *contract*
  — including cases the seed's tests may not cover. Reading or running the tests
  would bias you toward "it's green, so it's fine" and destroy the one thing this
  measures that the gate cannot: **over-fitting to the provided tests.**

## Inputs

- **Target** — the directory holding the produced implementation. Default
  `./sandbox` (what `cmd/example` writes). It is wiped on the next run, so never
  write outputs there. The user may pass another path.
- **Contract** — the task's `PROMPT.md`: for a bundled example,
  `examples/<name>/PROMPT.md`; otherwise ask. This is the source of truth for
  what the code must do.

## Procedure

1. Read the **contract** (`PROMPT.md`) and every implementation file under the
   target — glob `*.go`, **excluding `*_test.go`**. Skip the tests; do not read
   them to "check your work."
2. *(Optional objective signals — they reveal no tests)* run `gofmt -l <target>`
   and `go vet ./...` inside the target. Unformatted files or vet findings inform
   Idiomaticity / Readability / Robustness. **Never run `go test`.**
3. *(Optional stronger anchor)* write a short Opus reference solution for the
   contract and score the candidate *relative* to it; record
   `anchor: "opus-reference"`. Otherwise score against the descriptors below and
   record `anchor: "rubric"`.
4. Score **each dimension independently**: write a one-line justification *first*,
   then the number. Never collapse them into a single gestalt score.
5. Compute the scalar as the explicit **weighted mean** of the dimension scores
   (weights below). Show the arithmetic.
6. Print the human summary, then **append one JSON line** to
   `logs/judgments.jsonl` (schema below).

## Dimensions and weights (rubric `judge/v1`)

| Dimension (key)               | Weight | 1.0                                                                      | 0.0                                                              |
|-------------------------------|:------:|-------------------------------------------------------------------------|-----------------------------------------------------------------|
| Contract fidelity (`contract_fidelity`) | 0.30 | satisfies every clause of `PROMPT.md`, incl. unstated edges; survives stricter tests | meets only the happy path; brittle on edges the contract implies |
| Simplicity & restraint (`simplicity`)   | 0.20 | smallest correct design; no needless abstraction, dead code, or premature generality | over-engineered or bloated; indirection the task does not need   |
| Go idiomaticity (`idiomaticity`)        | 0.15 | idiomatic stdlib Go; natural naming, errors, control flow               | fights the language; reinvents the stdlib                       |
| Readability (`readability`)             | 0.15 | clear structure; comments earn their place                              | hard to follow; noisy or absent where needed                    |
| Robustness (`robustness`)               | 0.12 | correct error paths; no panics on bad input; sensible edges             | crashes or misbehaves on malformed input                        |
| Performance (`performance`)             | 0.08 | sane algorithms and allocation discipline                               | gratuitous waste or accidental quadratics                       |

Weights sum to 1.0 and encode this project's ethos — contract fidelity and
restraint dominate; perf rarely discriminates on these self-contained tasks.
They are a starting point: change them and bump `rubric_version`. Per dimension,
anchor with the row's cells so scores do not compress into 0.6–0.9 — 1.0 = the
left cell, 0.0 = the right cell, 0.5 = a competent-but-unremarkable middle.

**Security is intentionally omitted** — near-constant on these algorithmic tasks.
Add it (and reweight) once examples start parsing untrusted input.

## Output

**Human summary** — a short table of dimension → score + one-line why, the
weighted scalar, and 2–3 sentences on what an Opus rewrite would change.

**Record** — append exactly one line to `logs/judgments.jsonl` (create `logs/`
if absent; it is gitignored, like `runs.jsonl`). Get `time` from
`date -u +%Y-%m-%dT%H:%M:%SZ`; set `judge_model` to the *actual* current Claude
model id:

```json
{
  "time": "2026-01-01T00:00:00Z",
  "judge_model": "claude-opus-4-8",
  "rubric_version": "judge/v1",
  "task": "calc",
  "contract": "examples/calc/PROMPT.md",
  "target": "sandbox",
  "blind_to_tests": true,
  "anchor": "rubric",
  "dimensions": {
    "contract_fidelity": {"score": 0.0, "why": "..."},
    "simplicity":        {"score": 0.0, "why": "..."},
    "idiomaticity":      {"score": 0.0, "why": "..."},
    "readability":       {"score": 0.0, "why": "..."},
    "robustness":        {"score": 0.0, "why": "..."},
    "performance":       {"score": 0.0, "why": "..."}
  },
  "weights": {"contract_fidelity":0.30,"simplicity":0.20,"idiomaticity":0.15,"readability":0.15,"robustness":0.12,"performance":0.08},
  "score": 0.0,
  "notes": "caveats; standout findings"
}
```

One compact, valid-JSON line (e.g. append with `>> logs/judgments.jsonl`). Do
not write into `./sandbox` or anywhere under the target — both are transient.

## Caveats (surface these in the summary when they bite)

- **Ordinal, not cardinal** — see above.
- **Self-preference** — Opus grading toward an Opus anchor leans toward Opus's
  own style; the number has a built-in ceiling bias. Reference-anchoring narrows
  it but does not remove it.
- **Point estimate** — one pass is one sample. For a calibrated number, run this
  skill in **k fresh sessions** and take the per-dimension median; k re-scorings
  inside one session are correlated, not independent.
- **Loose correlation to `runs.jsonl`** — that log records the *harness* model and
  outcome but not the task name, so join a judgment to its run by timestamp
  proximity plus the example you know you judged.
