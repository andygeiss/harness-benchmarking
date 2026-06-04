package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestReseed checks the destructive copy the runner relies on: dst is wiped and
// replaced by the seed, and a missing seed yields an empty sandbox.
func TestReseed(t *testing.T) {
	root := t.TempDir()
	seed := filepath.Join(root, "seed")
	mustWrite(t, filepath.Join(seed, "go.mod"), "module x\n")
	mustWrite(t, filepath.Join(seed, "pkg", "x.go"), "package pkg\n")

	dst := filepath.Join(root, "sandbox")
	mustWrite(t, filepath.Join(dst, "stale.go"), "package stale\n") // must not survive

	if err := reseed(seed, dst); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dst, "stale.go")); !os.IsNotExist(err) {
		t.Errorf("stale file survived reseed (err=%v)", err)
	}
	if b, err := os.ReadFile(filepath.Join(dst, "pkg", "x.go")); err != nil || string(b) != "package pkg\n" {
		t.Errorf("seed file not copied: got %q err=%v", b, err)
	}

	// A missing seed yields an empty sandbox.
	if err := reseed(filepath.Join(root, "absent"), dst); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dst)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("expected empty sandbox, got %d entries", len(entries))
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
