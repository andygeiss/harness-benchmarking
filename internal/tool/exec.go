package tool

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"
)

const maxCmdOutput = 16 << 10 // bytes of command output fed back to the model

// runCmd runs name+args in dir under a timeout and returns the combined output
// clipped to maxCmdOutput, suitable for feeding back to the model. It reports a
// command that ran and failed via failed=true; err is set only if the command
// could not start at all — only the latter is a harness fault. Callers that parse
// structured output use runCmdFull instead, so a record is never truncated.
func runCmd(ctx context.Context, dir string, timeout time.Duration, name string, args ...string) (out string, failed bool, err error) {
	raw, failed, err := runCmdFull(ctx, dir, timeout, name, args...)
	if err != nil {
		return "", false, err
	}
	return clip(raw, maxCmdOutput), failed, nil
}

// runCmdFull is runCmd without the output clip: it returns the FULL combined
// output so a caller that parses structured output (the go-test verifier reading
// a -json stream) is never handed a buffer truncated mid-record. It distinguishes
// a command that ran and failed (failed=true, err=nil) from one that could not
// start at all (err!=nil), and appends a trailing note on timeout or non-zero exit.
func runCmdFull(ctx context.Context, dir string, timeout time.Duration, name string, args ...string) (out string, failed bool, err error) {
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(cctx, name, args...)
	cmd.Dir = dir
	b, runErr := cmd.CombinedOutput()
	res := string(b)

	if runErr != nil {
		if cctx.Err() != nil {
			return res + fmt.Sprintf("\n[timed out after %s]", timeout), true, nil
		}
		if ee, ok := errors.AsType[*exec.ExitError](runErr); ok {
			return res + fmt.Sprintf("\n[exit status %d]", ee.ExitCode()), true, nil
		}
		return "", false, fmt.Errorf("could not run %s: %w", name, runErr)
	}
	return res, false, nil
}

// clip shortens s to at most max bytes, keeping head and tail (where compiler
// errors and test summaries respectively tend to live).
func clip(s string, max int) string {
	if len(s) <= max {
		return s
	}
	const marker = "\n…[truncated]…\n"
	if max <= len(marker) {
		return s[:max]
	}
	keep := max - len(marker)
	head := keep / 2
	tail := keep - head
	return s[:head] + marker + s[len(s)-tail:]
}
