# Stagnation: why a too-small per-pass budget never completes — and what to do

This documents the root cause of the `apikit`/`graphkit` low-budget stagnation, why
raising the budget is the wrong fix, and the smallest change that would make the
Ralph loop complete arbitrarily large tasks under a *fixed* context window.

**Part 1 is an established finding** (grounded in the code and a stagnation log).
**Part 2 is a design proposal — not implemented.** Don't treat it as built.

---

## Part 1 — Finding (established)

### Observation
`apikit` at `-ctx-limit 11000` never completes: **8 runs, 0 completions**, all parked
at 3–4 of 5 packages with `api` reached by none. At `26000+` it one-shots (and 32768 /
65536 also one-shot — the upper regime is flat; once budget clears the floor, more is
inert). The flip point is task-specific and so generalises to nothing; the mechanism
does.

### Mechanism: re-orientation starvation
The inner loop ends a pass when `resp.Usage.TotalTokens >= CtxLimit`, checked after
each step (`internal/agent/loop.go:186`). Each pass starts from an empty conversation
(`msgs = {system, prompt}`); the model rebuilds its working context by **reading files**,
and every read is appended to that conversation and re-sent on the next call. So
**reading spends the same budget that ends the pass.** One budget is split between two
competing uses:

- **Loading** — re-listing dirs, re-reading `PROGRESS.md`, the test spec, and existing code.
- **Acting** — writing code, running tests, reading failures.

Below a floor, loading crowds out acting entirely.

### Evidence (`logs/apikit-ctx11k.log`, one 11k run)
| signal | value |
|---|--:|
| passes | 12 — **all 12 cut by `context`** (0 completed; `done` called 0 times) |
| `read_file` + `list_dir` (load) | 118 + 74 = **192** |
| `write_file` + `edit_file` (act) | 5 + 3 = **8** |
| `api/api.go` ever written | **0** |

~24 load ops per act. Twelve passes re-reading the same workspace, never with budget
left to write-and-verify the next increment. It ended on the stagnation guard's
"workspace unchanged" — and since `PROGRESS.md` is excluded from the fingerprint, the
late passes changed *no code at all*: pure loading, zero acting.

### Why it is a hard ceiling, not slow convergence
A pass makes durable progress only if **load + act + verify one increment** fits under
`CtxLimit`. The task's *costliest* increment sets the floor. Here that is `api`: writing
it requires holding all four feature packages plus `api_test.go` in context at once —
that load alone exceeds 11k, so `api` is unreachable in *any* pass. More passes can't
help; each hits the identical wall. That is exactly why it parks at 3–4/5 with `api`
reached by none.

### Why cross-pass memory didn't move it
The replicated `-memory` A/B was null (Fisher p≈0.5). That is a *fingerprint of where the
cost is*: carrying `PROGRESS.md` tells the model *what* to do next; it does not save the
*reads* needed to actually do it. The bottleneck is re-**loading**, not re-**remembering**.

### Why raising the budget is the wrong axis
`CtxLimit` can only rise to `max_model_len` — a hard wall. Escalation rescues exactly one
case: a task that *fits* the window but was handed an artificially small budget. For a
task whose working set genuinely exceeds the window — **the reason a Ralph loop exists
instead of a single prompt** — escalation walks to the ceiling and stagnates there, with
certainty. It treats the symptom and abandons the premise that passes are small and
progress accumulates across them.

### The reframe: convergence conditions
A Ralph loop is a fixed-point iteration over the filesystem, `(workspace) → (workspace')`,
that converges when the verifier passes. To converge on an **arbitrarily large** task
under a **fixed** window, every pass must satisfy:

1. **Bounded working set** — the state slice a step needs fits in the window.
2. **Cheap load** — rebuilding that slice costs little of the budget (today: ~all of it).
3. **Monotonic durable progress** — bank a compiling, verified increment before the wall; never regress.

The window *bounding* the working set is a feature — it forces decomposition. The enemy
is an **unbounded per-increment working set** plus expensive loading.

**Load-bearing insight:** the working set per increment must be bounded by **interface
size, not implementation size**. `api` needs the four packages' *signatures*
(`func New() http.Handler`), not their four *bodies*. A well-structured codebase has small
seams between large modules, so the slice for any one step stays small no matter how big
the whole codebase grows. That — not a bigger window — is how a bounded loop completes an
unbounded task: you never need a pass bigger than the window, only each pass to touch one
seam cheaply.

### Honest hard limit
If a *single atomic increment's* minimal interface-level working set still exceeds the full
window, it is impossible for that model — decompose the increment further, or use a
bigger-window model. No harness change crosses that. The goal is to push the per-step
footprint down to the task's irreducible interface complexity, so "doesn't fit" happens
only for genuinely monolithic steps — not for `api`, which fits trivially once it sees
signatures instead of source.

---

## Part 2 — Design proposal (NOT implemented; for review)

Goal: satisfy the three convergence conditions with the smallest possible change —
Go-toolchain-native, standard-library-only, no new dependency, window untouched.

### Lever 1 — pass-start ground-truth digest  (→ condition 2: cheap load)
At pass start, the harness injects a compact, *computed* digest into the prompt:
- per-package build/test status (pass / fail / does-not-compile), from the verifier it
  already runs;
- the workspace file tree.

A few hundred tokens replacing dozens of orientation ops. This is **not** the prose-notes
"memory" that A/B-nulled — it is cheap computed truth, recomputed each pass (so never
stale). *Hook:* `cmd/harness` already builds `(system, prompt)` per pass and calls
`Session.Run`; prepend the digest to the per-pass prompt. `loop.go` unchanged.

### Lever 2 — interfaces, not bodies  (→ condition 1: bounded working set)
Inject `go doc`-level exported signatures of sibling packages, so a step can be written
against seams without reading implementations. `go doc` is the toolchain (no dependency);
run it per package, tolerate packages that don't yet compile. This is the direct fix for
the `api` floor: it shrinks `api`'s working set from four files to four signatures.

### Lever 3 — graceful checkpoint at a soft limit  (→ condition 3: durable progress)
At a soft threshold (e.g. 0.8 × `CtxLimit`), inject a one-shot wind-down instruction:
"near budget — bring the code to a compiling state, update state, then stop," so the
inevitable limit-hit banks a clean checkpoint instead of dying mid-increment. The hard cap
stays as the backstop. *Hook:* `loop.go`, when `resp.Usage.TotalTokens` crosses the soft
threshold.

### Sequencing (minimalism first)
Build **Lever 1 alone first** and measure its effect on the 11k `apikit` case — the
load:act ratio and outcome. Add Lever 2 only if `api`'s body-reads are still the killer;
add Lever 3 only if passes still die mid-increment. Each lever is independently testable
against the existing throttled-budget setups (re-run at the low budget, compare outcome +
load:act ratio). All three are prompt/IO-level; none touches the window, the sandboxed
model, or `go.mod`.

### What this does NOT fix
- The honest hard limit above (irreducible increment > full window).
- It relies on a weak local model *using* the injected context well; the digest reduces
  but cannot eliminate misallocation.

### Open questions
- Is Lever 1 sufficient for `apikit`@11k, or is Lever 2 required because the body-reads
  dominate? (Testable.)
- `go doc` on a partially-compiling workspace — confirm graceful per-package degradation.
- Does injected ground truth ever mislead if the model distrusts it? (Recomputed each pass.)

---

## Part 3 — Measured: Lever 1 (2026-06-07)

Lever 1 was built and A/B-tested at `-ctx-limit 11000`, n=3 each (`-digest` vs baseline,
interleaved, identical otherwise), then **reverted** — recorded here, not kept as dead
weight (cf. the resume-nudges). **Result: 0/3 vs 0/3 — both stagnate. Lever 1 does not
clear the floor.**

It did exactly what it was built to do, and that turned out to be the wrong half of `load`:

- **`list_dir` eliminated** — 6.0/pass → **0** (the file tree is handed over), cutting
  load/pass ~37% (15.5 → 9.8).
- **`read_file` unchanged** — ~9.3/pass → ~9.6. The freed budget bought no completion.

The read breakdown says why. Across **both** arms the model re-reads **all five test specs
plus `go.mod` ≈ once per pass, every pass** (each spec ~19× over 19 baseline passes; ~29×
over 29 digest passes), plus the implementations. At 11k the five specs alone are most of
the budget — spent *before any code is written* — and the digest's per-package "this one
passes" status **did not stop the model re-reading already-done packages.** It re-sweeps
the full spec set to re-orient regardless of what it is told.

So the binding cost is **full-spec-set re-reading, and it is unresponsive to injected
ground truth.** That redirects the design:

- **Lever 1 (cheap status + tree): insufficient** — status is not what the budget is spent on.
- **Lever 2 (`go doc` sibling interfaces): lower value than hoped** — it cuts *impl-body*
  reads (47/90 here), secondary to *spec* reads (95/145); and `api`, the one increment whose
  sibling bodies are the waste, is reached in only 1/6 runs.
- The cost that *would* matter — reading only the next increment's spec instead of all five —
  is **behavioural**, and the A/B shows this model ignores the harness's "what's done" signal.
  A prompt saying "read only the next failing package's spec" fights the same habit and would
  likely null the same way.

**Honest conclusion.** For this local model the 11k floor is set by its re-sweep-all-specs
habit × spec size, not by orientation waste a digest can remove. The reliable levers remain
the known ones: a per-pass budget above (full-spec-set + act) — the working 26k+ regime — or
a model that reads selectively. This is the doc's own caveat, now demonstrated: the
cheap-loading levers "rely on a weak model *using* the injected context well," and it does
not. Raw rows in `runs.jsonl` (`digest` field) and `logs/apikit-11k-*.log`; n=3, ordinal.

---

## Part 4 — Measured: load:act baseline (2026-06-07)

The Part 3 analysis hand-parsed the load:act split from the stderr `tool` lines. That split is
now logged directly: every run records `tool_counts` (calls per tool name) and `read_bytes`
(bytes `read_file` returned into the model's context) in `runs.jsonl` (`agent.Metrics`,
agent-invisible). So the *count-based* load:act ratio (`read_file`+`list_dir` vs
`write_file`+`edit_file`) and the *token-weighted* load (`read_bytes`/pass against the window)
are readable per run without re-parsing logs.

This records the **baseline arm** of the proposed read-path-elision A/B: the current harness, no
elision, `apikit` at `-ctx-limit 11000` (`-max-iters 300 -max-steps 60`, else default), n=3. All
three **stagnated at 4 passes, 3/5 packages** — `health`, `users`, `tasks` green; `notes` and
`api` never written (the documented floor, `api` reached by none):

| run | read_file | list_dir | write_file | edit_file | read_bytes |
|---|--:|--:|--:|--:|--:|
| 1 | 35 | 24 | 4 | 1 | 93,048 |
| 2 | 36 | 24 | 4 | 2 | 88,164 |
| 3 | 36 | 25 | 4 | 0 | 96,123 |

Per pass (mean of 3):

- **load:act op ratio** (read+list)/(write+edit) = **~12:1** (11.8 / 10.0 / 15.3) — ~12 loading
  ops per acting op.
- ~8.9 `read_file`, ~6.1 `list_dir`, ~1.25 acts.
- **`read_bytes` ≈ 23.1 KB/pass ≈ ~5.8k tokens ≈ ~52% of the 11k window**, spent re-reading files
  *before any code is written* — Part 1's mechanism as a logged number, not a hand-parse.
- `read_bytes`/pass (23.1 KB) **exceeds the whole spec set** (15,139 B: health 936 + users 4745 +
  tasks 3693 + notes 3725 + api 2040), so each pass re-loads all five specs *plus* ~8 KB of
  already-done implementations. The re-sweep is real and cross-pass.

**What it sets up.** The three green specs total **9,374 B** (health+users+tasks). Eliding them at
the `read_file` return path once their package verifies (the "elide-on-pass" lever) projects to
cut `read_bytes`/pass from ~23.1 KB to ~13.7 KB — freeing **~2.3k tokens/pass** of the 11k budget,
ramping as packages go green. That is the number the B arm must move, and the kill criterion reads
straight off `read_bytes`: revert only if completion stays 0/3 **and** the token-weighted read
share does not fall.

*Incidental:* `recordTool` counts *attempted* calls by name, so run 2's map carries two malformed
keys (`go\n<parameter=args`, `go build ./...\n</parameter`) — the model occasionally emits broken
tool-call syntax (the unknown-tool recovery path). Rare (2 of 213 calls across the three runs); the
analysis sums the known keys. Rows in `runs.jsonl`; stderr in `logs/apikit-counters-{1,2,3}.log`.

---

## Part 5 — Measured: elide-on-pass clears the floor in 4 of 7 (2026-06-07)

The "elide-on-pass" lever was built behind `-elide-passing` (default off) and A/B-tested. When
the verifier certifies a package green, `read_file` returns a one-line notice instead of that
package's `*_test.go` bytes, so a fresh pass cannot re-spend its budget re-reading specs already
satisfied. It is **mechanical, not behavioural**: the model may still issue the read (the habit
Part 3 showed is immovable), but the spec's bytes do not re-enter the window. Disk is untouched —
the `go` tool and the done gate still compile and run the real files, so a false-green is
structurally impossible (the gate reads the real `*_test.go`, never the stub).

**A/B, `apikit` @ `-ctx-limit 11000`, interleaved (B,A,…), else identical — n=7 elide vs n=10
baseline, built up over batches at the same session/server:**

| arm | completed | read_bytes/pass | elided_reads/run |
|---|---|--:|--:|
| elide (B)    | **4 / 7** (57%) | ~18.6k | 5–14 (fired every run) |
| baseline (A) | **0 / 10**      | ~23.5k | — |

Every completed elide run reached full **5/5 including `api`** — the increment baseline never
reaches. Across **every** recorded `apikit`@11k run with elision off — the seven interleaved A-arm
runs plus all earlier ones in `runs.jsonl`, **24 in total** — the score is **0 completions**; the
elide arm's four are the first the harness has produced at this floor by any change other than
raising the budget. **Fisher one-tailed is p≈0.015** for 4/7 vs 0/10 (that n=10 pools three Part-4
baselines with the seven interleaved A-arm runs); against the interleaved A arm alone — 4/7 vs 0/7 —
it is **p≈0.035**, and one completion flipping to stagnation drops it to ≈0.05: past conventional
significance, but **fragile at this n**.

- **Mechanism confirmed.** Elision fired in every elide run (`elided_reads` 5–14), and
  `read_bytes`/pass fell **~21%** (baseline ~23.5k → elide ~18.6k). Completers cut load hardest;
  the stagnating elide runs elided least and stayed baseline-like.
- **Kill criterion → pursue.** It was: revert iff completion stays 0/3 *and* read-share does not
  fall. Completion went **0 → 4/7** and read-share fell — so keep it.

**Honest limits.** Significant (p≈0.015) but **not a deterministic fix**: 3/7 elide runs still
stagnated, so the lever raises the completion *probability* (~57% here), it does not guarantee
completion — matching the prediction that it helps without eliminating the floor. The failure mode
is **safe**: a stagnating elide run ends cleanly, never falsely completing or breaking. So Part 1 /
Part 3's "no harness change clears the floor for this model" is **updated, not overturned**: a
mechanical read-path elision clears it ~4 times in 7 — the cheap-loading idea works once it acts on
what the budget is actually spent on (spec *bytes* at the read boundary), the half Lever 1 missed.

**Limitations of the lever.** It assumes one package per directory (true for the example tasks)
and runs one extra `go test -json -count=3` per pass to compute the passing set (acceptable here;
could later fold into the end-of-pass probe). It elides only *specs* of green packages, not their
*implementation bodies* — a stacked `go doc`-signatures lever remains if the residual floor needs
it. Rows in `runs.jsonl` (`elide_passing` / `elided_reads`); stderr in
`logs/apikit-{elide,baseline}-*.log`.

---

## Part 6 — Measured: elide-on-pass replicates on `graphkit`, 6/6 vs 0/6 (2026-06-07)

Part 5's evidence was `apikit` alone — the gap the audit flagged. `graphkit` is the second
purpose-built substrate: six small packages (`graph` the core; the other five import it), one
package per directory, a `go test ./...` gate — so it satisfies the lever's assumptions and tests
whether the `apikit` signal generalises or was task-specific. Same protocol: A/B at `-ctx-limit
11000`, interleaved (B,A,…), else identical (`-max-iters 300 -max-steps 60`), n=6 elide vs n=6
baseline, same session/server.

| arm | completed | read_bytes/pass | elided_reads/run | passes |
|---|---|--:|--:|--:|
| elide (B)    | **6 / 6** (100%) | ~17.9k | 10–29 (every run) | 4–9 |
| baseline (A) | **0 / 6**        | ~18.8k | — | 4–8 |

Every elide run reached full **6/6**; every baseline run stagnated at **3–4 of 6** (each wrote
`graph`, `paths`, `traverse`; one also `toposort`), the documented floor, completing none. **Fisher
one-tailed on 6/6 vs 0/6 is p≈0.0011** (1/C(12,6)) — and unlike `apikit`'s 4/7 it is **not
fragile**: flipping one elide completion to stagnation (5/6 vs 0/6) still gives p≈0.008. The
interleaving controls for server drift; the contrast holds across all six time-adjacent pairs.

- **Generalises — more cleanly than `apikit`.** Two independently-built tasks now show the lever
  clearing the floor: `apikit` 4/7 (p≈0.015, fragile) and `graphkit` 6/6 (p≈0.001, clean). Part 1's
  "the mechanism generalises, the flip point does not" is now measured on both halves — same
  mechanism, perfect separation here.
- **The completion effect is stronger, the byte-share effect weaker.** `read_bytes`/pass fell only
  **~5%** (18.8k → 17.9k), against `apikit`'s ~21% — yet completion went 0 → 6/6. The per-pass byte
  proxy is the wrong lens when the arms travel different distances: baseline stalls early (few
  reads), while elide *completers* run more **productive** passes (more reads total, but banking a
  package each). The win is **compounding**, not raw byte reduction — `graphkit`'s six fine-grained,
  cleanly-seamed packages let the model bank one green package at a time and stop re-paying for its
  spec, a closer fit to the lever's intent than `apikit`'s monolithic `api` increment. So do not
  read the lever's value off `read_bytes`/pass alone; the outcome is what moved.
- **Same safe failure mode, none triggered.** No false completions — the gate ran the real specs
  every pass. No elide run stagnated at all, so Part 5's residual-floor caveat (3/7 there) did not
  recur on `graphkit` at this n.

**Honest limits.** n=6 per arm; 6/6 vs 0/6 is perfect separation — striking, but twelve runs at one
budget on one model. It does not show the lever is deterministic in general; it shows that on the
two tasks built for this, a baseline floor of 0/24 (`apikit`) and 0/6 (`graphkit`) yields
completions only with elision, now across both. The honest hard limit (an irreducible increment
exceeding the full window) is untouched — `graphkit`'s increments simply sit well under it once
green specs stop re-entering. Rows in `runs.jsonl` (`elide_passing` / `elided_reads`, `task` =
`examples/graphkit/PROMPT.md`, 2026-06-07T20:22–21:38); stderr in
`logs/graphkit-{elide,baseline}-*.log`.

---

## Part 7 — Promoted to built-in; the status probe folded into the gate (2026-06-07)

With the lever validated on both substrates (Parts 5–6), `-elide-passing` was **removed as a flag and
made unconditional**: spec elision is now the harness's default read-path behaviour, and the
`elide_passing` run-log field is dropped (new rows carry only `elided_reads`). The two costs Part 5
listed under "Limitations" are addressed:

- **No extra `go test` per pass.** The standalone per-pass status probe (its own
  `go test -json -count=3` to compute the passing set) is **gone**. `GoTestVerifier` now folds its
  `-json` stream once and hands the passing package directories to `ElideState` via an `onPass` hook
  (`passingDirsFrom` in `done.go`), so the done gate and the end-of-pass probe — which run anyway —
  double as the status probe. Eliding adds zero test executions.
- **Still un-gameable.** Folding does not weaken the gate: the verifier reads the real `*_test.go`
  from disk every run, elision only shrinks the string `read_file` returns, and a non-`go test`
  verify command never feeds the set (so elision is a no-op there). A false-green stays structurally
  impossible.

**Why drop the flag rather than default it on?** The design's asymmetry: elision is **safe by
construction and either helps or is inert**, so an off-by-default knob was friction with little to
weigh against. The `-memory` flag is kept because memory *changes outcomes both ways* and so wants an
ablation toggle; elision does not. The baseline arms are already on record (Parts 1, 4–6), and
re-adding a toggle is a small change if a future task ever needs to ablate it. What remains genuinely
unbuilt is unchanged: the `go doc`-interface lever (Lever 2) and the soft-limit checkpoint (Lever 3).

**Update (2026-06-09) — the toggle is back, default-on.** That "future task" arrived: a second model
(`gemma-4-26B-A4B-it`, sampled at temp 1.0) to test whether the re-orientation floor and elision's
effect generalise beyond Qwen, whose re-sweep-all-specs habit the whole finding rests on (Parts 3, 9).
Locating a floor needs the elision-OFF baseline, which the built-in path cannot produce, so
`-elide-passing` was restored — but **default-on** this time (the lever is validated, not the original
experimental-off knob), leaving the `ElideState` nil only when explicitly ablated. Part 7's folded
onPass probe is untouched; OFF is simply the nil state ReadFile/Update/Elided already tolerate. Gemma
results follow.

---

## Part 8 — Measured: a flat third task bounds the lever (2026-06-08)

Parts 5–6 showed elision clearing the floor on `apikit` and `graphkit` — both **composer-floored**:
their costliest increment needs every sibling at once (`apikit`'s `api`; `graphkit`'s packages all
import `graph`). The open question was whether the win generalises or is specific to that shape. So a
**third, deliberately different** substrate was built: `datakit` — five **independent** container
packages (`stack`, `queue`, `set`, `heap`, `ring`), **no package imports another**, ~12.4 KB of
specs (between `graphkit`'s 13.2 KB and `apikit`'s 15.1 KB). Validated satisfiable + deterministic
(93 test-passes under `-count=3`) before use. The A/B ran at commit `db635c4` (the flag-based path,
for comparability with Parts 5–6; the built-in path is equivalent).

**The flat task has no hard floor.** `datakit` one-shots at `-ctx-limit 11000` (1 pass, 5/5). Single
runs at 8k/6k/5k stagnated, which *looked* like a floor — but the A/B revealed those were the tail of
a noisy, mostly-completing distribution. Interleaved n=6 elide vs n=6 baseline at two budgets:

| budget | elide | baseline | read_bytes/pass (elide / baseline) |
|---|---|---|--:|
| 8k | **6/6** | **3/6** | 10.5k / 13.4k (**−21%**) |
| 6k | **5/6** | **5/6** | 11.2k / 12.6k |

**The clincher: baseline completes *more* at 6k (5/6) than at 8k (3/6)** — non-monotonic in budget,
which is impossible if budget were the binding constraint. The model one-shots `datakit` (passes
1–3, reading all specs once) even at 6k, because each increment is trivially small (a `stack` is ~30
lines). There is no budget where baseline reliably stagnates, so **there is nothing for elision to
clear**.

- **Mechanism generalises; completion benefit does not — because there is no floor here.** Elision
  fired (up to 10 stubs/run) and cut `read_bytes`/pass **21%** at 8k, matching `apikit` exactly. But
  with completion never blocked, that saving buys no completion lift (8k 6/6-vs-3/6 is p≈0.09 over a
  *drifting* baseline — outcomes ran `S S S C C C` across the batch; 6k is a flat 5/6-vs-5/6). The
  lever shows **no significant completion effect here, and is never harmful** (gate-safe as always).
  "Inert" is the *conservative* reading of an underpowered arm — the 8k trend actually runs
  pro-elision, and at n=6 even a large true effect would often be missed — so what this null
  establishes is the absence of a hard floor, not a measured zero.
- **This bounds the lever, and that is the value of the third task.** Elision's *completion* benefit
  is **conditional on a hard floor**, and a hard floor requires a **costly composing increment** —
  precisely what `apikit`/`graphkit` have and `datakit`, by construction, does not. Neither composer
  task alone could establish this; the flat task's null is the contrast that fixes the lever's scope.

**Honest conclusion.** Across three purpose-built substrates the picture is now: the read-boundary
**byte-reduction generalises** (−21% on each task that re-sweeps specs), but it converts to a
**completion** win only where stagnation is real — i.e. where one increment's working set is large
relative to the window. So Parts 5–6's "clears the floor" is **scoped, not overturned**: it clears a
*composer* floor; on a flat kit of small independent packages there is no floor to clear, and
elision rides along harmlessly. Rows in `runs.jsonl` (`task` = `examples/datakit/PROMPT.md`,
2026-06-07T23:29 (8k) and 2026-06-08T12:53 (6k)); stderr in `logs/datakit-{,6k-}{elide,baseline}-*.log`.

---

## Part 9 — Measured: Lever 2 (go-doc sibling interfaces) does not clear the floor (2026-06-09)

Lever 2 (Part 2): write a composing increment against sibling *signatures*, not *bodies*. Built as the
evidence-informed mechanical version (Part 3 showed prompt-injection is ignored): behind `-impl-doc`
(default off), a read of a **green** package's non-test `.go` file returned its `go doc -all`
signatures instead of the body, stacking on built-in spec elision. Gate-safe: `go doc` type-checks
but never executes; disk untouched; the body was returned whenever `go doc` failed or was not smaller.

Pre-build evidence was split: the Part 5 stagnators re-read green sibling *bodies* heavily (13–27× vs
completers' 7×), suggesting a body-read floor Lever 2 targets — but an adversarial lens warned the
stalls might be behavioural, not I/O. Three stages settled it.

**(0) A contamination, caught and corrected.** A design-workflow agent, experimenting with `go doc`,
wrote a `health.go` impl into the `apikit` *seed* (`examples/apikit/workspace/health/`). Because
`cmd/example` reseeds the sandbox *from* that seed, the first A/B started every apikit run with
`health` pre-implemented — inflating elision-only to 8/8 and removing the floor. Caught via
`git status` before any conclusion was committed; the seed was cleaned and the A/B re-run. (Not a
harness bug — the reseed is correct; an agent polluted the repo. Lesson: keep agent experiments out of
the example seeds.)

**(1) Clean A/B @ 11k — no floor to clear.** elision+L2 **8/8** vs elision-only **7/8** (Fisher
p≈0.5, n.s.); L2 cut read_bytes/pass **32%** (18.6k → 12.6k). But elision-only at 11k is now 7/8
(88%), vs Part 5's 4/7 (57%) — the residual floor did not reproduce, so the test was underpowered by
the absence of the thing being tested. apikit@11k elision completion is session-variable.

**(2) Floor located.** elision-only stagnates reliably below 11k: **0/2 at each of 9k, 8k, 7k**. So 9k
is a budget where the floor is present and reliable.

**(3) Definitive A/B @ the 9k floor — Lever 2 does not clear it.**

| arm | completed | read_bytes/pass | doc_subs |
|---|---|--:|--:|
| A (elision only)   | **1 / 8** | 18.2k | 0 |
| B (elision + L2)   | **1 / 8** | 16.9k (−7%) | 4–25 |

At the floor where elision-only stagnates 7/8, **Lever 2 stagnates identically** (1/8 = 1/8; Fisher
p≈1.0). It fired (doc_subs 4–25) and trimmed reads ~7%, but did not convert to completion. The
sibling-body re-reads it removes are **not the binding constraint at the floor**: a stagnating run's
wall is the `api` increment itself (and the model's pass-to-pass behaviour), which a read-boundary
byte-cut does not move. Above the floor (11k) elision already completes, so L2 is unnecessary; at the
floor (9k) it is insufficient — **no budget regime where it demonstrably helps.**

**Verdict: null; reverted** — like the Lever 1 digest (Part 3), recorded not kept. The `-impl-doc`
flag, the `go doc` read path, and its code are removed; only this record and the logs remain. This
completes the cheap-loading lever investigation: of the three proposed levers, only **read-boundary
spec elision converts to completion** (and only against a hard composer floor — Parts 5–6, bounded by
Part 8). The digest (Lever 1) and go-doc-impls (Lever 2) both measure null. **Lever 3** (soft-limit
checkpoint) is now the one proposed lever left unbuilt. Rows in `runs.jsonl` (`impl_doc` / `doc_subs`,
apikit ctx 9000/11000, 2026-06-08–09); stderr in `logs/apikit-L2{,c,f}-*.log`, `logs/apikit-floor2-*.log`.

---

## Part 10 — Measured: a second model floors lower, and spec elision does not clear its floor (2026-06-09)

Everything above is one model (`Qwen3.6-35B-A3B`), and the doc says so repeatedly: "for this local
model the 11k floor is set by its re-sweep-all-specs habit." That habit is *behavioural*, so the whole
elision result has an unstated external-validity question — does it generalise across models, or only
across tasks? A second model answers it: `gemma-4-26B-A4B-it-oQ4-fp16` (a 26B MoE, ~4B active),
sampled hot at **temp 1.0 / top_p 0.95 / top_k 64** (vs Qwen's 0.6). To run the baseline arm the
`-elide-passing` toggle was restored default-on (Part 7 addendum). Same substrates, same `go test`
gate, same interleaved-A/B protocol.

**Gemma drives the harness cleanly** (smoke: `reverse` one-shot, 6 well-formed tool calls, no malformed
syntax) and is **terse** — ~46–150 completion tokens/call, **no thinking trace** at all (Gemma emits
none; `SplitReasoning` degrades to a no-op). That terseness is the headline mechanism: where Qwen
spends its budget on a reasoning trace *and* re-sweeping every spec, Gemma spends it on **acting**.

### Floor maps (baseline, elision OFF) — Gemma floors, but lower than Qwen

| task | 6k | 9k | 11k |
|---|---|---|---|
| **graphkit** | 0/3 (reaches 1–2/6) | 0/3 (reaches 1–3/6) | **completed 6/6** (n=1) |
| **apikit**   | — | 0/2 (reaches 3–4/5) | 0/2 (reaches 2–4/5) |

graphkit's floor sits **below 11k** for Gemma — it *completes* at 11k where Qwen, without elision,
completed 1/20 across every recorded run (0/6 in Part 6's interleaved A/B; the lone straggler —
2026-06-06T14:39 in `runs.jsonl` — landed during the since-removed prompt-nudge experiment the README
records as "nudges 0–1/5", so the Qwen floor is near-deterministic, not absolute). So **the mechanism
generalises (Gemma floors too) but the flip-point shifts down**, exactly
Part 1's "the mechanism generalises, the flip point does not" — now shown across *models*, not just
tasks. The cause is logged: at its floor Gemma's **load:act ratio is ~1.6–2.4 : 1**, against Qwen's
**~12 : 1** (Part 4). Gemma is not drowning in re-orientation; when the budget merely *allows* one
increment it banks a package per pass and converges (graphkit @ 11k, 7 passes, all cut by `context`,
finished by the end-of-pass probe — it never called `done`).

### A/B results — elision does **not** clear Gemma's floors

| task @ budget | B: elision ON | A: baseline | read_bytes/pass | Fisher |
|---|---|---|--:|--:|
| **graphkit @ 9k** (n=4) | **0/4** | **0/4** | 6.7k / 8.1k (−16%) | p=1.0 |
| **apikit @ 11k** (n=4)  | **1/4** | **0/4** | 11.2k / 16.7k (−33%) | p≈0.5 (n.s.) |

Both null on completion — against Qwen's **6/6 vs 0/6** (graphkit) and **4/7 vs 0/10** (apikit). Elision
*fired* every B run (`elided_reads` 4–80, 0 on every A) and **cut reads 16–33%**, but the bytes did not
convert — the recurring Parts 6/8/9 result (byte-reduction ≠ completion), now across a model boundary.

The **why** is sharpest on apikit, and it is not the cap (n=4, `-max-iters 20`). On apikit **almost every
run, both arms, wrote all 5 packages including `api`, yet only one passed** — the end-of-pass probe
verifies each changed pass, so a `5/5-written` non-completion means **`api` was written but fails its
tests**. Direct check of a stagnated sandbox: `health`/`users`/`tasks`/`notes` pass, **`api` fails**
(`307` routing redirects, non-JSON 404). So Gemma's apikit wall is **`api`-correctness**, where Qwen's
was **never reaching `api`** (loading-starved at 3/5, Part 4). Gemma is budget-efficient enough to reach
and write the composer increment; it then gets the *code* wrong. Elision frees loading budget — which
is not the binding constraint for either Gemma floor (graphkit: too tight to bank an increment; apikit:
writing `api` correctly).

### Honest conclusion

The elision lever's **byte-reduction generalises universally** (−21/−5/−21% Qwen across three tasks,
−16/−33% Gemma across two). Its **completion conversion does not** — and the boundary is now two
conditions, one per axis:

- **(task)** a hard *composer* floor — Part 8: inert on flat `datakit`, no floor to clear;
- **(model)** that floor is *re-read-bound* — Part 10: inert on Gemma, whose floors are budget-bound
  (graphkit) or correctness-bound (apikit), neither being the spec re-sweeping elision removes.

Qwen's floors satisfy both because its re-sweep-all-specs habit *is* the floor; Gemma, reading
selectively and emitting no reasoning trace, has no re-read floor for a read-boundary lever to clear.
**Spec elision is therefore not a general fix for Ralph-loop stagnation; it is the fix for a
re-read-bound floor**, which is a property of the *model's behaviour* as much as the task. The lever
stays in (default-on, gate-safe, either helps or is inert — Part 7), now with a measured second model
on the inert side.

### Honest limits

n=4/arm, single session/server, **temp 1.0** (high variance — graphkit baseline passes ranged 4→34).
`-max-iters 20` caps the apikit A/B: 2/4 B and 1/4 A runs hit the cap at `5/5-written`, so the apikit
*completion count* is cap-confounded — but the *correctness-wall* finding (the real apikit result) is
cap-independent and gate-confirmed, and even an uncapped re-run would measure "how often Gemma lucks
into a correct `api` at temp 1.0," not a loading lever. graphkit @ 9k is the clean test (no B run hit
the cap; 0/4 is real). The honest hard limit (an irreducible increment exceeding the window) is
untouched. Rows in `runs.jsonl` (`model` = `gemma-4-26B-A4B-it-oQ4-fp16`, `elide_passing` true/false,
2026-06-09); stderr in `logs/gemma-{graphkit,apikit}-*.log`.

---

## Part 11 — Closing ledger: the line is closed (2026-06-09)

Parts 1–10 opened with one observed failure mode and ended with a measured, twice-bounded lever. This
part closes the investigation *deliberately* — every stagnation mode observed in `runs.jsonl`, mapped
to a measured clearing lever or an explicit out-of-scope call — after running the one experiment that
could still have changed the shipped default (quality under the cure, below).

### The ledger

| stagnation mode (where observed) | disposition |
|---|---|
| **Re-read-bound composer floor** — Qwen `apikit`@11k 0/24, `graphkit`@11k 1/20 without elision | **Cleared, shipped.** Built-in read-boundary spec elision (Parts 5–7): `apikit` 4/7 (p≈0.015 pooled / 0.035 interleaved), `graphkit` 6/6 vs 0/6 (p≈0.0011 — the one result here that survives a Holm correction over the doc's ~10 completion tests); the rate is session-variable upward (clean Part-9 controls 15/16; this part's runs 3/3). |
| **Increment-fit floor under the cure** — Qwen `apikit`@9k: 2/18 *with* elision | **Out of scope: hard-limit territory.** The wall is the `api` increment itself (Part 9: L2 null 1/8 vs 1/8 at this floor); when one increment's working set ~fills the window, no read-boundary lever applies — Part 1's honest hard limit, relocated, not refuted. |
| **Budget-bound floor** — Gemma `graphkit`@9k 0/4 vs 0/4 | **Out of scope: model × budget.** Too tight to bank one increment per pass; elision fired (−16% bytes) and converted nothing (Part 10). |
| **Correctness-bound wall** — Gemma `apikit`@11k writes 5/5, `api` fails its tests | **Out of scope: model capability.** The gate refusing wrong code is the gate working; a loading lever cannot fix routing bugs (Part 10). |
| **Stochastic flat-task stragglers** — `datakit` baseline 3/6@8k vs 5/6@6k | **Out of scope: noise, no floor.** Non-monotonic in budget, so budget is not binding; no significant elision effect, the pro-elision trend disclosed and underpowered (Part 8). |
| **Unsatisfiable spec** — the `stuck` example | **By design.** The stagnation guard halting is the correct outcome, not a mode to clear. |

### Lever 3, waived — not forgotten

Lever 3 (soft-limit checkpoint, Part 2) is the one proposed lever never built; that is now a decision
recorded with reasons, not an omission. (1) Its Part-2 build trigger — "passes still die
mid-increment" — turns out to be near-tautological: at the `apikit` 9k floor, 152 of 153
stagnating-run passes end by `context`, but so do the passes of *completing* runs (Gemma
`graphkit`@11k completed with all 7 passes cut; this part's elide-judge-1 completed via the probe
with its final pass cut). A context-cut pass loses little durable progress — the filesystem already
holds the writes and the end-of-pass probe already verifies them — so the checkpoint's value-add over
probe+filesystem is marginal on every floor actually observed. (2) The track record prices it: both
behaviour-shaped levers nulled (Lever 1 digest 0/3 vs 0/3; the Part-1-era prompt nudges 0–1/5), and
the mechanical Lever 2 nulled at the floor it targeted (1/8 vs 1/8). **Reopen trigger:** a measured
floor whose passes repeatedly end with *unverified mid-increment work the probe then fails* —
durable-progress loss, not loading cost — is the one signature that would justify building it.

### The terminal experiment — quality under the cure (2026-06-09)

`judgments.jsonl` had **zero elision-era rows** (it stopped 2026-06-06), so the one question that
could still flip the shipped default — does the cure trade code *quality* for completion? — was open.
Run: three elide-mode `apikit`@11k completions (3/3 completed: 2, 4, 8 passes — the high session-rate
regime again) plus a 26k one-shot comparator (1 pass; elision never fires in a first pass), each
judged head-to-head against one fresh Sonnet baseline by four independent Opus sessions per the
`judge` skill (blind to tests; rows `apikit-2026-06-09T19:47:27Z`…`20:02:30Z` in `judgments.jsonl`;
candidates preserved under `logs/candidates/`).

| candidate | passes | subject | Sonnet baseline (same code, re-scored per session) | gap |
|---|--:|--:|--:|--:|
| elide-1 | 2 | 0.69 | 0.71 | −0.02 |
| elide-2 | 4 | 0.65 | 0.70 | −0.06 |
| elide-3 | 8 | 0.58 | 0.78 | −0.21 |
| 26k one-shot | 1 | **0.79** | 0.71 | **+0.08** |

Reading (ordinal; trust gaps): the elide-mode gaps (−0.02…−0.21) are *smaller* than the pre-elision
apikit one-shots' (0.55/0.64 vs 0.83 → −0.28/−0.19, the 2026-06-06 rows) — cure-era output sits inside
the model's historical band, and the parity pre-commitment is met: **no evidence elision degrades
quality.** Two structural notes, honestly held. (1) Quality declines monotonically with pass count
(1→0.79, 2→0.69, 4→0.65, 8→0.58; n=4, single sessions) — the plausible cost is *fragmented multi-pass
construction*, not elision, which only removes re-reads of already-green specs; the clean attribution
test (elide-off multi-pass completions at 11k) cannot exist — they are 0/24, the very reason the lever
ships. (2) The same baseline code re-scored 0.70–0.78 across four fresh sessions — the judge's own
single-session noise, and why gaps, not absolutes. Bonus, and a caution about the bar itself: **all
four blind sessions independently flagged a real slice-aliasing bug in the *Sonnet* baseline**
(`byID` holding pointers into a reallocating `[]T`; list-vs-byID divergence after growth) that its
green tests never catch — the over-fit-to-tests gap the blind judge exists to measure, found this
time on the *reference* side.

### Closed

The failure mode this document set out to fix — re-orientation starvation on the target model — is
measured, mechanised away by a default-on, gate-safe, ablatable lever, replicated on a second composer
task, bounded on the task axis (Part 8) and the model axis (Part 10), and now quality-checked under
the cure. What still stagnates does so from *different* diseases, each dispositioned above.
Stagnation-in-general is not "solved" — Part 10's two-condition boundary stands — but no further lever
is planned: **this line of work is closed.** Reopen criteria, so the closure stays falsifiable: a
third model with a demonstrably re-read-bound floor that elision fails to clear (breaks the model-axis
boundary); the durable-progress signature above (builds Lever 3); or an elision-attributable quality
regression at matched pass count (revisits the default).
