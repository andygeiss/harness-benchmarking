# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

An autonomous AI agent harness — a Go implementation of the "Ralph loop": it runs a single fixed prompt against a local LLM, letting the model act through tools until the task **verifiably** completes, with no human in the loop. The target model is `Qwen3.6-35B-A3B-oQ6-fp16-mtp` served by a local **oMLX** server (OpenAI-compatible API) on an Apple Silicon Mac.

## Engineering philosophy (read first)

These constraints are deliberate and override default instincts:

- **Minimal over complete.** Disciplined minimalism, not maximal features. "World-class" here means the smallest correct design, not the most capable one — do not over-engineer.
- **Ask before adding.** Do not introduce new tools, features, abstractions, or dependencies without asking first. Grow the system incrementally from the existing seed.
- **Standard library only.** No third-party dependencies. If one seems genuinely warranted, stop and make the case before adding it.
- **Go is the only language.** No bash/python/js for tooling, scripts, or orchestration — that is the entire reason this harness exists in Go rather than as a shell loop. The agent itself may only run the Go toolchain (see the `go` tool's allowlist).
- **Every change stays green:** `gofmt`, `go vet`, `go test ./...`, and `golangci-lint run` (config in `.golangci.yml`) must all pass. golangci-lint is a local dev gate, not a project dependency — it adds nothing to `go.mod`, so the standard-library-only rule above still holds for shipped code.

## Commands

```bash
go build ./...                                    # compile
go vet ./...                                      # static checks
go test ./...                                     # all tests
go test ./internal/llm -run TestCompleteStream    # a single test
gofmt -w .                                        # format
golangci-lint run                                 # lint — config in .golangci.yml (requires golangci-lint v2.x)

# Run the harness (expects an oMLX server at http://localhost:1234/v1):
go run ./cmd/harness -prompt task.md -workdir ./sandbox
#   -stream   stream model tokens live to stderr
#   -debug    log the model's reasoning trace
#   -verify   verification command for the done-gate (default: "go test ./...")
#   -log-dir  append a per-run JSONL record here (default: "logs"; empty disables)
#   -protect-tests  refuse agent writes to *_test.go (default: true)
#   -memory   carry PROGRESS.md across passes as the agent's plan memory (default: true; -memory=false ablates it)

# Run a bundled example (copies its seed to ./sandbox, then runs the harness):
go run ./cmd/example reverse
go run ./cmd/example calc -stream
go run ./cmd/example todo            # htmx web app: net/http + html/template + embed
go run ./cmd/example graphkit        # six-package graph library: the large, multi-pass task

# Ablate the agent's cross-pass memory (drops the PROGRESS.md guidance and wipes
# the file before each pass) to measure resumption from the persisted code alone.
# NB: the default 35B model one-shots the small examples, so they do not actually
# span passes here — the ablation matters for tasks/models that do, e.g. the larger
# graphkit example (examples/README.md).
go run ./cmd/example calc -memory=false
```

Defaults target the local setup (model name, `:1234` endpoint, Qwen3 thinking-mode sampling: temp 0.6 / top_p 0.95 / top_k 20). All are overridable by flag — see `go run ./cmd/harness -h`.

## Architecture

Two nested loops; understanding the split is essential:

- **Inner loop** (`agent.Session.Run`, `internal/agent/loop.go`): one tool-use session — call model → run tool calls → feed results back → repeat — until the model stops, the task completes, or a budget (max-steps / context tokens) trips.
- **Outer loop** (the `for` in `cmd/harness/main.go`): the Ralph loop. It re-runs `Session.Run` with a **fresh context** each pass. State survives between passes only through the **filesystem** — the code being written, plus a `PROGRESS.md` the agent is told to maintain — never in memory. This is how the harness exceeds a single context window. Between passes it fingerprints the workspace (`fingerprint` in `cmd/harness/fingerprint.go`); if `-max-stale` consecutive passes leave it unchanged (default 3, 0 disables), the loop stops early instead of spending the remaining budget on a stuck model. A run in which *every* pass errored (e.g. the model endpoint was unreachable) is reported as a fault, not stagnation or budget exhaustion, so a transport outage is not misread as a stuck model. The fingerprint excludes `PROGRESS.md` (`scratchFiles`): the agent is told to rewrite it every pass, so counting that churn would reset the stale counter and blind the guard to a model that is stuck but still taking notes. That same `scratchFiles` set powers the **`-memory` ablation**: with `-memory=false` the loop drops the `PROGRESS.md` guidance from the system prompt (`systemPrompt`) and wipes `PROGRESS.md` before every pass (`wipeScratch`), so a run measures how well the model resumes from the persisted code alone. The `calc` example is a staged task (lexer → parser → eval) meant to exercise resume, though the current 35B model completes it in a single pass regardless of the per-pass budget, so the cross-pass handoff is not actually triggered there. The **`graphkit`** example — a six-package graph library (~15 functions across `graph`/`traverse`/`paths`/`toposort`/`components`/`spanning`, ~730 lines) — is the deliberately larger task built to force the multi-pass resume `calc` cannot; but a measured run completes it in a single pass too (`passes: 1`, 23 steps, ~4 min), because ~730 lines is not enough tokens to overflow a 52k-token pass on this model (whose window is 64k+ and whose prompt-cache made that run ~79% cached). Forcing genuine cross-pass resume needs a capped `-ctx-limit` set *below the task's ~15–16k single-pass peak* (a measured 27B-oQ4 run at `-ctx-limit 16384` still one-shot — reasoning is stripped from history, writes are compact, ~87% of the prompt cached — so ~10000–12000 is the working range), a far larger task, or a weaker model (see `examples/README.md`). The `-memory` ablation applies to both. That same between-pass fingerprint also drives an **end-of-pass verification probe** (`probeComplete`): when a pass leaves the workspace changed but the model stopped without a successful `done`, the loop runs the verifier itself and completes the run if it passes — so a model that finishes the work but forgets to signal completion doesn't cost an extra pass. The probe shares the `done` gate's `Verifier`, so its strictness is identical. On exit the loop appends one JSON line to `<log-dir>/runs.jsonl` (`runlog.go`; `-log-dir`, default `logs`) capturing the config, the task that ran, outcome, and aggregate token/timing metrics — written outside the sandbox by the outer loop, never seen by the agent.

**Code quality is a separate, out-of-loop axis.** The done gate and `runs.jsonl` answer only *did it verifiably work* — a binary. How *good* the resulting code is (Go idiomaticity, simplicity, contract fidelity, robustness) is scored out of band by the `judge` skill (`.claude/skills/judge/SKILL.md`): an Opus-as-a-judge rubric a human runs after a run, never the agent. It reads the produced code **blind to the spec's tests** — so it can catch code that passes the gate by over-fitting them — rates six Go-specific dimensions on a 0–1 scale, scoring it head-to-head against a real Sonnet solution to the same contract (Opus referees both candidates, blind), and appends one line per candidate to `logs/judgments.jsonl`, a sibling to `runs.jsonl` that is likewise gitignored and never seen by the agent. Each record also carries, beside the six subjective scores, a **deterministic** `modernize`-finding count — and an optional paired `modernize --fix` score uplift — a reproducible, noise-free idiomaticity signal computed out of band; the harness itself never runs the linter, so this adds no dependency to the loop and nothing for the local model to optimise against. It is a measurement instrument, never a gate: keeping it outside the loop is what keeps it honest, since the local model then has nothing to optimise against. The skill is committed and travels with the repo; its output does not.

Packages under `internal/`: `llm` (HTTP client + DTOs for the OpenAI-compatible API), `tool` (registry + built-in tools), `agent` (the inner session loop). `cmd/harness` wires them together and owns the Ralph loop and the system prompt. `cmd/example` is a convenience runner: it copies a bundled example's seed to `./sandbox` and launches the harness against it. `examples/` holds the example tasks — each a `PROMPT.md` plus a `workspace/` seed that ships the spec (a test) but no implementation. Every seed is its own Go module, so a deliberately-unimplemented seed is excluded from the repo's own `go test ./...` and never reddens the build.

### Invariants that span files (don't break these)

- **Reasoning is never stored in history.** Qwen emits a thinking trace (`reasoning_content`); `llm.SplitReasoning` separates it and `loop.go` appends the assistant turn *without* it. Re-sending it would violate Qwen's multi-turn contract and waste the context window.
- **Completion runs the verifier; it is never a bare return value.** Two paths end the run as "done," and both invoke the *same* `Verifier`: (1) the model calls `tool.Done` inside a pass, which returns `tool.ErrCompleted` *only if verification passes* (`agent.runTool` detects it via `errors.Is`); (2) the outer loop's end-of-pass probe (`probeComplete` in `cmd/harness/main.go`) runs that verifier itself when a pass left the workspace changed without a successful `done`, completing the run if it passes — so finishing the work but forgetting to call `done` no longer costs an extra pass. The `done` call is an early-signal optimization; the outer loop is authoritative. Because both gates are the same `Verifier`, every anti-gaming guarantee below holds whichever one fires. A failing verification is fed back to the model as an ordinary tool result so it keeps working. As a guard, if `done` fails `maxDoneFails` times in one pass with no file-mutating tool call in between, the session ends the pass early (`reason=done_loop`) so a model looping on an unsatisfiable check can't burn the step budget; the cross-pass stagnation guard then halts the run.
- **"Passes" means the tests ran and passed, not that the process exited 0.** For a `go test` gate (`VerifierFor` routes it to `GoTestVerifier`), the verifier runs `go test -json -count=1` and parses the event stream (`analyzeGoTest` in `done.go`): it accepts only when ≥1 test-level pass is seen, with no failure and no test left unfinished. A test binary that exits early — e.g. `init(){ os.Exit(0) }` in a non-test file — prints `ok` and exits 0 while running no tests; trusting exit status alone would mark that "done." `-count=1` defeats the test cache so a fresh execution is always observed. A non-`go test` `-verify` command falls back to `CommandVerifier` (exit-status only), which carries this caveat.
- **Execution is Go-toolchain-only.** The `go` tool (`gocmd.go`) and the verifier (`done.go`) both run through `exec.go`'s `runCmd` (or `runCmdFull`, the unclipped variant the `-json` verifier parses), and `go` is restricted to an allowlist (`build`/`test`/`vet`/`fmt`/`mod`). There is intentionally no general shell/`run` tool.
- **Filesystem tools are sandboxed.** Every path is resolved through `safeJoin`, which keeps access inside the workspace root.
- **Tests are the spec; the agent cannot author them.** With `-protect-tests` (default on), `write_file`/`edit_file` refuse `*_test.go` paths (`isTestFile` in `fs.go`). This closes a reward-hacking path: otherwise a model can pass the `go test` done-gate by gutting a test or adding a `TestMain` shim that exits 0 instead of implementing the code. A verifier is only trustworthy if the model cannot write the spec it is graded against. Protecting `*_test.go` is necessary but not sufficient — a *non-test* `.go` file can still short-circuit the run (`init(){ os.Exit(0) }`), which is why the done-gate independently verifies the tests ran and passed (see "Passes" above).
- **Streaming is transparent to the loop.** `CompleteStream` assembles SSE frames (accumulating tool-call arguments per `index`) into the *same* `*Response` shape as `Complete`, so the loop logic is identical whether or not `-stream` is set.
- **Transient LLM failures are retried, not fatal.** `llm.Retryable` flags transport errors, truncated reads, and 5xx responses as transient; `agent.complete` retries those with backoff (`maxRetries`) before failing the pass. A cancelled context, 4xx, and decode errors fail immediately. Separately, when the model names a tool that isn't registered, `runTool` feeds back an error listing the valid tools so it can recover instead of looping on a malformed call.
