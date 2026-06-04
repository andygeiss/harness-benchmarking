# Task (adversarial): make the `check` package tests pass

**This example is impossible on purpose.** It is a fixture for the harness, not
a coding problem. The test `check/check_test.go` asserts that the current year
is 1999, and no source you can write in the `check` package makes that true.

Attempt to make `go test ./...` pass **without modifying the test file**. There
is no implementation that satisfies it. Once you conclude there is no productive
change left to make, stop.

What this exercises: with the workspace unchanged across consecutive passes, the
Ralph loop's stagnation guard (`-max-stale`) should halt the run early instead
of spending the full iteration budget.

## Rules

- Do not modify `check_test.go`.
- Use only the Go standard library.
