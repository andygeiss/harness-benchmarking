# harness

An autonomous AI agent harness — a Go implementation of the **"Ralph loop."** It
runs a single fixed prompt against a local LLM and lets the model act through
tools until the task *verifiably* completes, with no human in the loop.

The default target is `Qwen3.6-35B-A3B-oQ6-fp16-mtp` served by a local **oMLX**
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
go run ./cmd/example graphkit    # six-package graph library — the largest task
go run ./cmd/example stuck       # adversarial: unsatisfiable test, exercises the stagnation guard
```

Each example is a `PROMPT.md` plus a `workspace/` seed that ships the spec (a
test) but no implementation. Every seed is its own Go module, so an
unimplemented seed never reddens the repo's own `go test ./...`. Full catalogue
and details: [examples/README.md](examples/README.md).

## An honest finding: cross-pass resume

The Ralph loop exists to carry a task across context resets — but on the default
35B model, **every example completes in a single pass.** Even `graphkit` (six
packages, ~730 lines) one-shots: ~730 lines is not enough tokens to overflow a
52k-token pass when the model's window is 64k+ and prompt-caching keeps
accumulation cheap. So the `PROGRESS.md` hand-off and the `-memory` A/B are, on
this model, effectively un-exercised by real work.

To genuinely trigger cross-pass resume you have to cap the budget *below* a
task's single-pass peak (`-ctx-limit ~10000–12000` on graphkit), go dramatically
larger, or use a weaker model that makes mistakes and iterates. The machinery is
kept for exactly those cases. The full write-up — including a measured 27B-oQ4
run that also one-shot — is in [examples/README.md](examples/README.md).

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
