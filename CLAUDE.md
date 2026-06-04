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
- **Every change stays green:** `gofmt`, `go vet`, and `go test ./...` must all pass.

## Commands

```bash
go build ./...                                   # compile
go vet ./...                                      # static checks
go test ./...                                     # all tests
go test ./internal/llm -run TestCompleteStream    # a single test
gofmt -w .                                        # format

# Run the harness (expects an oMLX server at http://localhost:1234/v1):
go run ./cmd/harness -prompt task.md -workdir ./sandbox
#   -stream   stream model tokens live to stderr
#   -debug    log the model's reasoning trace
#   -verify   verification command for the done-gate (default: "go test ./...")

# Run a bundled example (copies its seed to ./sandbox, then runs the harness):
go run ./cmd/example reverse
go run ./cmd/example calc -stream
```

Defaults target the local setup (model name, `:1234` endpoint, Qwen3 thinking-mode sampling: temp 0.6 / top_p 0.95 / top_k 20). All are overridable by flag — see `go run ./cmd/harness -h`.

## Architecture

Two nested loops; understanding the split is essential:

- **Inner loop** (`agent.Session.Run`, `internal/agent/loop.go`): one tool-use session — call model → run tool calls → feed results back → repeat — until the model stops, the task completes, or a budget (max-steps / context tokens) trips.
- **Outer loop** (the `for` in `cmd/harness/main.go`): the Ralph loop. It re-runs `Session.Run` with a **fresh context** each pass. State survives between passes only through the **filesystem** — the code being written, plus a `PROGRESS.md` the agent is told to maintain — never in memory. This is how the harness exceeds a single context window. Between passes it fingerprints the workspace (`fingerprint` in `cmd/harness/fingerprint.go`); if `-max-stale` consecutive passes leave it byte-for-byte unchanged (default 3, 0 disables), the loop stops early instead of spending the remaining budget on a stuck model.

Packages under `internal/`: `llm` (HTTP client + DTOs for the OpenAI-compatible API), `tool` (registry + built-in tools), `agent` (the inner session loop). `cmd/harness` wires them together and owns the Ralph loop and the system prompt. `cmd/example` is a convenience runner: it copies a bundled example's seed to `./sandbox` and launches the harness against it. `examples/` holds the example tasks — each a `PROMPT.md` plus a `workspace/` seed that ships the spec (a test) but no implementation. Every seed is its own Go module, so a deliberately-unimplemented seed is excluded from the repo's own `go test ./...` and never reddens the build.

### Invariants that span files (don't break these)

- **Reasoning is never stored in history.** Qwen emits a thinking trace (`reasoning_content`); `llm.SplitReasoning` separates it and `loop.go` appends the assistant turn *without* it. Re-sending it would violate Qwen's multi-turn contract and waste the context window.
- **Completion is a sentinel, not a return value.** `tool.Done` runs the verification command and returns `tool.ErrCompleted` *only if it passes*; `agent.runTool` detects that via `errors.Is` to end the run successfully. A failing verification is fed back to the model as an ordinary tool result so it keeps working. This is the only path that terminates the loop as "done."
- **Execution is Go-toolchain-only.** The `go` tool (`gocmd.go`) and the verifier (`done.go`) both run through `exec.go`'s `runCmd`, and `go` is restricted to an allowlist (`build`/`test`/`vet`/`fmt`/`mod`). There is intentionally no general shell/`run` tool.
- **Filesystem tools are sandboxed.** Every path is resolved through `safeJoin`, which keeps access inside the workspace root.
- **Streaming is transparent to the loop.** `CompleteStream` assembles SSE frames (accumulating tool-call arguments per `index`) into the *same* `*Response` shape as `Complete`, so the loop logic is identical whether or not `-stream` is set.
