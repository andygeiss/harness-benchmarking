package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestFingerprint checks the property the stagnation guard relies on: the digest
// is stable when nothing changes and moves on any content/structure change, and
// undoing a change restores the prior digest (content-addressed, path-aware).
func TestFingerprint(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	fp := func() string {
		t.Helper()
		h, err := fingerprint(dir)
		if err != nil {
			t.Fatal(err)
		}
		return h
	}

	write("main.go", "package main")
	write("sub/util.go", "package sub")
	base := fp()

	if base != fp() {
		t.Fatal("fingerprint not stable with no change")
	}

	write("sub/util.go", "package sub // edited")
	edited := fp()
	if edited == base {
		t.Fatal("fingerprint unchanged after editing a file")
	}

	write("extra.go", "package main")
	added := fp()
	if added == edited {
		t.Fatal("fingerprint unchanged after adding a file")
	}

	if err := os.Remove(filepath.Join(dir, "extra.go")); err != nil {
		t.Fatal(err)
	}
	if fp() != edited {
		t.Fatal("removing the added file should restore the prior fingerprint")
	}
}
