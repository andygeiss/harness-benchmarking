---
name: judge
description: Score the Go code from a harness run (in ./sandbox or an example workspace) for quality on a 0–1 scale — contract fidelity, simplicity/no-bloat, Go idiomaticity, readability, robustness, performance. Fable referees; the bars are real Sonnet and Opus solutions to the same contract, scored head-to-head (all judged blind on the same rubric). Appends one record per candidate to logs/judgments.jsonl, each carrying a deterministic modernize-finding count and an optional paired modernize --fix uplift as a noise-free idiomaticity signal. Use after a run to measure code quality; it never gates completion. Best run under Fable in a fresh session.
---

# judge — Fable-as-a-judge for harness output

Grade the **quality** of the Go code a harness run produced, on a 0.0–1.0 scale
where **1.0 = a careful top-tier idiomatic Go reference** and **0.0 = compiles
and barely passes but you would reject it in review**.

**Fable is the judge; Sonnet and Opus are the bars.** The code under test is the
local model's. To measure it, this skill scores it **head-to-head against a real
Sonnet solution and a real Opus solution** to the same contract — all three
judged blind, by Fable, on the same rubric. Fable referees; it supplies **none**
of the candidates. That split is the whole point (see next section).

## Why Fable referees, and why two bars

The first version of this skill pinned 1.0 to an *Opus* reference, with Opus
judging. That was wrong twice over: (1) **wrong bar** — a small local model
measured against the judge's own frontier tier compresses every score into the
low end, so the number barely discriminates; (2) **self-preference** — a judge
rating code anchored to its own style inflates the anchor, a known judge bias.
The second version fixed both: Opus stayed referee, and the baseline dropped one
tier to a concrete, *scored* **Sonnet** solution the judge did not write.

This version moves both pieces up a step, on the same two principles:

- **The referee is the most capable model available.** That is now **Fable**,
  not Opus. A stronger judge is a better instrument.
- **The judge authors no candidate.** With Fable refereeing, Opus is freed to
  enter as a **second baseline**: Fable writes none of the three solutions, so
  self-preference still cancels in every gap.

The two bars answer different questions. **Sonnet stays the headline peer bar**
— near-tier, so the gap discriminates for a small local model. **Opus is the
frontier bar** — the headroom above Sonnet on the identical rubric. The old
"wrong bar" objection does not return: it applied to Opus as the 1.0 *anchor* of
a relative scale, not to Opus as one more candidate scored against the rubric's
absolute cells, where it compresses nothing and adds a second reading.

## What this is — and is not

This is a **measurement instrument, never a gate.** The harness's `done` gate
(`go test`) decides *correctness*, deterministically and anti-gameably. This
skill measures the orthogonal axis — *how good is the code, given it is correct*
— and must never feed back into a harness run. The local model never sees these
scores; that is exactly what keeps them honest (nothing to optimize against).

Treat each scalar as **ordinal, not cardinal**, and trust the **gaps** between
subject and baselines over any absolute number: the comparisons are what this
measures (local vs Sonnet, local vs Opus, run A vs B, config vs config), not
that code is "0.78 good."

## Before you start

- **Run the judge under Fable.** The referee must be Fable; if the current
  session model is not Fable, say so and stop — rows refereed by different
  models are not comparable (`judge_model` records the referee; rows from before
  2026-06 were Opus-refereed).
- **Each baseline is that model's actual code, not an idea of it.** Produce it
  by invoking the model itself (a subagent with `model: sonnet` / `model: opus`,
  or the harness pointed at that backend), handed the **same `PROMPT.md` and
  seed** the local model got — same contract, same provided test, no extra
  coaching. Never have Fable "write what Sonnet or Opus would": the judge cannot
  faithfully emulate another tier, and doing so smuggles back the
  self-preference you are removing.
- **Contestant vs. judge — different rules on the test.** Producing code and
  judging it are different roles. The contestants — the local model, Sonnet,
  Opus — **may read and run `*_test.go`** while solving; it is the spec. The
  **judge** (you, Fable) may **not** — see below.
- **Judge from files, not memory.** Evaluate only the on-disk artifacts of each
  candidate. Ignore any prior conversation about the code; ideally judge in a
  fresh session.
- **Stay blind to the spec's tests when judging.** Do **not** open `*_test.go`
  and do **not** run `go test` to score. The point is to judge whether the code
  satisfies the *contract* — including cases the seed's tests may not cover.
  Reading or running the tests biases you toward "it's green, so it's fine" and
  destroys the one thing this measures that the gate cannot: **over-fitting to
  the provided tests.** Judge every candidate under the identical blind lens.

## Inputs

- **Subject** — the directory holding the local model's implementation. Default
  `./sandbox` (what `cmd/example` writes). It is wiped on the next run, so never
  write outputs there. The user may pass another path.
- **Baselines** — two directories holding the Sonnet and the Opus solutions to
  the *same* contract, produced as above (e.g. scratch dirs outside the repo).
  Generate whichever does not exist yet (step 1).
- **Contract** — the task's `PROMPT.md`: for a bundled example,
  `examples/<name>/PROMPT.md`; otherwise ask. The single source of truth for
  every candidate.

## Procedure

1. **Get the baselines.** For each baseline model (Sonnet, then Opus) lacking a
   solution to this contract, generate one: copy the example's seed (its
   `workspace/` — `go.mod`, `static/`, `*_test.go`) into a scratch dir *outside*
   the repo, hand a subagent of that model (`model: sonnet` / `model: opus`) the
   `PROMPT.md` and that seed, and have it implement the task until
   `go test ./...` passes there. Keep each contestant independent — do **not**
   show it the subject's code or the other baseline's.
2. For **each** candidate (subject, then each baseline), run steps 3–6 with the
   identical lens.
3. Read the **contract** (`PROMPT.md`) and every implementation file under the
   candidate — glob `*.go`, **excluding `*_test.go`**. Skip the tests; do not read
   them to "check your work."
4. **Objective signals (read-only; they reveal no tests).** Inside the candidate
   run `gofmt -l <dir>` and `go vet ./...` — unformatted files or vet findings
   inform Idiomaticity / Readability / Robustness. Then take the **deterministic
   modernize-finding count**, the noise-free idiomaticity metric this skill logs
   (`modernize_findings`, schema below): run modernize **read-only** and count what
   it reports — never `--fix` the candidate, that would rewrite the very code you
   are judging and erase the signal (and the score). **Never run `go test`.**

       # inside the candidate dir — counts modernize findings on the code AS WRITTEN
       golangci-lint run --default=none --enable=modernize --output.json.path stdout ./... 2>/dev/null \
         | python3 -c "import json,sys; s=sys.stdin.read().strip(); print(sum(i['FromLinter']=='modernize' for i in (json.JSONDecoder().raw_decode(s)[0].get('Issues') or [])) if s else 0)"

   golangci-lint's bundled modernize is the *pinned* gopls suite, so record the
   `golangci-lint version` alongside the count to keep it reproducible (a plain
   text run also prints a `* modernize: N` tally if you prefer to eyeball it).
   Record this count for **every** candidate — it is head-to-head like the scores:
   "subject 3 vs Sonnet 0 vs Opus 0" says more than any number alone.
5. Score **each dimension independently**: write a one-line justification *first*,
   then the number. Never collapse them into a single gestalt score. Anchor with
   the rubric cells — 1.0 = the left cell, 0.0 = the right, 0.5 = competent-but-
   unremarkable — so scores do not pile up in 0.6–0.9.
6. Compute the scalar as the explicit **weighted mean** of the dimension scores
   (weights below). Show the arithmetic.
7. *(Optional — the paired modernize uplift.)* To express what a deterministic
   `modernize --fix` would buy the **subject** in the judge's own units, score a
   fixed copy *in this same session*: `cp -r <subject> <scratch>` (a dir **outside**
   the repo and `./sandbox`), run `golangci-lint run --default=none --enable=modernize
   --fix ./...` then `gofmt -w .` inside `<scratch>` (the fixer can leave imports
   ungrouped — see caveats), and re-run steps 3–6 on `<scratch>` under the identical
   lens. Record `raw_score`, `fixed_score`, and `delta = fixed_score − raw_score` as
   `modernize_uplift` on the subject row. **Score both halves in one session** so the
   session's shared bias cancels in the delta — the same trick the head-to-head uses.
   The delta is **single-sample and directional only**: modernize moves just
   idiomaticity/readability/(a little) performance, so it is bounded to a few
   hundredths and sits at or below this judge's noise floor — do not report it as
   calibrated unless you repeat across k sessions (see caveats). Skip this step and
   set `modernize_uplift` null if you only want the deterministic count.
8. Print the head-to-head summary (all three candidates side by side, the gaps,
   and what a rewrite of the *subject* would change), then **append one JSON line
   per candidate** to `logs/judgments.jsonl` (schema below), sharing a `pair_id`.

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
the same cells to subject and baselines alike.

**Security is intentionally omitted** — near-constant on these algorithmic tasks.
Add it (and reweight) once examples start parsing untrusted input.

## Output

**Human summary** — a side-by-side table: each dimension → **subject**,
**Sonnet**, and **Opus** scores with one-line whys, the three weighted scalars,
the **gaps** (subject − each baseline), and 2–3 sentences on what a rewrite of
the *subject* would change to close (or extend) them.

**Record** — append exactly **three** lines to `logs/judgments.jsonl` (one per
candidate; create `logs/` if absent — it is gitignored, like `runs.jsonl`). Get
`time` from `date -u +%Y-%m-%dT%H:%M:%SZ`; set `judge_model` to the *actual*
current Fable model id; set `subject_model` to the model that **produced** the
judged code; give all three rows the same `pair_id` so the comparison can be
rejoined (the field name predates the second baseline; it is the join key):

```json
{
  "time": "2026-01-01T00:00:00Z",
  "judge_model": "claude-fable-5",
  "rubric_version": "judge/v1",
  "method": "head-to-head",
  "pair_id": "todo-2026-01-01T00:00:00Z",
  "role": "subject",
  "subject_model": "local:Qwen3.6-35B-A3B-oQ6-fp16-mtp",
  "task": "todo",
  "contract": "examples/todo/PROMPT.md",
  "target": "sandbox",
  "blind_to_tests": true,
  "golangci_lint_version": "2.12.2",
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
  "modernize_findings": 0,
  "modernize_uplift": {"raw_score": 0.0, "fixed_score": 0.0, "delta": 0.0, "method": "paired-same-session", "samples": 1, "calibrated": false},
  "notes": "caveats; standout findings"
}
```

The two baseline rows are identical but with `"role": "baseline"`,
`subject_model` naming the baseline's author (`"claude-sonnet-4-6"`,
`"claude-opus-4-8"`), and `"target"` pointing at that baseline's dir.
`modernize_findings` is recorded on **all** rows — it is head-to-head like
the scores; `golangci_lint_version` pins the count so it stays reproducible across
time. `modernize_uplift` is **subject-only**: set it `null` on the baseline rows,
and `null` on the subject row too when you skip step 7. One compact, valid-JSON
line each (append with `>> logs/judgments.jsonl`). Do not write into `./sandbox`
or any candidate dir — they are transient (the uplift's `--fix` runs on a copy).

## Caveats (surface these in the summary when they bite)

- **Ordinal, not cardinal; trust the gaps** — the absolute scalars rank, they do
  not certify "0.78 good." The subject−baseline **gaps** are the durable signal.
- **Self-preference, mostly neutralized** — the judge authors none of the three
  candidates, so the old anchor-inflation is gone. A residual *judge-style* bias
  remains (Fable's taste in Go), but it falls equally on all candidates, so it
  largely cancels in the gaps — though judge and baselines share a model family
  while the subject does not, an asymmetry unchanged from the Opus-refereed era.
  It would still distort a lone absolute score.
- **The referee changed (Opus → Fable, 2026-06)** — scores are comparable only
  within one referee; `judge_model` says which produced a row. To set a new run
  against a pre-change result, re-judge the old subject under Fable rather than
  mixing eras.
- **Each baseline is one sample, written directly** — a comparison inherits that
  model's run-to-run variance (generate k baselines and take the median for a
  stable bar), and reflects the model *writing the code directly*, not driven
  *through this harness*. For a strict harness-vs-harness read, back the harness
  with that model and judge the output instead.
- **Point estimate** — one judging pass is one sample. For a calibrated number,
  run this skill in **k fresh sessions** and take the per-dimension median; k
  re-scorings inside one session are correlated, not independent.
- **`modernize_findings` is deterministic but narrow** — it counts one linter's
  idiomaticity nits (`any`/`slices`/`min`/range-int), not design quality, and it is
  frequently **0** on a capable model (this repo's own code: 0). That zero is the
  *finding* — "writes modern Go unaided" — not a measurement failure. Unlike the
  scores it is reproducible (pinned to the `golangci-lint` version), so trust it as
  a hard number where you treat the scores as ordinal.
- **`modernize_uplift` is bounded and noisy — directional only** — modernize moves
  just idiomaticity/readability/(a little) performance, ≈0.38 of the rubric weight
  and only a fraction of that, so its scalar effect is a few *hundredths* — at or
  below this judge's single-session noise. Paired same-session scoring cancels the
  session's shared bias in the delta (why step 7 insists on one session), but the
  number still wants k repeats to certify. Never `--fix` the candidate itself —
  only a throwaway copy — or you poison both this metric and the score.
- **Joining to `runs.jsonl`** — that log records a `task` field (the `-prompt`
  path, e.g. `examples/calc/PROMPT.md`) alongside the *harness* model and outcome,
  so a judgment joins to its run on the task (modulo path-vs-name), with timestamp
  proximity disambiguating repeated runs of the same task.
