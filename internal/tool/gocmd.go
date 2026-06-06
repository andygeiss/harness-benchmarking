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

// goFlags is the allowlist of flags accepted after a build/test/vet/fmt
// subcommand. The subcommand allowlist alone is NOT a sandbox: go forwards flags
// like -exec, -toolexec, -vettool, -o and -overlay that run arbitrary programs or
// write outside the workspace, so an allowlisted `go test` can still escape both
// the "Go-toolchain-only execution" and "filesystem is sandboxed" invariants — and
// -overlay even re-opens -protect-tests by redirecting a *_test.go on disk. Only
// these benign test-selection / diagnostic flags, which neither execute a program
// nor write or redirect files, are permitted; every other flag is refused.
// Positional arguments (package paths like ./... and flag values) carry no such
// risk and are always allowed.
var goFlags = map[string]bool{
	"-v":       true,
	"-run":     true,
	"-count":   true,
	"-timeout": true,
	"-short":   true,
	"-race":    true,
	"-cover":   true,
}

// goModVerbs is the allowlist of `go mod` subverbs. tidy/download/verify only read
// or normalise dependencies; edit (and the rest) can rewrite go.mod to redirect the
// build, so they are refused.
var goModVerbs = map[string]bool{
	"tidy":     true,
	"download": true,
	"verify":   true,
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
			if err := checkGoArgs(a.Args); err != nil {
				return "", err
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

// checkGoArgs enforces the per-subcommand argument allowlist that turns the
// subcommand check into an actual execution sandbox (see goFlags). For `mod` only
// the read-only verbs in goModVerbs are accepted; for build/test/vet/fmt every
// flag (an argument starting with "-") must be in goFlags, while positional
// arguments — package paths and flag values — are always allowed. args[0] is the
// already-validated subcommand.
func checkGoArgs(args []string) error {
	if args[0] == "mod" {
		if len(args) < 2 || !goModVerbs[args[1]] {
			return fmt.Errorf("go mod subcommand not allowed; allowed: tidy, download, verify")
		}
		return nil
	}
	for _, arg := range args[1:] {
		if !strings.HasPrefix(arg, "-") {
			continue // package path or a preceding flag's value: no execution risk
		}
		name, _, _ := strings.Cut(strings.TrimLeft(arg, "-"), "=")
		if !goFlags["-"+name] {
			return fmt.Errorf("flag %q not allowed; only benign flags (-v -run -count -timeout -short -race -cover) and package paths are permitted", arg)
		}
	}
	return nil
}
