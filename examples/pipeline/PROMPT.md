# Task: a context-aware concurrent fan-out/fan-in pipeline

Implement package `pipeline` so that the provided test
(`pipeline/pipeline_test.go`) passes. The build currently fails because the
package has no implementation.

## Contract

    func Pipeline(ctx context.Context, in []int, workers int, fn func(ctx context.Context, n int) int) ([]int, error)

`Pipeline` applies `fn` to every element of `in` and returns the results **in
the same order as `in`**, computing them concurrently with a **bounded pool of
exactly `workers` goroutines**. Structure it as the three stages the name
implies, connected by channels:

1. a **generator** that emits the input values (tagged with their position) into
   a channel;
2. a **fan-out** of `workers` goroutines, each reading that channel and running
   `fn`;
3. a **fan-in** collector that gathers the results and restores input order.

## Behaviour

- **Order.** `out[i]` must be `fn(ctx, in[i])`. Results complete in an arbitrary
  order across workers, so the collector must place each result by its original
  index — do not append in completion order.
- **Concurrency.** Up to `workers` calls to `fn` run at the same time. `fn`
  receives `ctx` so it can abandon long work when the context is cancelled.
- **Cancellation.** If `ctx` is cancelled before all work completes, stop
  promptly, return `(nil, ctx.Err())`, and **leave no goroutine running** — the
  generator, the workers, and the collector must all unwind. Discard partial
  results; do not return a short slice.
- **Invalid workers.** If `workers < 1`, return a non-nil error without calling
  `fn`.
- **Empty input.** With `workers >= 1` and no input, return an empty slice and a
  nil error. Do not block.

## You create

- `pipeline/pipeline.go` (you may split across files) — the generator, the
  worker pool, the order-restoring collector, and the context plumbing.

## Rules

- Use only the Go standard library (`context`, `sync`, channels, …).
- Do not modify `pipeline_test.go`.
- Use a bounded pool of exactly `workers` goroutines — not one goroutine per
  input element.
- Honour `ctx` cancellation in every stage; do not leak goroutines.
- You are done when `go test ./...` passes.

## How to approach it (this may take several passes)

Your context resets between passes, so build it in stages and track them in
PROGRESS.md (read it first on every pass — it is your only memory across
resets). A clean decomposition:

1. The happy path with no cancellation: generator → `workers` goroutines → an
   index-keyed collector that returns the ordered slice.
2. Thread `ctx` through every stage — `select` on `ctx.Done()` wherever you send
   to or receive from a channel — so a cancelled run returns `ctx.Err()`
   promptly and every goroutine exits.
3. The edges: `workers < 1` and empty input.

Run `go test ./...` after each stage to see how far you have gotten, record the
state in PROGRESS.md, and continue.
