package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// goSubcommands is the allowlist for the go tool. Everything else is rejected,
// keeping execution to the Go toolchain only (no arbitrary shell).
var goSubcommands = map[string]bool{
	"build": true,
	"test":  true,
	"vet":   true,
	"fmt":   true,
	"mod":   true,
}

// Go returns a tool that runs allowlisted go subcommands in root.
func Go(root string, timeout time.Duration) Tool {
	return Tool{
		Name:        "go",
		Description: `Run the Go toolchain in the workspace. Pass arguments as a list beginning with the subcommand, e.g. ["build","./..."], ["test","./..."], ["vet","./..."], ["fmt","./..."], ["mod","tidy"]. Allowed subcommands: build, test, vet, fmt, mod.`,
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"args": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "Arguments passed to `go`, beginning with the subcommand.",
				},
			},
			"required": []string{"args"},
		},
		Run: func(ctx context.Context, raw json.RawMessage) (string, error) {
			var a struct {
				Args []string `json:"args"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			if len(a.Args) == 0 {
				return "", fmt.Errorf("args must not be empty")
			}
			if !goSubcommands[a.Args[0]] {
				return "", fmt.Errorf("subcommand %q not allowed; allowed: build, test, vet, fmt, mod", a.Args[0])
			}
			out, _, err := runCmd(ctx, root, timeout, "go", a.Args...)
			if err != nil {
				return "", err
			}
			if strings.TrimSpace(out) == "" {
				return "(go " + strings.Join(a.Args, " ") + " succeeded, no output)", nil
			}
			return out, nil
		},
	}
}
