---
name: judge
description: Score the Go code from a harness run (in ./sandbox or an example workspace) for quality on a 0–1 scale — contract fidelity, simplicity/no-bloat, Go idiomaticity, readability, robustness, security, performance. Opus referees; the bar is a real Sonnet-medium solution to the same contract, scored head-to-head (both judged blind on the same rubric). Appends one record per candidate to logs/judgments.jsonl, each carrying a deterministic modernize-finding count and an optional paired modernize --fix uplift as a noise-free idiomaticity signal. Use after a run to measure code quality; it never gates completion. Best run under Opus in a fresh session.
---

# judge — Opus-as-a-judge for harness output

Grade the **quality** of the Go code a harness run produced, on a 0.0–1.0 scale
where **1.0 = a careful top-tier idiomatic Go reference** and **0.0 = compiles
and barely passes but you would reject it in review**.

**Opus is the judge; a Sonnet-medium solution is the bar.** The code under test
is the local model's. To measure it, this skill scores it **head-to-head against
a real Sonnet-medium solution** to the same contract — both judged blind, by
Opus, on the same rubric. Opus referees; it supplies **neither** candidate. That
split is the whole point (see next section).

## Why Opus referees, and why one bar

The first version of this skill pinned 1.0 to an *Opus* reference, with Opus
judging. That was wrong twice over: (1) **wrong bar** — a small local model
measured against the judge's own frontier tier compresses every score into the
low end, so the number barely discriminates; (2) **self-preference** — a judge
rating code anchored to its own style inflates the anchor, a known judge bias.
The fix, restored here, rests on two principles:

- **The bar is one tier below the frontier, not at it.** The baseline is a
  concrete, *scored* **Sonnet-medium** solution — near the local model's tier, so
  the subject−bar gap actually discriminates instead of bottoming out. 1.0 stays
  an *absolute* rubric cell (a careful top-tier idiomatic Go reference), so Sonnet
  earns its score blind rather than defining the top by fiat.
- **The judge authors no candidate.** **Opus referees**; it writes neither the
  subject's code nor the Sonnet bar, so self-preference cancels in the gap. The
  referee need only outrank the candidates and author none of them — with a
  single Sonnet bar, Opus satisfies both.

The old "wrong bar" objection does not return: it applied to a frontier model as
the 1.0 *anchor* of a relative scale, not to Sonnet as a candidate scored against
the rubric's absolute cells, where it compresses nothing.

An earlier experiment moved the referee to **Fable** and added **Opus as a
second, frontier bar** (three rows per `pair_id`). This returns to a **single
near-tier peer bar under an Opus referee**: the headline question is always
*local vs. its near peer*, and the frontier-headroom the second bar read was never
the gap that discriminated. Those Fable-era rows are a distinct set — `judge_model`
splits them (see Caveats).

## What this is — and is not

This is a **measurement instrument, never a gate.** The harness's `done` gate
(`go test`) decides *correctness*, deterministically and anti-gameably. This
skill measures the orthogonal axis — *how good is the code, given it is correct*
— and must never feed back into a harness run. The local model never sees these
scores; that is exactly what keeps them honest (nothing to optimize against).

Treat each scalar as **ordinal, not cardinal**, and trust the **gap** between
subject and baseline over any absolute number: the comparisons are what this
measures (local vs Sonnet, run A vs B, config vs config), not that code is
"0.78 good."

## Before you start

- **Run the judge under Opus.** The referee must be Opus; if the current
  session model is not Opus, say so and stop — rows refereed by different
  models are not comparable (`judge_model` records the referee; the 2026-06
  Fable-refereed rows are a separate set).
- **The baseline is that model's actual code, not an idea of it.** Produce it
  by invoking the model itself (a subagent with `model: sonnet` at **medium
  reasoning effort**, or the harness pointed at that backend), handed the **same
  `PROMPT.md` and seed** the local model got — same contract, same provided test,
  no extra coaching. Never have Opus "write what Sonnet would": the judge cannot
  faithfully emulate another tier, and doing so smuggles back the self-preference
  you are removing. (The subagent spawn exposes the model but not an effort knob,
  so "medium" is the documented intent for the bar; note it on the row.)
- **Contestant vs. judge — different rules on the test.** Producing code and
  judging it are different roles. The contestants — the local model and Sonnet —
  **may read and run `*_test.go`** while solving; it is the spec. The **judge**
  (you, Opus) may **not** — see below.
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
- **Baseline** — one directory holding the Sonnet-medium solution to the *same*
  contract, produced as above (e.g. a scratch dir outside the repo). Generate it
  if it does not exist yet (step 1).
- **Contract** — the task's `PROMPT.md`: for a bundled example,
  `examples/<name>/PROMPT.md`; otherwise ask. The single source of truth for
  every candidate.

## Procedure

1. **Get the baseline.** If the Sonnet-medium solution to this contract does not
   exist yet, generate one: copy the example's seed (its `workspace/` — `go.mod`,
   `static/`, `*_test.go`) into a scratch dir *outside* the repo, hand a subagent
   (`model: sonnet`, medium reasoning effort) the `PROMPT.md` and that seed, and
   have it implement the task until `go test ./...` passes there. Keep the
   contestants independent — do **not** show it the subject's code.
2. For **each** candidate (subject, then the baseline), run steps 3–6 with the
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
   "subject 3 vs Sonnet 0" says more than any number alone.
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
8. Print the head-to-head summary (both candidates side by side, the gap, and
   what a rewrite of the *subject* would change), then **append one JSON line
   per candidate** to `logs/judgments.jsonl` (schema below), sharing a `pair_id`.

## Dimensions and weights (rubric `judge/v2`)

| Dimension (key)               | Weight | 1.0                                                                      | 0.0                                                              |
|-------------------------------|:------:|-------------------------------------------------------------------------|-----------------------------------------------------------------|
| Contract fidelity (`contract_fidelity`) | 0.28 | satisfies every clause of `PROMPT.md`, incl. unstated edges; survives stricter tests | meets only the happy path; brittle on edges the contract implies |
| Simplicity & restraint (`simplicity`)   | 0.18 | smallest correct design; no needless abstraction, dead code, or premature generality | over-engineered or bloated; indirection the task does not need   |
| Go idiomaticity (`idiomaticity`)        | 0.14 | idiomatic stdlib Go; natural naming, errors, control flow               | fights the language; reinvents the stdlib                       |
| Readability (`readability`)             | 0.13 | clear structure; comments earn their place                              | hard to follow; noisy or absent where needed                    |
| Robustness (`robustness`)               | 0.10 | correct error paths; no panics on bad input; sensible edges             | crashes or misbehaves on malformed input                        |
| Security (`security`)                   | 0.10 | validates and bounds untrusted input; no injection, path-escape, or resource-exhaustion vectors; safe defaults | trusts input blindly; exploitable injection, path traversal, or unbounded resource use |
| Performance (`performance`)             | 0.07 | sane algorithms and allocation discipline                               | gratuitous waste or accidental quadratics                       |

Weights sum to 1.0 and encode this project's ethos — contract fidelity and
restraint dominate; perf, and for now security, rarely discriminate on these
self-contained tasks. They are a starting point: change them and bump
`rubric_version`. Per dimension, anchor with the row's cells so scores do not
compress into 0.6–0.9 — 1.0 = the left cell, 0.0 = the right cell, 0.5 = a
competent-but-unremarkable middle. Apply the same cells to subject and baseline
alike.

**Security is scored but usually near-constant** — on these self-contained
algorithmic tasks the contestants rarely differ on it, so it ties and the
subject−baseline gap still comes from the other dimensions; score the tie
honestly rather than inventing a difference. It earns its 0.10 once examples
start parsing untrusted input (path handling, deserialization, command
construction) — judged by reading the code, not by adding a scanner tool.

## Output

**Human summary** — a side-by-side table: each dimension → **subject** and
**Sonnet** scores with one-line whys, the two weighted scalars, the **gap**
(subject − Sonnet), and 2–3 sentences on what a rewrite of the *subject* would
change to close (or extend) it.

**Record** — append exactly **two** lines to `logs/judgments.jsonl` (one per
candidate; create `logs/` if absent — it is gitignored, like `runs.jsonl`). Get
`time` from `date -u +%Y-%m-%dT%H:%M:%SZ`; set `judge_model` to the *actual*
current Opus model id; set `subject_model` to the model that **produced** the
judged code; give both rows the same `pair_id` so the comparison can be rejoined
(it is the join key):

```json
{
  "time": "2026-01-01T00:00:00Z",
  "judge_model": "claude-opus-4-8",
  "rubric_version": "judge/v2",
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
    "security":          {"score": 0.0, "why": "..."},
    "performance":       {"score": 0.0, "why": "..."}
  },
  "weights": {"contract_fidelity":0.28,"simplicity":0.18,"idiomaticity":0.14,"readability":0.13,"robustness":0.10,"security":0.10,"performance":0.07},
  "score": 0.0,
  "modernize_findings": 0,
  "modernize_uplift": {"raw_score": 0.0, "fixed_score": 0.0, "delta": 0.0, "method": "paired-same-session", "samples": 1, "calibrated": false},
  "notes": "caveats; standout findings"
}
```

The baseline row is identical but with `"role": "baseline"`, `subject_model`
naming the baseline's author (`"claude-sonnet-4-6"`, medium effort), and
`"target"` pointing at the baseline's dir. `modernize_findings` is recorded on
**both** rows — it is head-to-head like the scores; `golangci_lint_version` pins
the count so it stays reproducible across time. `modernize_uplift` is
**subject-only**: set it `null` on the baseline row, and `null` on the subject
row too when you skip step 7. One compact, valid-JSON line each (append with
`>> logs/judgments.jsonl`). Do not write into `./sandbox` or any candidate dir —
they are transient (the uplift's `--fix` runs on a copy).

## Caveats (surface these in the summary when they bite)

- **Ordinal, not cardinal; trust the gaps** — the absolute scalars rank, they do
  not certify "0.78 good." The subject−baseline **gaps** are the durable signal.
- **Self-preference, mostly neutralized** — the judge authors neither
  candidate, so the old anchor-inflation is gone. A residual *judge-style* bias
  remains (Opus's taste in Go), but it falls equally on both candidates, so it
  largely cancels in the gap — though judge and baseline share a model family
  (Claude) while the subject does not, an asymmetry it would still let distort a
  lone absolute score.
- **The referee is Opus; a 2026-06 Fable era sits between** — scores are
  comparable only within one referee (and bar set); `judge_model` says which
  produced a row, `role`/`subject_model` say which bars were present. To set a
  new run against a Fable-era result, re-judge the old subject under Opus rather
  than mixing eras.
- **`judge/v2` added a weighted security dimension** — a v2 `score` is not
  comparable to a v1 `score`: the scalar's basis changed (six dims reweighted to
  make room for `security` at 0.10), the same way referee eras don't mix.
  `rubric_version` says which basis a row uses; do not diff a v2 scalar against a
  v1 one. The subject−baseline **gap** is more portable, but security ties on
  today's algorithmic tasks, so v2 mostly re-slices the same signal until an
  example parses untrusted input.
- **The baseline is one sample, written directly** — the comparison inherits
  Sonnet's run-to-run variance (generate k baselines and take the median for a
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
  just idiomaticity/readability/(a little) performance, ≈0.34 of the rubric weight
  and only a fraction of that, so its scalar effect is a few *hundredths* — at or
  below this judge's single-session noise. Paired same-session scoring cancels the
  session's shared bias in the delta (why step 7 insists on one session), but the
  number still wants k repeats to certify. Never `--fix` the candidate itself —
  only a throwaway copy — or you poison both this metric and the score.
- **Joining to `runs.jsonl`** — that log records a `task` field (the `-prompt`
  path, e.g. `examples/calc/PROMPT.md`) alongside the *harness* model and outcome,
  so a judgment joins to its run on the task (modulo path-vs-name), with timestamp
  proximity disambiguating repeated runs of the same task.
