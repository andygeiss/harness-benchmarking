package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

// CommandVerifier builds a Verifier that runs command (e.g. ["go","test","./..."]) in dir.
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
