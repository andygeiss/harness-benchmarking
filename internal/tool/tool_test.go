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
	// Distinct head and tail so a regression that kept only one end (or dropped the
	// boundary) is caught: clip preserves the head (where compiler errors live) and
	// the tail (where the test summary lives), eliding the middle.
	got := clip("HEAD"+strings.Repeat("-", 1000)+"TAIL", 100)
	if len(got) > 100 {
		t.Errorf("clip len = %d, want <= 100", len(got))
	}
	if !strings.HasPrefix(got, "HEAD") {
		t.Errorf("clip dropped the head: %q", got)
	}
	if !strings.HasSuffix(got, "TAIL") {
		t.Errorf("clip dropped the tail: %q", got)
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

// TestGoArgAllowlist locks in that the go tool is an execution sandbox, not just a
// subcommand filter. The subcommand allowlist alone still forwards flags like
// -exec, -toolexec, -vettool, -o and -overlay that run arbitrary programs or write
// outside the workspace (and -overlay re-opens -protect-tests), so checkGoArgs must
// permit only benign flags plus package paths, and restrict `go mod` to its
// read-only verbs.
func TestGoArgAllowlist(t *testing.T) {
	allowed := [][]string{
		{"build", "./..."},
		{"test", "-run", "TestX", "-v", "./..."},
		{"test", "-count=1", "-timeout", "30s", "./..."},
		{"test", "-short", "-race", "./..."},
		{"vet", "."},
		{"fmt", "./..."},
		{"mod", "tidy"},
		{"mod", "download"},
		{"mod", "verify"},
	}
	for _, args := range allowed {
		if err := checkGoArgs(args); err != nil {
			t.Errorf("checkGoArgs(%v) = %v, want allowed", args, err)
		}
	}

	rejected := [][]string{
		{"test", "-exec", "touch /tmp/pwned", "./..."}, // run an arbitrary command
		{"test", "-exec=touch x"},                      // ...also in -flag=value form
		{"build", "-o", "../escape"},                   // write a binary outside the workspace
		{"build", "-o=../escape"},
		{"build", "-toolexec=touch x"},        // run a program per tool invocation
		{"vet", "-vettool=/bin/false"},        // run an arbitrary vet tool
		{"build", "-ldflags=-X=a.b=c"},        // linker-driven execution surface
		{"test", "-overlay=o.json"},           // redirect file contents (re-opens -protect-tests)
		{"mod", "edit", "-replace=a=../b"},    // rewrite go.mod to redirect the build
		{"mod", "tidy", "-modfile=../escape"}, // redirect the write to a go.mod outside root
		{"mod"},                               // missing verb
	}
	for _, args := range rejected {
		if err := checkGoArgs(args); err == nil {
			t.Errorf("checkGoArgs(%v) = nil, want rejected", args)
		}
	}
}

// TestGoToolRejectsUnsafeInvocations checks the same boundary end-to-end through
// the tool's Run: an empty arg list, a non-allowlisted subcommand, and a smuggled
// -exec flag are all refused before any go process starts.
func TestGoToolRejectsUnsafeInvocations(t *testing.T) {
	g := Go(t.TempDir(), time.Second)
	for _, args := range [][]string{
		{},                      // empty
		{"run", "."},            // non-allowlisted subcommand
		{"test", "-exec", "sh"}, // allowlisted subcommand, unsafe flag
	} {
		raw, err := json.Marshal(map[string]any{"args": args})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := g.Run(context.Background(), raw); err == nil {
			t.Errorf("Go.Run(%v) = nil error, want rejection", args)
		}
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

	// The normalization-mismatch bypass: a path like "x_test.go/." has base "."
	// (slips past isTestFile) but resolves back to x_test.go (the real spec). Both
	// tools must refuse it on the resolved path, and the spec must be untouched.
	if _, err := w.Run(context.Background(), writeArgs(t, "x_test.go/.", "package x // clobbered")); err == nil {
		t.Error("write_file allowed the x_test.go/. bypass to overwrite a protected spec")
	}
	if _, err := EditFile(dir, true).Run(context.Background(), args(t, "x_test.go/.", "orig", "hacked")); err == nil {
		t.Error("edit_file allowed the x_test.go/. bypass to edit a protected spec")
	}

	// The case-variant bypass: on a case-insensitive FS (the macOS/APFS target)
	// "x_Test.go" names the same on-disk file as x_test.go, so the suffix check must
	// fold case. Refused on every platform — a file literally named x_Test.go is not
	// a Go test anyway, so over-refusing on a case-sensitive FS costs nothing.
	if _, err := w.Run(context.Background(), writeArgs(t, "x_Test.go", "package x // clobbered")); err == nil {
		t.Error("write_file allowed the x_Test.go case-variant bypass")
	}
	if _, err := EditFile(dir, true).Run(context.Background(), args(t, "x_Test.go", "orig", "hacked")); err == nil {
		t.Error("edit_file allowed the x_Test.go case-variant bypass")
	}

	if b, err := os.ReadFile(filepath.Join(dir, "x_test.go")); err != nil || string(b) != "package x // orig" {
		t.Errorf("protected spec was modified through a bypass: %q (%v)", b, err)
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

// TestSandboxedGoEnv locks in the env-scrub that keeps checkGoArgs authoritative:
// the Go control vars that inject flags out of band (GOFLAGS/GOENV/GOTOOLCHAIN) must
// be dropped and pinned inert, while unrelated vars pass through untouched. This is
// the only thing closing the contaminated-environment -exec channel, so the prose
// guarantee needs a behavioral guard against a silent regression.
func TestSandboxedGoEnv(t *testing.T) {
	t.Setenv("GOFLAGS", "-exec=/bin/sh")
	t.Setenv("GOENV", "/tmp/evil.env")
	t.Setenv("GOTOOLCHAIN", "go1.0rc1")
	t.Setenv("CGO_ENABLED", "1")
	t.Setenv("HARNESS_KEEP", "1")

	got := map[string][]string{}
	for _, kv := range sandboxedGoEnv() {
		k, v, _ := strings.Cut(kv, "=")
		got[k] = append(got[k], v)
	}

	if vs := got["GOFLAGS"]; len(vs) != 1 || vs[0] != "" {
		t.Errorf("GOFLAGS = %q, want exactly one empty value (injected -exec must be dropped and pinned)", vs)
	}
	if vs := got["GOTOOLCHAIN"]; len(vs) != 1 || vs[0] != "local" {
		t.Errorf("GOTOOLCHAIN = %q, want exactly [local] (toolchain switch must be forbidden)", vs)
	}
	if vs := got["CGO_ENABLED"]; len(vs) != 1 || vs[0] != "0" {
		t.Errorf("CGO_ENABLED = %q, want exactly [0] (cgo C-compiler channel must be off)", vs)
	}
	if vs, ok := got["GOENV"]; ok {
		t.Errorf("GOENV survived the scrub: %q", vs)
	}
	if got["HARNESS_KEEP"] == nil {
		t.Error("scrub dropped an unrelated environment variable")
	}
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

// TestPassingPackages locks the per-package verdict behind -elide-passing: it
// mirrors the gate's rule (>=1 test pass, no failure, none dangling) at package
// granularity, so only a genuinely green package is reported. The streams reuse the
// gate's own cases — a clean pass, a test failure, a build error, an exit-0/no-test
// package, and a dangling test — folded together in one run; only "pass" survives.
func TestPassingPackages(t *testing.T) {
	combined := strings.Join([]string{jsonPass, jsonFail, jsonBuildErr, jsonExitZeroNoTests, jsonDangling}, "\n")
	s, err := foldGoTest(combined)
	if err != nil {
		t.Fatalf("foldGoTest: %v", err)
	}
	got := s.passingPackages()
	if len(got) != 1 || !got["pass"] {
		t.Fatalf("passingPackages = %v, want only {pass}", got)
	}
	for _, notPassing := range []string{"fail", "builderr", "initexit", "d"} {
		if got[notPassing] {
			t.Errorf("%q must not be reported passing", notPassing)
		}
	}
}

// TestImportToDir maps import paths to workspace-relative dirs against the module
// root — the conversion -elide-passing uses to key reads by directory.
func TestImportToDir(t *testing.T) {
	cases := []struct {
		module, pkg, wantDir string
		wantOK               bool
	}{
		{"apikit", "apikit", ".", true},
		{"apikit", "apikit/users", "users", true},
		{"apikit", "apikit/api", "api", true},
		{"apikit", "other/pkg", "", false},
	}
	for _, c := range cases {
		dir, ok := importToDir(c.module, c.pkg)
		if ok != c.wantOK || dir != c.wantDir {
			t.Errorf("importToDir(%q,%q) = (%q,%v), want (%q,%v)", c.module, c.pkg, dir, ok, c.wantDir, c.wantOK)
		}
	}
}

// elideFeedModule writes a one-package module (a passing lib package) under a fresh
// temp dir for the spec-elision feed tests, returning the dir and a write helper to
// mutate it. Shared so each feed test stays small.
func elideFeedModule(t *testing.T) (dir string, write func(name, content string)) {
	t.Helper()
	dir = t.TempDir()
	write = func(name, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(dir, filepath.Dir(name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module ef\n\ngo 1.26\n")
	write("lib/lib_test.go", "package lib\n\nimport \"testing\"\n\nfunc TestOK(t *testing.T) {}\n")
	return dir, write
}

// TestVerifierFeedsPassingDirs proves spec elision is fed from the gate's own run
// (GoTestVerifier's onPass), at directory granularity, and only for a `go test`
// command — a non-test verify command routes to the exit-status verifier and never
// feeds the set, so elision is a no-op there. This is the guarantee that lets the
// completion run double as the per-package status probe (no separate test run).
func TestVerifierFeedsPassingDirs(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles and runs go test")
	}
	dir, _ := elideFeedModule(t)

	var got map[string]bool
	called := false
	sink := func(dirs map[string]bool) { called, got = true, dirs }

	// `go test` feeds the set: lib passes, mapped to its directory relative to root.
	if ok, out, err := VerifierFor(dir, []string{"go", "test", "./..."}, time.Minute, sink)(context.Background()); err != nil {
		t.Fatalf("go test verify: %v\n%s", err, out)
	} else if !ok {
		t.Fatalf("the passing spec should verify; got ok=false:\n%s", out)
	}
	if !called || len(got) != 1 || !got["lib"] {
		t.Errorf("onPass should report {lib:true}; called=%v got=%v", called, got)
	}

	// A non-go-test command routes to the exit-status verifier and never feeds the set.
	called = false
	if _, _, err := VerifierFor(dir, []string{"go", "vet", "./..."}, time.Minute, sink)(context.Background()); err != nil {
		t.Fatalf("go vet verify: %v", err)
	}
	if called {
		t.Error("a non go-test verify command must not feed the elide set")
	}
}

// TestVerifierFeedDropsRegressionAndSkipsStartError exercises the feed's gate-safe
// edges: a regressed (now-failing) package drops out of the set on the next run, and a
// run that cannot start at all skips the feed entirely so the prior set carries forward
// (never cleared). With the disk-untouched gate, this is why a stale entry can never
// cause a false completion.
func TestVerifierFeedDropsRegressionAndSkipsStartError(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles and runs go test")
	}
	dir, write := elideFeedModule(t)

	var got map[string]bool
	called := false
	sink := func(dirs map[string]bool) { called, got = true, dirs }

	// A regression still feeds the set, with the now-failing package dropped out — so a
	// package that breaks self-corrects rather than staying wrongly elided.
	write("lib/lib_test.go", "package lib\n\nimport \"testing\"\n\nfunc TestOK(t *testing.T) { t.Fatal(\"boom\") }\n")
	if ok, _, err := VerifierFor(dir, []string{"go", "test", "./..."}, time.Minute, sink)(context.Background()); err != nil {
		t.Fatalf("go test verify (regressed): %v", err)
	} else if ok {
		t.Fatal("a failing spec must not verify")
	}
	if !called || got["lib"] {
		t.Errorf("a regressed package must drop out of the fed set; called=%v got=%v", called, got)
	}

	// A run that cannot start at all (missing workspace) skips onPass entirely, so the
	// prior set carries forward — it is never cleared on a start failure.
	called = false
	missing := filepath.Join(dir, "does-not-exist")
	if _, _, err := VerifierFor(missing, []string{"go", "test", "./..."}, time.Minute, sink)(context.Background()); err == nil {
		t.Error("a missing workspace should surface a verifier start error")
	}
	if called {
		t.Error("onPass must not fire when go test cannot start; the prior set carries forward")
	}
}

// TestReadFileElidesPassingSpec is the mechanical core of spec elision: once a
// package is marked passing, read_file returns a short notice for its *_test.go (the
// spec bytes never re-enter context) and counts the stub, while a non-test file, a
// failing package's spec, and a nil ElideState are all read in full.
func TestReadFileElidesPassingSpec(t *testing.T) {
	dir := t.TempDir()
	mustWrite := func(rel, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(dir, filepath.Dir(rel)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, rel), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite("users/users_test.go", "package users // SPEC BODY")
	mustWrite("users/users.go", "package users // IMPL BODY")
	mustWrite("notes/notes_test.go", "package notes // SPEC BODY")

	read := func(elide *ElideState, path string) string {
		t.Helper()
		raw, err := json.Marshal(map[string]string{"path": path})
		if err != nil {
			t.Fatal(err)
		}
		out, err := ReadFile(dir, elide).Run(context.Background(), raw)
		if err != nil {
			t.Fatalf("read %q: %v", path, err)
		}
		return out
	}

	// The verifier reports only users green; ReadFile elides that package's spec.
	elide := NewElideState()
	elide.Update(map[string]bool{"users": true})

	if out := read(elide, "users/users_test.go"); strings.Contains(out, "SPEC BODY") || !strings.Contains(out, "elided") {
		t.Errorf("passing package's spec should be elided, got: %q", out)
	}
	if out := read(elide, "users/users.go"); !strings.Contains(out, "IMPL BODY") {
		t.Errorf("a non-test file must never be elided, got: %q", out)
	}
	if out := read(elide, "notes/notes_test.go"); !strings.Contains(out, "SPEC BODY") {
		t.Errorf("a failing package's spec must be read in full, got: %q", out)
	}
	if elide.Elided() != 1 {
		t.Errorf("Elided() = %d, want 1 (only the users spec)", elide.Elided())
	}

	// A nil state disables elision: the spec is read in full, exactly like baseline.
	if out := read(nil, "users/users_test.go"); !strings.Contains(out, "SPEC BODY") {
		t.Errorf("nil ElideState must read the spec in full, got: %q", out)
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

	verify := GoTestVerifier(dir, time.Minute, []string{"./..."}, nil)

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

// TestGoTestVerifierRunsRepeatedly proves the gate runs the suite more than once,
// so an order-dependent flaky test gets more than a single roll to fail. The spec
// here passes on its first invocation and fails on its second; a package-level
// counter persists across the repeated runs within the one test process, so
// -count=1 would accept it while -count>=2 must reject it.
func TestGoTestVerifierRunsRepeatedly(t *testing.T) {
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
	write("go.mod", "module flaky\n\ngo 1.26\n")
	// calls persists across -count repeats in the same process: pass on run 1,
	// fail on run 2. A single execution would never observe the failure.
	write("p_test.go", "package p\n\nimport \"testing\"\n\nvar calls int\n\nfunc TestFlaky(t *testing.T) {\n\tcalls++\n\tif calls >= 2 {\n\t\tt.Fatalf(\"deterministic fail on run %d\", calls)\n\t}\n}\n")

	ok, out, err := GoTestVerifier(dir, time.Minute, []string{"./..."}, nil)(context.Background())
	if err != nil {
		t.Fatalf("verify: %v\n%s", err, out)
	}
	if ok {
		t.Fatalf("gate accepted a test that fails on its second run; the suite was not re-run (count<2):\n%s", out)
	}
}

// TestVerifierForRouting pins the dispatch that decides whether the anti-gaming
// guarantee even applies: a `go test` command must get the strict GoTestVerifier
// (which rejects a build that runs zero tests), while any other command falls back
// to the exit-status CommandVerifier (which trusts the process exit code).
func TestVerifierForRouting(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go toolchain")
	}
	dir := t.TempDir()
	write := func(d, name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(d, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(dir, "go.mod", "module route\n\ngo 1.26\n")
	write(dir, "p_test.go", "package p\n\nimport \"testing\"\n\nfunc TestSpec(t *testing.T) {}\n")
	// A non-test file that exits before any test runs: `go test` prints ok/exit 0
	// while running zero tests, but `go vet` neither runs it nor objects.
	write(dir, "exit.go", "package p\n\nimport \"os\"\n\nfunc init() { os.Exit(0) }\n")

	ctx := context.Background()

	// `go test` routes to the strict verifier: zero tests actually ran, so reject.
	if ok, _, err := VerifierFor(dir, []string{"go", "test", "./..."}, time.Minute, nil)(ctx); err != nil {
		t.Fatalf("go test verify: %v", err)
	} else if ok {
		t.Error("go test route should use the strict gate and reject a zero-test build")
	}

	// `go vet` routes to the exit-status fallback: the tree vets clean, exit 0 => ok.
	if ok, _, err := VerifierFor(dir, []string{"go", "vet", "./..."}, time.Minute, nil)(ctx); err != nil {
		t.Fatalf("go vet verify: %v", err)
	} else if !ok {
		t.Error("non-test route should trust a clean exit (ok=true)")
	}

	// The fallback reports failure when the command exits non-zero. A separate
	// module with a Printf verb/arg mismatch makes `go vet` find an issue and exit 1.
	bad := t.TempDir()
	write(bad, "go.mod", "module bad\n\ngo 1.26\n")
	write(bad, "bad.go", "package p\n\nimport \"fmt\"\n\nfunc Bad() { fmt.Printf(\"%d\", \"x\") }\n")
	if ok, _, err := VerifierFor(bad, []string{"go", "vet", "./..."}, time.Minute, nil)(ctx); err != nil {
		t.Fatalf("go vet (bad) verify: %v", err)
	} else if ok {
		t.Error("fallback must report ok=false when go vet finds an issue (exit 1)")
	}
}
