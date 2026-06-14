# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with
code in this repository. It is the **agent contract**: the engineering
philosophy and the cross-file invariants below override default instincts. For
the conceptual overview, the full architecture narrative, the quickstart, and
the findings, see [README.md](README.md).

## What this is

A benchmarking project: it measures how small local LLMs behave when run as
autonomous coding agents — where they stagnate, how their code quality compares
to frontier baselines, and which harness levers measurably move completion. The
instrument is the harness in this repo — a Go implementation of the "Ralph
loop": it runs a single fixed prompt against a local LLM, letting the model act
through tools until the task **verifiably** completes, with no human in the
loop. The measurements are the product (`docs/stagnation.md`, `logs/runs.jsonl`,
`logs/judgments.jsonl`); the default subject model is
`Qwen3.6-35B-A3B-oQ6-mtp` served by a local **oMLX** server
(OpenAI-compatible API) on an Apple Silicon Mac. See [README.md](README.md) for
the full overview; the rest of this file is the parts an agent modifying the
repo must not get wrong.

## Engineering philosophy (read first)

These constraints are deliberate and override default instincts:

- **Minimal over complete.** Disciplined minimalism, not maximal features. "World-class" here means the smallest correct design, not the most capable one — do not over-engineer.
- **Ask before adding.** Do not introduce new tools, features, abstractions, or dependencies without asking first. Grow the system incrementally from the existing seed.
- **Standard library only.** No third-party dependencies. If one seems genuinely warranted, stop and make the case before adding it.
- **Go is the only language.** No bash/python/js for tooling, scripts, or orchestration — that is the entire reason this harness exists in Go rather than as a shell loop. The agent itself may only run the Go toolchain (see the `go` tool's allowlist).
- **Every change stays green:** `gofmt`, `go vet`, `go test ./...`, and `golangci-lint run` (config in `.golangci.yml`) must all pass. golangci-lint is a local dev gate, not a project dependency — it adds nothing to `go.mod`, so the standard-library-only rule above still holds for shipped code.

## Commands

The development green gate — keep all of these passing:

```bash
go build ./...                                    # compile
go vet ./...                                      # static checks
go test ./...                                     # all tests
go test ./internal/llm -run TestCompleteStream    # a single test
gofmt -w .                                        # format
golangci-lint run                                 # lint — config in .golangci.yml (requires golangci-lint v2.x)
```

Running the harness, the bundled examples (`go run ./cmd/example <name>`), the
`-memory` ablation, and the full flag list are documented in
[README.md](README.md) and `go run ./cmd/harness -h`.

## Architecture

Two nested loops; understanding the split is essential (full narrative in
[README.md](README.md)):

- **Inner loop** (`agent.Session.Run`, `internal/agent/loop.go`): one tool-use session — call model → run tool calls → feed results back → repeat — until the model stops, the task completes, or a budget (max-steps / context tokens) trips.
- **Outer loop** (the `for` in `cmd/harness/main.go`): the Ralph loop. It re-runs `Session.Run` with a **fresh context** each pass; state survives between passes only through the **filesystem** — the code being written, plus a `PROGRESS.md` the agent maintains — never in memory. This is how the harness exceeds a single context window. Between passes it fingerprints the workspace (`fingerprint` in `cmd/harness/fingerprint.go`, excluding `PROGRESS.md` via `scratchFiles`) to drive three things: the **stagnation guard** (stop after `-max-stale` unchanged passes — and report a run where *every* pass errored as a fault, not stagnation), the **`-memory` ablation** (`wipeScratch` drops the cross-pass notes), and an **end-of-pass verification probe** (`probeComplete`, which reuses the `done` gate's `Verifier` to finish a run whose work verifies even if the model never called `done`). On exit it appends one JSON line to `<log-dir>/runs.jsonl` (`runlog.go`), written outside the sandbox, never seen by the agent.

Packages: `internal/llm` (HTTP client + DTOs for the OpenAI-compatible API), `internal/tool` (registry + built-in tools), `internal/agent` (the inner session loop). `cmd/harness` wires them together and owns the Ralph loop and the system prompt. `cmd/example` copies a bundled example's seed to `./sandbox` and launches the harness against it. `examples/` holds the example tasks — each a `PROMPT.md` plus a `workspace/` seed that ships the spec (a test) but no implementation, each its own Go module so a deliberately-unimplemented seed is excluded from the repo's own `go test ./...` and never reddens the build.

**Code quality is a separate, out-of-loop axis.** The done gate and `runs.jsonl` answer only *did it verifiably work*. How *good* the code is (idiomaticity, simplicity, contract fidelity, robustness) is scored out of band by the `judge` skill (`.claude/skills/judge/SKILL.md`) — an Opus-as-a-judge rubric a human runs after a run, blind to the spec's tests and head-to-head against a real Sonnet-medium baseline, appending to `logs/judgments.jsonl`. It is **never a gate and never seen by the agent**, so the local model has nothing to optimise against; that is what keeps it honest (details in [README.md](README.md)).

### Invariants that span files (don't break these)

- **Reasoning is never stored in history.** Qwen emits a thinking trace (`reasoning_content`); `llm.SplitReasoning` separates it and `loop.go` appends the assistant turn *without* it. Re-sending it would violate Qwen's multi-turn contract and waste the context window.
- **Completion runs the verifier; it is never a bare return value.** Two paths end the run as "done," and both invoke the *same* `Verifier`: (1) the model calls `tool.Done` inside a pass, which returns `tool.ErrCompleted` *only if verification passes* (`agent.runTool` detects it via `errors.Is`); (2) the outer loop's end-of-pass probe (`probeComplete` in `cmd/harness/main.go`) runs that verifier itself when a pass left the workspace changed without a successful `done`, completing the run if it passes — so finishing the work but forgetting to call `done` no longer costs an extra pass. The `done` call is an early-signal optimization; the outer loop is authoritative. Because both gates are the same `Verifier`, every anti-gaming guarantee below holds whichever one fires. A failing verification is fed back to the model as an ordinary tool result so it keeps working. As a guard, if `done` fails `maxDoneFails` times in one pass with no file-mutating tool call in between, the session ends the pass early (`reason=done_loop`) so a model looping on an unsatisfiable check can't burn the step budget; the cross-pass stagnation guard then halts the run.
- **"Passes" means the tests ran and passed, not that the process exited 0.** For a `go test` gate (`VerifierFor` routes it to `GoTestVerifier`), the verifier runs `go test -json -count=3` and parses the event stream (`analyzeGoTest` in `done.go`): it accepts only when ≥1 test-level pass is seen, with no failure and no test left unfinished. A test binary that exits early — e.g. `init(){ os.Exit(0) }` in a non-test file — prints `ok` and exits 0 while running no tests; trusting exit status alone would mark that "done." `-count=3` defeats the test cache (a fresh execution is always observed) and runs each test three times in one process, so an order-dependent flaky test — e.g. one sensitive to Go's randomized map iteration — gets three independent chances to fail the gate; this is a probabilistic guard against non-determinism, not a guarantee, since a low-rate flake can still pass all three rolls. The verdict is still only as trustworthy as the test binary's own `-json` markers, though: a non-test `.go` file linked into that binary can forge them — an `init()` that prints a `--- PASS` line for a fabricated test, then `os.Exit(0)` before the real spec runs, counterfeits a test-level pass the parser accepts (`-protect-tests` guards `*_test.go`, not code beside it). So the ≥1-pass rule defeats the *accidental* exit-0/no-test case, not a *deliberate* forgery; closing that fully would mean not executing adversary-authored code in the deciding process, which the harness does not attempt — an accepted limitation for the local, non-adversarial model it targets. A non-`go test` `-verify` command falls back to `CommandVerifier` (exit-status only), which carries this caveat.
- **Execution is Go-toolchain-only.** The `go` tool (`gocmd.go`) and the verifier (`done.go`) both run through `exec.go`'s `runCmd` (or `runCmdFull`, the unclipped variant the `-json` verifier parses), and `go` is restricted to an allowlist — both the subcommand (`build`/`test`/`vet`/`fmt`/`mod`) and its arguments (`checkGoArgs` in `gocmd.go`, which permits only benign flags): without the argument check, flags like `-exec`/`-toolexec`/`-o`/`-overlay` would turn an allowlisted `go` call into a launcher for arbitrary programs or writes outside the sandbox. The argument allowlist is not sufficient on its own, because `go` also reads flags from the environment: the same subprocess therefore runs with a scrubbed environment (`sandboxedGoEnv` in `exec.go` strips `GOFLAGS`/`GOENV`/`GOTOOLCHAIN` and pins them inert, and pins `CGO_ENABLED=0` so an inherited `CC`/`CGO_*` cannot make `go test` run a C compiler — the example tasks are pure Go), since an inherited `GOFLAGS=-exec=…` would inject those very flags out of band — `checkGoArgs` only inspects the explicit argument list. The agent cannot set env vars, so this closes the contaminated-operator/CI path as defense-in-depth, not an agent-reachable hole. There is intentionally no general shell/`run` tool.
- **Filesystem tools are sandboxed.** Every path is resolved through `safeJoin`, which keeps access inside the workspace root.
- **Tests are the spec; the agent cannot author them.** With `-protect-tests` (default on), `write_file`/`edit_file` refuse `*_test.go` paths (`isTestFile` in `fs.go`). This closes a reward-hacking path: otherwise a model can pass the `go test` done-gate by gutting a test or adding a `TestMain` shim that exits 0 instead of implementing the code. A verifier is only trustworthy if the model cannot write the spec it is graded against. Protecting `*_test.go` is necessary but not sufficient — a *non-test* `.go` file can still short-circuit the run (`init(){ os.Exit(0) }`), which is why the done-gate independently checks the tests ran and passed (see "Passes" above). That check defeats the naive short-circuit but not a non-test file that *forges* the pass markers outright, so the gate raises the cost of cheating rather than making it impossible.
- **Spec elision is verifier-fed and only shrinks context, never disk.** Once a package's tests pass, `read_file` returns a one-line notice instead of its `*_test.go` bytes (`ElideState` in `fs.go`), so a fresh pass does not re-spend its budget re-reading satisfied specs — the dominant cost behind the re-orientation floor (`docs/stagnation.md`). It is on by default; `-elide-passing=false` leaves the `ElideState` nil and ablates it (the OFF arm, restored so the floor it clears can be measured on a second model — see `docs/stagnation.md`): when on, the passing-package set is fed by `GoTestVerifier`'s `onPass` hook from the *same* `go test -json` run the done gate and end-of-pass probe already execute (`passingDirsFrom` in `done.go`), so elision adds no test execution beyond what the gate already runs. It only ever shrinks the string returned into context; disk is untouched, so the verifier still compiles and runs the real specs and a false-green stays structurally impossible — this holds even if the set goes briefly stale, since the set carries forward (never clears) only when a verifier run cannot start at all, and a regression surfaces as a test *failure* that the same `onPass` folds in (dropping the package). A non-`go test` verify command never feeds the set, so elision is a no-op there.
- **Streaming is transparent to the loop.** `CompleteStream` assembles SSE frames (accumulating tool-call arguments per `index`) into the *same* `*Response` shape as `Complete`, so the loop logic is identical whether or not `-stream` is set.
- **Transient LLM failures are retried, not fatal.** `llm.Retryable` flags transport errors, truncated reads, and 5xx responses as transient; `agent.complete` retries those with backoff (`maxRetries`) before failing the pass. A cancelled context, 4xx, and decode errors fail immediately. A streamed response that ends before the `[DONE]` sentinel *and* without a finish reason counts as a truncated read too, so `CompleteStream` retries a body cut mid-generation instead of returning the partial turn as complete (a clean mid-stream EOF is reported by `bufio.Scanner` as success, so it must be caught explicitly). Separately, when the model names a tool that isn't registered, `runTool` feeds back an error listing the valid tools so it can recover instead of looping on a malformed call.
