package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
	edit := EditFile(dir, false)

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

// TestProtectTests locks in the governance boundary that closes the false-completed
// hole: with protection on, the agent cannot create or edit *_test.go files (the
// spec), so it cannot pass verification by shimming the runner or gutting a test.
func TestProtectTests(t *testing.T) {
	dir := t.TempDir()

	// The exact cheat: a new TestMain shim in a *_test.go must be refused.
	w := WriteFile(dir, true)
	shim := "package check\nimport (\"os\";\"testing\")\nfunc TestMain(m *testing.M){os.Exit(0)}\n"
	if _, err := w.Run(context.Background(), writeArgs(t, "check/skip_test.go", shim)); err == nil {
		t.Error("write_file allowed a new *_test.go with protection on")
	}
	// A normal implementation file is still allowed.
	if _, err := w.Run(context.Background(), writeArgs(t, "check/impl.go", "package check\n")); err != nil {
		t.Errorf("write_file blocked a non-test file: %v", err)
	}

	// Editing an existing test file is refused too.
	if err := os.WriteFile(filepath.Join(dir, "x_test.go"), []byte("package x // orig"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := EditFile(dir, true).Run(context.Background(), args(t, "x_test.go", "orig", "hacked")); err == nil {
		t.Error("edit_file allowed editing a *_test.go with protection on")
	}

	// With protection off, writing a test file is permitted (opt-out path).
	if _, err := WriteFile(dir, false).Run(context.Background(), writeArgs(t, "y_test.go", "package y\n")); err != nil {
		t.Errorf("protection off should allow test writes: %v", err)
	}
}

func writeArgs(t *testing.T, path, content string) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(map[string]string{"path": path, "content": content})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// Real `go test -json` streams captured from go 1.26, one per case. The attack
// stream is the signature of init(){os.Exit(0)} in a non-test file: start, a
// package-level "ok", a package-level pass — and not a single test-level event.
const (
	jsonPass = `{"Action":"run","Package":"pass","Test":"TestOK"}
{"Action":"output","Package":"pass","Test":"TestOK","Output":"=== RUN   TestOK\n"}
{"Action":"pass","Package":"pass","Test":"TestOK","Elapsed":0}
{"Action":"output","Package":"pass","Output":"PASS\n"}
{"Action":"pass","Package":"pass","Elapsed":0.365}`

	jsonFail = `{"Action":"run","Package":"fail","Test":"TestBad"}
{"Action":"output","Package":"fail","Test":"TestBad","Output":"    x_test.go:3: boom\n"}
{"Action":"fail","Package":"fail","Test":"TestBad","Elapsed":0}
{"Action":"fail","Package":"fail","Elapsed":0.307}`

	jsonBuildErr = `{"ImportPath":"builderr [builderr.test]","Action":"build-output","Output":"./x_test.go:3:27: undefined: nope\n"}
{"ImportPath":"builderr [builderr.test]","Action":"build-fail"}
{"Action":"fail","Package":"builderr","Elapsed":0,"FailedBuild":"builderr [builderr.test]"}`

	jsonExitZeroNoTests = `{"Action":"start","Package":"initexit"}
{"Action":"output","Package":"initexit","Output":"ok  \tinitexit\t0.293s\n"}
{"Action":"pass","Package":"initexit","Elapsed":0.293}`

	jsonDangling = `{"Action":"run","Package":"d","Test":"TestX"}
{"Action":"output","Package":"d","Test":"TestX","Output":"=== RUN   TestX\n"}`
)

// TestAnalyzeGoTest locks in the gate's core rule: a run verifies only if the
// spec's tests actually ran and passed. Exit status is never trusted on its own.
func TestAnalyzeGoTest(t *testing.T) {
	cases := []struct {
		name     string
		stream   string
		wantOK   bool
		wantText string // substring the rejection feedback must contain
	}{
		{"honest pass", jsonPass, true, ""},
		{"test failure surfaces the message", jsonFail, false, "boom"},
		{"build error surfaces the compiler output", jsonBuildErr, false, "undefined: nope"},
		{"exit 0 with zero tests is rejected (the attack)", jsonExitZeroNoTests, false, "0 tests"},
		{"a test that started but never finished is rejected", jsonDangling, false, "never finished"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ok, feedback, err := analyzeGoTest(c.stream)
			if err != nil {
				t.Fatalf("analyzeGoTest: %v", err)
			}
			if ok != c.wantOK {
				t.Fatalf("ok = %v, want %v (feedback: %s)", ok, c.wantOK, feedback)
			}
			if !ok && !strings.Contains(feedback, c.wantText) {
				t.Errorf("feedback missing %q:\n%s", c.wantText, feedback)
			}
		})
	}
}

// TestGoTestVerifierClosesExitHole is the end-to-end proof against the real
// toolchain: a non-test file (allowed under -protect-tests) that exits the process
// before tests run makes `go test` print "ok"/exit 0, yet the gate must reject it.
func TestGoTestVerifierClosesExitHole(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles and runs go test")
	}
	dir := t.TempDir()
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module probe\n\ngo 1.26\n")
	write("p_test.go", "package p\n\nimport \"testing\"\n\nfunc TestSpec(t *testing.T) {}\n")

	verify := GoTestVerifier(dir, time.Minute, []string{"./..."})

	// An honest passing spec must verify.
	ok, out, err := verify(context.Background())
	if err != nil {
		t.Fatalf("verify (honest pass): %v\n%s", err, out)
	}
	if !ok {
		t.Fatalf("honest passing spec should verify; got ok=false:\n%s", out)
	}

	// The attack: init() exits 0 before any test runs. go test reports "ok".
	write("evil.go", "package p\n\nimport \"os\"\n\nfunc init() { os.Exit(0) }\n")
	ok, out, err = verify(context.Background())
	if err != nil {
		t.Fatalf("verify (attack): %v", err)
	}
	if ok {
		t.Fatalf("gate accepted a build that runs zero tests — the exit-0 hole is still open:\n%s", out)
	}
	if !strings.Contains(out, "0 tests") {
		t.Errorf("expected a 0-tests diagnostic in the feedback, got:\n%s", out)
	}
}
