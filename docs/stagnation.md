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
each step (`internal/agent/loop.go:153`). Each pass starts from an empty conversation
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

## Part 5 — Measured: elide-on-pass clears the floor in 2 of 3 (2026-06-07)

The "elide-on-pass" lever was built behind `-elide-passing` (default off) and A/B-tested. When
the verifier certifies a package green, `read_file` returns a one-line notice instead of that
package's `*_test.go` bytes, so a fresh pass cannot re-spend its budget re-reading specs already
satisfied. It is **mechanical, not behavioural**: the model may still issue the read (the habit
Part 3 showed is immovable), but the spec's bytes do not re-enter the window. Disk is untouched —
the `go` tool and the done gate still compile and run the real files, so a false-green is
structurally impossible (the gate reads the real `*_test.go`, never the stub).

**A/B, `apikit` @ `-ctx-limit 11000`, n=3 each, interleaved (B,A,B,A,B,A), else identical:**

| arm | completed | passes | elided_reads | read_bytes/pass |
|---|---|--:|--:|--:|
| elide (B)    | **2 / 3** | 4, 5, 6 | 7, 14, 5 | ~16.7k, ~16.2k, ~20.7k |
| baseline (A) | 0 / 3     | 8, 5, 4 | 0        | ~22.9k, ~23.4k, ~21.5k |

Both completed elide runs reached full **5/5 including `api`** — the increment baseline never
reaches. Across **all** 11k baseline runs ever recorded (these 6 — the 3 here plus the 3 in
Part 4 — and the 8 in Part 1) the score is **0/14**; the elide arm's two completions are the first
the harness has produced at this floor by any change other than raising the budget.

- **Mechanism confirmed.** Elision fired in every elide run (`elided_reads` 7 / 14 / 5), and
  `read_bytes`/pass fell **~22%** (baseline ~22.9k → elide ~17.9k). The two completers cut load
  hardest (~16.5k/pass); the one that stagnated elided least (5) and stayed baseline-like.
- **Kill criterion met the "pursue" side.** It was: revert iff completion stays 0/3 *and*
  read-share does not fall. Completion went **0 → 2/3** and read-share fell — so keep it.

**Honest limits.** n=3 is suggestive, not conclusive: Fisher one-tailed on 2/3 vs 0/6 is
**p≈0.08** (≈0.02 folding in the historical 0/8, though that mixes sessions). It is **not a
deterministic fix** — 1/3 elide runs still stagnated, matching the prediction that elision raises
the completion *probability*, not a guarantee. The failure mode is **safe**: the stagnated elide
run ended cleanly, never falsely completing or breaking. So Part 1 / Part 3's "no harness change
clears the floor for this model" is **updated, not overturned**: a mechanical read-path elision
clears it in 2 of 3 runs — the cheap-loading idea works once it acts on what the budget is actually
spent on (spec *bytes* at the read boundary), the half Lever 1 missed.

**Limitations of the lever.** It assumes one package per directory (true for the example tasks)
and runs one extra `go test -json -count=3` per pass to compute the passing set (acceptable here;
could later fold into the end-of-pass probe). It elides only *specs* of green packages, not their
*implementation bodies* — a stacked `go doc`-signatures lever remains if the residual floor needs
it. Rows in `runs.jsonl` (`elide_passing` / `elided_reads`); stderr in
`logs/apikit-{elide,baseline}-*.log`.
