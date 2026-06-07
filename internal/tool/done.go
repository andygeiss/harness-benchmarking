package tool

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrCompleted is returned by the done tool when verification passes. The agent
// loop treats it as a control signal to end the run successfully, not a fault.
var ErrCompleted = errors.New("task completed and verified")

// Verifier runs the project's verification check. ok reports whether it passed;
// output is the command output, shown to the model on failure.
type Verifier func(ctx context.Context) (ok bool, output string, err error)

// Done returns the completion tool. When called it runs verify: on success it
// signals ErrCompleted; on failure it returns the output so the model can fix
// the issues and call done again.
func Done(verify Verifier) Tool {
	return Tool{
		Name:        "done",
		Description: "Call this only when the task is fully implemented. The harness then runs verification: if it passes the run ends, otherwise you receive the errors and must fix them and call done again.",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"summary": map[string]any{"type": "string", "description": "Brief summary of what was accomplished."},
			},
			"required": []string{"summary"},
		},
		Run: func(ctx context.Context, _ json.RawMessage) (string, error) {
			ok, output, err := verify(ctx)
			if err != nil {
				return "", fmt.Errorf("verification could not run: %w", err)
			}
			if ok {
				return "", ErrCompleted
			}
			return "Verification FAILED — fix the issues below and call done again:\n" + output, nil
		},
	}
}

// VerifierFor returns the verification gate for a parsed verify command. A
// `go test …` command gets the strict GoTestVerifier, which confirms the spec's
// tests actually ran and passed; any other command falls back to CommandVerifier,
// which can only observe the process exit status.
func VerifierFor(dir string, command []string, timeout time.Duration) Verifier {
	if len(command) >= 2 && command[0] == "go" && command[1] == "test" {
		return GoTestVerifier(dir, timeout, command[2:])
	}
	return CommandVerifier(dir, command, timeout)
}

// CommandVerifier builds a Verifier that runs command (e.g. ["make","check"]) in
// dir and trusts its exit status. It cannot tell a run that genuinely passed from
// one that exited 0 without doing anything; use GoTestVerifier for `go test`.
func CommandVerifier(dir string, command []string, timeout time.Duration) Verifier {
	return func(ctx context.Context) (bool, string, error) {
		if len(command) == 0 {
			return false, "", fmt.Errorf("empty verification command")
		}
		out, failed, err := runCmd(ctx, dir, timeout, command[0], command[1:]...)
		if err != nil {
			return false, "", err
		}
		return !failed, out, nil
	}
}

// GoTestVerifier builds a Verifier that runs `go test -json -count=3` for the
// given package patterns and passes only if the spec's tests actually RAN and
// PASSED. Exit status alone is not trusted: a test binary that exits early — an
// os.Exit or init() in a NON-test file, which -protect-tests permits because it
// guards only *_test.go — prints "ok" and exits 0 while running no tests. Demanding
// at least one test-level pass, no failures, and no test left unfinished closes
// that last path to a falsely-green gate. -count=3 defeats the test cache (a fresh
// execution is always observed, not a replay) and runs each test three times in
// one process, so an order-dependent flaky test — e.g. one sensitive to Go's
// randomized map iteration — gets three independent chances to fail the gate. That
// is a probabilistic guard against non-determinism, not a guarantee: a low-rate
// flake can still pass all three rolls (see the "Passes" invariant in CLAUDE.md).
func GoTestVerifier(dir string, timeout time.Duration, patterns []string) Verifier {
	return func(ctx context.Context) (bool, string, error) {
		args := append([]string{"test", "-json", "-count=3"}, patterns...)
		out, _, err := runCmdFull(ctx, dir, timeout, "go", args...)
		if err != nil {
			return false, "", err
		}
		return analyzeGoTest(out)
	}
}

// goTestEvent is the subset of a `go test -json` event the verifier inspects.
type goTestEvent struct {
	Action  string `json:"Action"`
	Package string `json:"Package"`
	Test    string `json:"Test"`
	Output  string `json:"Output"`
}

// goTestSummary folds a -json event stream into the facts a verdict needs.
type goTestSummary struct {
	log     strings.Builder // reconstructed console output, including compiler errors
	failed  bool            // any test-, package-, or build-level failure
	passed  int             // test-level pass events (Test != "")
	started map[string]bool // tests that emitted "run" but no terminal event yet
}

// add folds one event into the summary. A package-level pass/skip/fail carries an
// empty Test, so the delete is a harmless no-op there and only real (run) tests
// are ever cleared from started.
func (s *goTestSummary) add(e goTestEvent) {
	switch e.Action {
	case "output", "build-output":
		s.log.WriteString(e.Output)
	case "run":
		s.started[e.Package+"\x00"+e.Test] = true
	case "pass":
		if e.Test != "" {
			s.passed++
		}
		delete(s.started, e.Package+"\x00"+e.Test)
	case "skip":
		delete(s.started, e.Package+"\x00"+e.Test)
	case "fail", "build-fail":
		s.failed = true
		delete(s.started, e.Package+"\x00"+e.Test)
	}
}

// Diagnostics appended to the feedback when a run is rejected for a reason the raw
// output does not explain on its own (it just says "ok"), so the model is steered
// to fix the code rather than the test bypass.
const (
	msgNoTests  = "\n[verification executed 0 tests: the test binary reported success but no spec test ran — either the package's tests are still unimplemented or all skipped, or a non-test file (e.g. an os.Exit or init()) short-circuited the run before they could. Implement the code so the spec's own tests run and pass; do not skip or bypass them.]"
	msgDangling = "\n[a test started but never finished: the test binary exited mid-run. Remove any os.Exit or other early termination from non-test code so the suite runs to completion.]"
)

// verdict turns the folded summary into (ok, feedback). It passes only when the
// suite ran cleanly: no failure, no test left unfinished, and at least one real
// test pass — so a binary that exits 0 without executing the spec is rejected.
func (s *goTestSummary) verdict() (ok bool, feedback string) {
	out := clip(s.log.String(), maxCmdOutput)
	switch {
	case s.failed:
		return false, out
	case len(s.started) > 0:
		return false, out + msgDangling
	case s.passed == 0:
		return false, out + msgNoTests
	default:
		return true, ""
	}
}

// analyzeGoTest decides verification from a `go test -json` stream, tolerating
// non-JSON lines (stderr, the trailing status note) by skipping them.
func analyzeGoTest(out string) (bool, string, error) {
	s := goTestSummary{started: make(map[string]bool)}
	sc := bufio.NewScanner(strings.NewReader(out))
	sc.Buffer(make([]byte, 0, 64<<10), 4<<20) // tolerate long -json lines
	for sc.Scan() {
		line := sc.Bytes()
		var e goTestEvent
		if len(line) == 0 || line[0] != '{' || json.Unmarshal(line, &e) != nil {
			continue
		}
		s.add(e)
	}
	if err := sc.Err(); err != nil {
		return false, "", fmt.Errorf("read go test output: %w", err)
	}
	ok, feedback := s.verdict()
	return ok, feedback, nil
}
