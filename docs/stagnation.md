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
