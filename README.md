# harness

An autonomous AI agent harness — a Go implementation of the **"Ralph loop."** It
runs a single fixed prompt against a local LLM and lets the model act through
tools until the task *verifiably* completes, with no human in the loop.

The default target is `Qwen3.6-35B-A3B-oQ4-fp16-mtp` served by a local **oMLX**
server (an OpenAI-compatible API) on an Apple Silicon Mac, but any
OpenAI-compatible endpoint and model work via flags.

> **Working in this repo?** Read [CLAUDE.md](CLAUDE.md) first. It holds the
> engineering philosophy (disciplined minimalism, standard library only, Go as
> the only language) and the cross-file invariants you must not break. This
> README is the conceptual overview; CLAUDE.md is the contract.

## What it does

A run gives the model a task — a prompt plus a workspace seed — and a small set
of sandboxed tools: read / write / edit files, list directories, run the Go
toolchain, and a `done` tool. The model works until it calls `done`; the harness
then runs a **verification command** (default `go test ./...`) and ends the run
only if it passes. Completion is never the model's word for it — it's a gate the
model has to actually pass.

Two properties make that gate hard to game:

- **The tests are the spec, and the model can't author them.** Writes to
  `*_test.go` are refused, so the model can't pass by gutting the very test it is
  graded against.
- **"Passed" means the tests actually ran.** For a `go test` gate the harness
  parses the `-json` event stream and accepts only when real tests passed — a
  binary that prints `ok` and exits 0 without running anything (e.g. an
  `os.Exit` in non-test code) is rejected.

Both raise the cost of cheating rather than making it impossible: the verdict is
parsed from the test binary's own `-json` markers, so a non-test `.go` file
compiled beside the spec could forge a passing one — print a `--- PASS` line,
then `os.Exit` before the real test runs. It's a narrow, documented hole,
acceptable for the local, non-adversarial model this targets; the
[CLAUDE.md](CLAUDE.md) invariants spell it out.

## Architecture

Two nested loops; the split is the whole idea.

### Inner loop — one session

`agent.Session.Run` (`internal/agent/loop.go`): a single tool-use session. Call
the model → run its tool calls → feed the results back → repeat, until the model
stops, the task completes, or a budget trips (max steps, or context tokens).

### Outer loop — the Ralph loop

The `for` in `cmd/harness/main.go`. It re-runs the session with a **fresh
context** every pass. State survives between passes only on the **filesystem** —
the code being written, plus a `PROGRESS.md` the agent maintains as its plan
memory — never in process memory. That is how a run can exceed a single context
window.

Between passes the loop:

- **Fingerprints the workspace** and stops early if `-max-stale` consecutive
  passes change nothing — a stuck model — instead of burning the whole budget.
  `PROGRESS.md` is excluded from the fingerprint, since the agent rewrites it
  every pass.
- **Runs an end-of-pass verification probe:** if a pass changed the workspace
  but the model stopped without a successful `done`, the loop runs the verifier
  itself and finishes the run if it passes — so doing the work but forgetting to
  signal it doesn't cost an extra pass. The probe shares the `done` gate's
  verifier, so its strictness is identical.
- **Distinguishes outcomes.** Completed, stagnated (stuck), budget (ran out of
  passes), and fault (e.g. *every* pass errored because the endpoint was
  unreachable) get distinct exit codes, so a transport outage is not misread as
  a stuck model.

On exit it appends one JSON line to `logs/runs.jsonl` — config, outcome, and
aggregate token/timing metrics — written outside the sandbox and never seen by
the agent.

### The `-memory` ablation

`-memory=false` drops the `PROGRESS.md` guidance from the system prompt and
wipes the file before each pass, so a run measures how well the model resumes
from the persisted *code* alone. (See [the honest finding](#an-honest-finding-cross-pass-resume)
below on when this actually matters.)

### Packages

- `internal/llm` — HTTP client + DTOs for the OpenAI-compatible API. Streaming
  and non-streaming responses assemble to the same shape, so the loop is
  identical either way.
- `internal/tool` — the tool registry and the built-in tools: filesystem, the
  Go-toolchain runner, and the `done` gate with its verifiers.
- `internal/agent` — the inner session loop.
- `cmd/harness` — wires it together; owns the Ralph loop and the system prompt.
- `cmd/example` — a convenience runner that copies a bundled example's seed into
  `./sandbox` and launches the harness against it.
- `examples/` — the task catalogue (see [examples/README.md](examples/README.md)).

For the precise cross-file invariants — reasoning is never stored in history,
completion always runs the verifier, execution is Go-toolchain-only, filesystem
tools are sandboxed, and the rest — see the **Invariants** section of
[CLAUDE.md](CLAUDE.md).

## Requirements

- Go 1.26+
- A running oMLX (or other OpenAI-compatible) server — the default expects one at
  `http://localhost:1234/v1`
- `golangci-lint` v2.x for the lint gate (a local dev tool only; it adds nothing
  to `go.mod`)

## Quickstart

The development green gate — every change must keep all of these passing:

```bash
go build ./...        # compile
go vet ./...          # static checks
go test ./...         # all tests
gofmt -w .            # format
golangci-lint run     # lint (config in .golangci.yml)
```

Run the harness against a task:

```bash
go run ./cmd/harness -prompt task.md -workdir ./sandbox
```

Useful flags (full list via `go run ./cmd/harness -h`):

- `-stream` — stream model tokens live to stderr
- `-debug` — log the model's reasoning trace
- `-verify` — verification command for the done-gate (default `go test ./...`)
- `-memory=false` — ablate the cross-pass `PROGRESS.md` memory
- `-protect-tests` — refuse agent writes to `*_test.go` (default on)
- `-ctx-limit` / `-max-iters` / `-max-steps` — the per-pass and per-run budgets

Defaults target the local setup (model name, `:1234` endpoint, Qwen3
thinking-mode sampling: temp 0.6 / top_p 0.95 / top_k 20); all are
flag-overridable.

## Examples

Run a bundled example — this wipes `./sandbox`, copies the seed in, and runs the
harness against it (extra flags are forwarded):

```bash
go run ./cmd/example reverse     # rune-aware string reversal — smallest end-to-end check
go run ./cmd/example calc        # arithmetic evaluator: lexer → parser → eval
go run ./cmd/example todo        # htmx web app: net/http + html/template + embed
go run ./cmd/example graphkit    # six-package graph algorithms library
go run ./cmd/example apikit      # modular JSON HTTP API (5 packages) — built for multi-pass runs
go run ./cmd/example stuck       # adversarial: unsatisfiable test, exercises the stagnation guard
```

Each example is a `PROMPT.md` plus a `workspace/` seed that ships the spec (a
test) but no implementation. Every seed is its own Go module, so an
unimplemented seed never reddens the repo's own `go test ./...`. Full catalogue
and details: [examples/README.md](examples/README.md).

## An honest finding: cross-pass resume

The Ralph loop exists to carry a task across context resets — but at the **default
per-pass budget, every example completes in a single pass.** Even `graphkit` (six
packages, ~730 lines) one-shots: ~730 lines is not enough tokens to overflow a
52k-token pass when the model's window is 64k+ and prompt-caching keeps
accumulation cheap. So at default budget the `PROGRESS.md` hand-off and the
`-memory` A/B are effectively un-exercised by real work.

Capping the per-pass budget *below* a task's single-pass peak does drive the loop
into multiple passes. Two measured `graphkit` runs on the oQ4 default bracket it,
and the result is more nuanced than a clean "resume works":

- **`-ctx-limit 11000` → stagnates at 4/6 packages** (12 passes, all `context`).
  Here resume is *real but incomplete*: `graph`, `traverse`, `paths` (including a
  cross-pass fix to its Dijkstra heap) and `toposort` accrete across resets — then
  it stalls, because the model writes `PROGRESS.md` *once* and never updates it, so
  every fresh pass re-derives state by re-reading and re-testing the existing
  packages, exhausting the budget before new code is written. With the plan-memory
  neglected, `-memory=true` quietly degrades to `-memory=false`.
- **`-ctx-limit 16000` → completes in 2 passes** (`context` → `completed`) — the
  first run to finish across a context reset, but a *thin* multi-pass win: pass 1
  writes all six packages and is guillotined one step before it can verify; pass 2
  only resumes, re-verifies, and calls `done` — it writes no new code. A clean
  "implementation split across passes" still hasn't landed on this model; the band
  between stagnation and one-shot is narrow.

**Two nudges, tried and removed.** Because the first bullet pins the stall partly on
neglected notes, an experiment added two opt-in reminders between passes — one to
update `PROGRESS.md` when code advanced without it, one to implement rather than
re-read when a pass made no code progress. Measured on `graphkit`/oQ4, neither
lifted completion. At `-ctx-limit 11000` every run jammed at the same 3-of-6 wall
(baseline 0/4, nudges 0–1/5): resuming means re-reading the done packages plus the
next spec, which fills the 11k window *before* any code can be written — a per-pass
**budget floor** a prompt cannot lift. Raise the budget to 13000 and the floor
clears, but there the baseline already completes on its own (3/4) and the stall
nudge only matches it (3/4). The real lever is per-pass budget (`-ctx-limit`), not
prompting — so both nudges were removed rather than kept as dead weight. (Raw rows
in `logs/runs.jsonl`; single-digit samples, ordinal.)

**Why per-pass budget is the lever.** Digging into that floor isolated the
mechanism. At `-ctx-limit 11000` the model re-reads ~70% of the workspace *every
pass* — 8–9 `.go`-file reads per pass (the six specs plus the implementations) — and
`-memory` does not change it: memory=true and memory=false land at the same rate
(8.2 / 8.4 / 9.2 vs 8.0 / 8.2 / 9.8 reads per pass, three runs each). It is **not**
that the model ignores its notes — traces show it reads `PROGRESS.md` *first* on
every resume pass, as instructed, then re-sweeps the code anyway. The re-sweep is
mostly **structural, not distrust**: to implement the next package the model needs
that package's *test* in context, and `PROGRESS.md` records *status* ("toposort:
todo") but not the *spec content* the model must read to write the code. A reset
context therefore re-pays to load the working set every pass; the part notes could
save — re-verifying already-done packages — is the smaller slice. So the budget floor
is inherent to the Ralph design, and per-pass budget (`-ctx-limit`) is the only lever
that moved completion (11k jams, 13k ~3/4, 16k two-shots) — neither prompting nor the
`PROGRESS.md` memory reduces the re-derivation cost beneath it.

That same completing run also exposed a gap in the gate: the `components` it
certified is **flaky** (its SCC mispartitions on ~8% of runs, by map-iteration
order). at the time, `go test -count=1` defeated the test *cache* but sampled a *single*
execution, so a non-deterministic implementation passed on a lucky roll — a
non-adversarial path to a falsely-green gate, distinct from the adversarial one in
[CLAUDE.md](CLAUDE.md). The gate has since been hardened to `-count=3` (three
independent rolls): enough for high-rate flakes, but at ~8% no guarantee. The full
write-up — including a measured 27B-oQ4 run that also one-shot — is in
[examples/README.md](examples/README.md).

**Confirmed on a second, purpose-built task.** `apikit` — five independent packages,
built for exactly this — reproduces both halves on fresh ground. At `-ctx-limit 26000`
it one-shots (**5/5**, one pass, ~3.7 min); at `-ctx-limit 11000` it never completes —
**8 runs, 0 completions**, all stagnating at 3–4 of 5 packages with `api` reached by
none. A **replicated `-memory` A/B** (n=4 each) settles the memory question on
*outcome*, not read-counts: packages reached `{4,3,4,4}` (memory) vs `{3,3,3,4}` (no
memory) overlap, and "reached 4" (3/4 vs 1/4) is **not significant (Fisher p ≈ 0.5)**.
The first single pair looked like a memory win and replication dissolved it — the n=1
trap the "ordinal, single-digit samples" caveat is about. Detail in
[examples/README.md](examples/README.md).

**The mechanism, quantified — and why more budget is not the fix.** One `apikit`@11k
stagnation log makes the floor concrete: across 12 passes (all cut by `context`, with
`done` never called) the model spent **~192 orientation ops (`read_file` + `list_dir`)
against just 8 file-mutations**, and never once wrote `api`. Each fresh pass splits its
budget between *loading* context — re-reading the spec and code — and *acting* on it;
below a floor, loading crowds out acting entirely, and the **costliest increment sets
that floor**: `api`, which must hold all four feature packages plus its spec at once, is
unreachable in any single pass, so more passes cannot help. The obvious fix — raise
`-ctx-limit` — does not generalise: it caps at the model's context window, and the tasks
a Ralph loop exists for are *larger* than any window, so escalation only walks to the
wall. Completing under a fixed window instead needs, per pass, a **bounded working set**
(the slice a step needs fits the window), **cheap loading** of it, and **durable,
monotonic progress** — and the working set must be bounded by the *interface* a step
touches, not the *implementation* behind it (`api` needs four signatures, not four files).
That is how bounded passes compose into an unbounded task. The full mechanism — and the one
lever that moves it — is in [docs/stagnation.md](docs/stagnation.md). The proposed pass-start
digest was built, A/B-tested, and reverted (null); what works is a now-built-in read-boundary elision
that stubs a spec once its package verifies, so a fresh pass cannot re-spend its budget re-reading
already-satisfied specs — fed by the verifier the loop already runs, so it adds no test execution. At `apikit`@11k it is the first change other
than raising the budget to complete the task — **4/7 runs vs 0 across every recorded baseline**
(Fisher p≈0.015, though fragile at this n) — and it **replicates on a second task, `graphkit`@11k,
6/6 vs 0/6** (p≈0.001, clean separation). It raises completion *probability* rather than guaranteeing
it (3/7 `apikit` runs still stagnate; `graphkit` did not, at n=6). A third, deliberately *flat* task
(`datakit` — five independent packages, no composer) **bounds** the lever: the byte-reduction still
generalises (−21% reads), but the model one-shots the flat task even at a tight budget, so there is
no floor to clear and elision rides along harmlessly — its *completion* benefit needs the hard floor
that a costly composing increment creates, which independent-package kits lack. The proposed
`go doc`-interface and soft-limit-checkpoint levers remain unbuilt.

## Code quality is a separate axis

Passing the gate means the code is *correct*, not that it is *good*; the harness
only gates on the first. The **judge** skill (`.claude/skills/judge/SKILL.md`)
measures the second, out of band: an Opus-as-judge rubric scores the produced
code on six Go dimensions (contract fidelity, simplicity, idiomaticity,
readability, robustness, performance), **blind to the spec's tests** and
head-to-head against a real Sonnet solution to the same contract. Each record
also carries a deterministic `modernize`-finding count as a noise-free
idiomaticity signal. It appends to `logs/judgments.jsonl`, is never a gate, and
is never seen by the agent — keeping it outside the loop is what keeps it honest.

## License

[MIT](LICENSE) © Andy Geiss
