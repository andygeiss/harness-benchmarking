package tool

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"
)

const maxCmdOutput = 16 << 10 // bytes of command output fed back to the model

// runCmd runs name+args in dir under a timeout. It distinguishes a command that
// ran and failed (failed=true, output captured, err=nil) from one that could
// not start at all (err!=nil) — only the latter is a harness fault.
func runCmd(ctx context.Context, dir string, timeout time.Duration, name string, args ...string) (out string, failed bool, err error) {
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(cctx, name, args...)
	cmd.Dir = dir
	b, runErr := cmd.CombinedOutput()
	res := clip(string(b), maxCmdOutput)

	if runErr != nil {
		if cctx.Err() != nil {
			return res + fmt.Sprintf("\n[timed out after %s]", timeout), true, nil
		}
		var ee *exec.ExitError
		if errors.As(runErr, &ee) {
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
