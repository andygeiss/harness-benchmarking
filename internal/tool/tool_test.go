package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSafeJoinStaysInRoot(t *testing.T) {
	root := "/work"
	cases := map[string]string{
		"a.go":            "/work/a.go",
		"sub/b.go":        "/work/sub/b.go",
		".":               "/work",
		"../etc/passwd":   "/work/etc/passwd", // traversal collapsed under root
		"../../../secret": "/work/secret",
	}
	for in, want := range cases {
		got, err := safeJoin(root, in)
		if err != nil {
			t.Errorf("safeJoin(%q) error: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("safeJoin(%q) = %q, want %q", in, got, want)
		}
		if !strings.HasPrefix(got, root) {
			t.Errorf("safeJoin(%q) = %q escaped root", in, got)
		}
	}
}

func TestClip(t *testing.T) {
	if got := clip("short", 100); got != "short" {
		t.Errorf("clip short = %q", got)
	}
	got := clip(strings.Repeat("x", 1000), 100)
	if len(got) > 100 {
		t.Errorf("clip len = %d, want <= 100", len(got))
	}
	if !strings.Contains(got, "truncated") {
		t.Errorf("clip missing marker: %q", got)
	}
}

func TestEditFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("alpha beta gamma"), 0o644); err != nil {
		t.Fatal(err)
	}
	edit := EditFile(dir)

	if _, err := edit.Run(context.Background(), args(t, "f.txt", "beta", "BETA")); err != nil {
		t.Fatalf("edit success: %v", err)
	}
	if b, _ := os.ReadFile(path); string(b) != "alpha BETA gamma" {
		t.Fatalf("after edit: %q", b)
	}
	if _, err := edit.Run(context.Background(), args(t, "f.txt", "zzz", "x")); err == nil {
		t.Error("expected error for missing old_string")
	}
	if err := os.WriteFile(path, []byte("dup dup"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := edit.Run(context.Background(), args(t, "f.txt", "dup", "x")); err == nil {
		t.Error("expected error for non-unique old_string")
	}
}

func args(t *testing.T, path, oldStr, newStr string) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(map[string]string{"path": path, "old_string": oldStr, "new_string": newStr})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestRegistryOrderAndGet(t *testing.T) {
	r := NewRegistry()
	r.Register(Tool{Name: "a", Schema: map[string]any{"type": "object"}})
	r.Register(Tool{Name: "b", Schema: map[string]any{"type": "object"}})
	specs := r.Specs()
	if len(specs) != 2 || specs[0].Function.Name != "a" || specs[1].Function.Name != "b" {
		t.Fatalf("specs order wrong: %+v", specs)
	}
	if _, ok := r.Get("a"); !ok {
		t.Error("Get(a) missing")
	}
	if _, ok := r.Get("missing"); ok {
		t.Error("Get(missing) should fail")
	}
	if got := r.Names(); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("Names() = %v, want [a b]", got)
	}
}
