---
name: judge
description: Score the Go code from a harness run (in ./sandbox or an example workspace) for quality on a 0–1 scale — contract fidelity, simplicity/no-bloat, Go idiomaticity, readability, robustness, performance. Opus referees; the bar is a real Sonnet solution to the same contract, scored head-to-head (both judged blind on the same rubric). Appends one record per candidate to logs/judgments.jsonl. Use after a run to measure code quality; it never gates completion. Best run under Opus in a fresh session.
---

# judge — Opus-as-a-judge for harness output

Grade the **quality** of the Go code a harness run produced, on a 0.0–1.0 scale
where **1.0 = a careful top-tier idiomatic Go reference** and **0.0 = compiles
and barely passes but you would reject it in review**.

**Opus is the judge; Sonnet is the bar.** The code under test is the local
model's. To measure it, this skill scores it **head-to-head against a real
Sonnet solution** to the same contract — both judged blind, by Opus, on the same
rubric. Opus referees; it does **not** supply the baseline. That split is the
whole point (see next section).

## Why Sonnet, not Opus, is the baseline

Earlier this skill pinned 1.0 to an *Opus* reference. That was wrong twice over:
(1) **wrong bar** — a small local model measured against the judge's own frontier
tier compresses every score into the low end, so the number barely discriminates;
(2) **self-preference** — Opus rating code anchored to Opus's own style inflates
the anchor, a known judge bias.

Keeping Opus as the **referee** is correct — it is the most capable judge. The
fix is to drop the **baseline** one tier, to **Sonnet**, and make it concrete:
generate an actual Sonnet solution and score it *alongside* the subject on the
identical rubric. Opus authored neither candidate, so self-preference cancels (it
bears equally on both), and the headline becomes a peer comparison — *local-vs-
Sonnet, refereed by Opus* — instead of *local-vs-the-judge's-ceiling*.

## What this is — and is not

This is a **measurement instrument, never a gate.** The harness's `done` gate
(`go test`) decides *correctness*, deterministically and anti-gameably. This
skill measures the orthogonal axis — *how good is the code, given it is correct*
— and must never feed back into a harness run. The local model never sees these
scores; that is exactly what keeps them honest (nothing to optimize against).

Treat each scalar as **ordinal, not cardinal**, and trust the **gap** between
subject and baseline over either absolute number: the comparison is what this
measures (local vs Sonnet, run A vs B, config vs config), not that code is
"0.78 good."

## Before you start

- **Run the judge under Opus.** The referee must be Opus; if the current session
  model is not Opus, say so and stop — a Sonnet-refereed row is not comparable to
  an Opus-refereed one.
- **The baseline is Sonnet's actual code, not an idea of it.** Produce it by
  invoking Sonnet (a subagent with `model: sonnet`, or the harness pointed at a
  Sonnet backend), handed the **same `PROMPT.md` and seed** the local model got —
  same contract, same provided test, no extra coaching. Never have Opus "write
  what Sonnet would": Opus cannot faithfully emulate a lower tier, and doing so
  smuggles back the self-preference you are removing.
- **Contestant vs. judge — different rules on the test.** Producing code and
  judging it are different roles. The Sonnet contestant, like the local model,
  **may read and run `*_test.go`** while solving — it is the spec. The **judge**
  (you, Opus) may **not** — see below.
- **Judge from files, not memory.** Evaluate only the on-disk artifacts of each
  candidate. Ignore any prior conversation about the code; ideally judge in a
  fresh session.
- **Stay blind to the spec's tests when judging.** Do **not** open `*_test.go`
  and do **not** run `go test` to score. The point is to judge whether the code
  satisfies the *contract* — including cases the seed's tests may not cover.
  Reading or running the tests biases you toward "it's green, so it's fine" and
  destroys the one thing this measures that the gate cannot: **over-fitting to
  the provided tests.** Judge both candidates under the identical blind lens.

## Inputs

- **Subject** — the directory holding the local model's implementation. Default
  `./sandbox` (what `cmd/example` writes). It is wiped on the next run, so never
  write outputs there. The user may pass another path.
- **Baseline** — a directory holding the Sonnet solution to the *same* contract,
  produced as above (e.g. a scratch dir outside the repo). Generate it if it does
  not exist yet (step 1).
- **Contract** — the task's `PROMPT.md`: for a bundled example,
  `examples/<name>/PROMPT.md`; otherwise ask. The single source of truth for both
  candidates.

## Procedure

1. **Get the Sonnet baseline.** If a Sonnet solution to this contract does not
   already exist, generate one: copy the example's seed (its `workspace/` —
   `go.mod`, `static/`, `*_test.go`) into a scratch dir *outside* the repo, hand a
   `model: sonnet` subagent the `PROMPT.md` and that seed, and have it implement
   the task until `go test ./...` passes there. Keep it independent — do **not**
   show it the subject's code.
2. For **each** candidate (subject, then baseline), run steps 3–6 with the
   identical lens.
3. Read the **contract** (`PROMPT.md`) and every implementation file under the
   candidate — glob `*.go`, **excluding `*_test.go`**. Skip the tests; do not read
   them to "check your work."
4. *(Optional objective signals — they reveal no tests)* run `gofmt -l <dir>`
   and `go vet ./...` inside the candidate. Unformatted files or vet findings
   inform Idiomaticity / Readability / Robustness. **Never run `go test`.**
5. Score **each dimension independently**: write a one-line justification *first*,
   then the number. Never collapse them into a single gestalt score. Anchor with
   the rubric cells — 1.0 = the left cell, 0.0 = the right, 0.5 = competent-but-
   unremarkable — so scores do not pile up in 0.6–0.9.
6. Compute the scalar as the explicit **weighted mean** of the dimension scores
   (weights below). Show the arithmetic.
7. Print the head-to-head summary (both candidates side by side, the gap, and
   what a rewrite of the *subject* would change), then **append one JSON line per
   candidate** to `logs/judgments.jsonl` (schema below), sharing a `pair_id`.

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
left cell, 0.0 = the right cell, 0.5 = a competent-but-unremarkable middle. Apply
the same cells to subject and baseline alike.

**Security is intentionally omitted** — near-constant on these algorithmic tasks.
Add it (and reweight) once examples start parsing untrusted input.

## Output

**Human summary** — a side-by-side table: each dimension → **subject** score and
**baseline** score with one-line whys, the two weighted scalars, the **gap**
(subject − baseline), and 2–3 sentences on what a rewrite of the *subject* would
change to close (or extend) that gap.

**Record** — append exactly **two** lines to `logs/judgments.jsonl` (one per
candidate; create `logs/` if absent — it is gitignored, like `runs.jsonl`). Get
`time` from `date -u +%Y-%m-%dT%H:%M:%SZ`; set `judge_model` to the *actual*
current Opus model id; set `subject_model` to the model that **produced** the
judged code; give both rows the same `pair_id` so the comparison can be rejoined:

```json
{
  "time": "2026-01-01T00:00:00Z",
  "judge_model": "claude-opus-4-8",
  "rubric_version": "judge/v1",
  "method": "head-to-head",
  "pair_id": "todo-2026-01-01T00:00:00Z",
  "role": "subject",
  "subject_model": "local:Qwen3.6-35B-A3B-oQ6-fp16-mtp",
  "task": "todo",
  "contract": "examples/todo/PROMPT.md",
  "target": "sandbox",
  "blind_to_tests": true,
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

The paired baseline row is identical but with `"role": "baseline"`,
`"subject_model": "claude-sonnet-4-6"`, and `"target"` pointing at the Sonnet
dir. One compact, valid-JSON line each (append with `>> logs/judgments.jsonl`).
Do not write into `./sandbox` or any candidate dir — they are transient.

## Caveats (surface these in the summary when they bite)

- **Ordinal, not cardinal; trust the gap** — the absolute scalars rank, they do
  not certify "0.78 good." The subject−baseline **gap** is the durable signal.
- **Self-preference, mostly neutralized** — Opus no longer authors the baseline,
  so the old anchor-inflation is gone. A residual *judge-style* bias remains
  (Opus's taste in Go), but it falls equally on both candidates, so it largely
  cancels in the gap. It would still distort a lone absolute score.
- **Baseline is one Sonnet sample, written directly** — the comparison inherits
  Sonnet's run-to-run variance (generate k baselines and take the median for a
  stable bar), and reflects Sonnet *writing the code directly*, not Sonnet driven
  *through this harness*. For a strict harness-vs-harness read, back the harness
  with a Sonnet model and judge that output instead.
- **Point estimate** — one judging pass is one sample. For a calibrated number,
  run this skill in **k fresh sessions** and take the per-dimension median; k
  re-scorings inside one session are correlated, not independent.
- **Joining to `runs.jsonl`** — that log records a `task` field (the `-prompt`
  path, e.g. `examples/calc/PROMPT.md`) alongside the *harness* model and outcome,
  so a judgment joins to its run on the task (modulo path-vs-name), with timestamp
  proximity disambiguating repeated runs of the same task.
