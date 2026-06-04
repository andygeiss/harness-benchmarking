// Command example sets up and runs a bundled harness example: it copies the
// example's seed workspace to a fresh ./sandbox, then launches the harness
// against it. Run from the repository root:
//
//	go run ./cmd/example <name> [harness flags...]
//	go run ./cmd/example reverse -stream
package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

const sandbox = "sandbox"

func main() {
	if len(os.Args) < 2 || os.Args[1] == "-h" || os.Args[1] == "--help" {
		fmt.Fprintln(os.Stderr, "usage: go run ./cmd/example <name> [harness flags...]")
		listExamples()
		os.Exit(2)
	}
	name := os.Args[1]
	prompt := filepath.Join("examples", name, "PROMPT.md")
	if _, err := os.Stat(prompt); err != nil {
		fmt.Fprintf(os.Stderr, "no such example %q (looked for %s)\n", name, prompt)
		listExamples()
		os.Exit(1)
	}
	if err := reseed(filepath.Join("examples", name, "workspace"), sandbox); err != nil {
		fmt.Fprintf(os.Stderr, "prepare sandbox: %v\n", err)
		os.Exit(1)
	}

	args := append([]string{"run", "./cmd/harness", "-prompt", prompt, "-workdir", sandbox}, os.Args[2:]...)
	harness := exec.Command("go", args...)
	harness.Stdin, harness.Stdout, harness.Stderr = os.Stdin, os.Stdout, os.Stderr
	fmt.Fprintf(os.Stderr, "example %q: seeded ./%s, launching harness\n", name, sandbox)
	if err := harness.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			os.Exit(ee.ExitCode())
		}
		fmt.Fprintf(os.Stderr, "launch harness: %v\n", err)
		os.Exit(1)
	}
}

// reseed replaces dst with a fresh copy of the example's seed workspace, so each
// run starts from the same pristine state. A missing seed yields an empty dst
// (for from-scratch tasks).
func reseed(seed, dst string) error {
	if err := os.RemoveAll(dst); err != nil {
		return err
	}
	if info, err := os.Stat(seed); err != nil || !info.IsDir() {
		return os.MkdirAll(dst, 0o755)
	}
	return os.CopyFS(dst, os.DirFS(seed))
}

// listExamples prints the available example names: directories under examples/
// that contain a PROMPT.md.
func listExamples() {
	entries, err := os.ReadDir("examples")
	if err != nil {
		return
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join("examples", e.Name(), "PROMPT.md")); err == nil {
			names = append(names, e.Name())
		}
	}
	if len(names) > 0 {
		fmt.Fprintf(os.Stderr, "available: %v\n", names)
	}
}
