package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const maxFileBytes = 256 << 10 // cap on read_file output

// ElideState is the per-run state for the -elide-passing read optimization. It
// holds a status probe reporting which package directories currently pass, the most
// recent such set, and a count of the reads it has stubbed. ReadFile consults it to
// return a short notice instead of an already-green package's *_test.go spec, so a
// fresh Ralph pass does not re-spend its context budget re-reading specs the
// verifier has already certified — the dominant cost behind the re-orientation
// floor (see docs/stagnation.md). Disk is never touched; only the string returned
// into the model's context shrinks, so the go tool and the done gate still compile
// and run the real files. The set is refreshed between passes and only read during
// one — single-goroutine throughout — so it needs no lock. A nil *ElideState
// disables elision (the default), making ReadFile behaviour identical to baseline.
type ElideState struct {
	status  func(context.Context) (map[string]bool, error)
	passing map[string]bool
	elided  int
}

// NewElideState builds the elide state from a per-pass status probe (see
// tool.StatusFor). A nil probe makes Refresh a no-op, so nothing is ever elided.
func NewElideState(status func(context.Context) (map[string]bool, error)) *ElideState {
	return &ElideState{status: status}
}

// Refresh recomputes the passing-package set for the upcoming pass. On a probe
// error it clears the set (eliding nothing that pass) and returns the error. A nil
// receiver or nil probe is a no-op.
func (e *ElideState) Refresh(ctx context.Context) error {
	if e == nil || e.status == nil {
		return nil
	}
	dirs, err := e.status(ctx)
	if err != nil {
		e.passing = nil
		return err
	}
	e.passing = dirs
	return nil
}

// Elided reports how many reads have been stubbed over the run, for the run log.
func (e *ElideState) Elided() int {
	if e == nil {
		return 0
	}
	return e.elided
}

// shouldElide reports whether a read of the test file at relPath should be stubbed
// — relPath names a *_test.go whose package the last Refresh found passing — and
// counts the stub. A nil receiver never elides.
func (e *ElideState) shouldElide(relPath string) bool {
	if e == nil || !isTestFile(relPath) {
		return false
	}
	if e.passing[filepath.Dir(relPath)] {
		e.elided++
		return true
	}
	return false
}

// elidedSpecNotice is returned by read_file in place of a *_test.go spec whose
// package already passes, under -elide-passing: short and factual, so the model
// still gets a coherent reply while the spec's bytes do not re-enter the window.
func elidedSpecNotice(path string) string {
	return "[" + path + ": this package's tests already pass — spec elided to save context; no changes are needed here.]"
}

// ReadFile returns a tool that reads a text file within root. When elide is
// non-nil and reports the file's package already passing, a *_test.go read returns
// a short notice instead of the file's bytes (see ElideState); elide may be nil.
func ReadFile(root string, elide *ElideState) Tool {
	return Tool{
		Name:        "read_file",
		Description: "Read a UTF-8 text file and return its contents. Paths are relative to the workspace root.",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{"type": "string", "description": "File path relative to the workspace root."},
			},
			"required": []string{"path"},
		},
		Run: func(_ context.Context, raw json.RawMessage) (string, error) {
			var a struct {
				Path string `json:"path"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			abs, err := safeJoin(root, a.Path)
			if err != nil {
				return "", err
			}
			if elide.shouldElide(a.Path) {
				return elidedSpecNotice(a.Path), nil
			}
			b, err := os.ReadFile(abs)
			if err != nil {
				return "", err
			}
			return clip(string(b), maxFileBytes), nil
		},
	}
}

// WriteFile returns a tool that creates or replaces a file within root. When
// protectTests is set it refuses to write Go test files: they are the fixed
// specification the agent must satisfy, not author — otherwise a model can pass
// verification by gutting the tests or shimming the runner (e.g. a TestMain that
// exits 0) instead of implementing the code.
func WriteFile(root string, protectTests bool) Tool {
	desc := "Create or overwrite a file. Writes the ENTIRE file, so provide the complete intended contents. Paths are relative to the workspace root."
	if protectTests {
		desc += " You cannot write Go test files (*_test.go) — they are the fixed specification."
	}
	return Tool{
		Name:        "write_file",
		Description: desc,
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":    map[string]any{"type": "string", "description": "File path relative to the workspace root."},
				"content": map[string]any{"type": "string", "description": "Full contents to write."},
			},
			"required": []string{"path", "content"},
		},
		Run: func(_ context.Context, raw json.RawMessage) (string, error) {
			var a struct {
				Path    string `json:"path"`
				Content string `json:"content"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			abs, err := safeJoin(root, a.Path)
			if err != nil {
				return "", err
			}
			if protectTests && isTestFile(abs) {
				return "", errProtectedTest(abs)
			}
			if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
				return "", err
			}
			if err := os.WriteFile(abs, []byte(a.Content), 0o644); err != nil {
				return "", err
			}
			return fmt.Sprintf("wrote %d bytes to %s", len(a.Content), a.Path), nil
		},
	}
}

// EditFile returns a tool that replaces an exact, unique snippet within a file.
// Like WriteFile it refuses Go test files when protectTests is set.
func EditFile(root string, protectTests bool) Tool {
	desc := "Replace an exact, unique snippet of text in a file. old_string must occur EXACTLY once — include enough surrounding context to make it unique. Prefer this over write_file for small changes. Paths are relative to the workspace root."
	if protectTests {
		desc += " You cannot edit Go test files (*_test.go) — they are the fixed specification."
	}
	return Tool{
		Name:        "edit_file",
		Description: desc,
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":       map[string]any{"type": "string", "description": "File path relative to the workspace root."},
				"old_string": map[string]any{"type": "string", "description": "Exact text to replace; must be unique in the file."},
				"new_string": map[string]any{"type": "string", "description": "Replacement text."},
			},
			"required": []string{"path", "old_string", "new_string"},
		},
		Run: func(_ context.Context, raw json.RawMessage) (string, error) {
			var a struct {
				Path      string `json:"path"`
				OldString string `json:"old_string"`
				NewString string `json:"new_string"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			if a.OldString == "" {
				return "", fmt.Errorf("old_string must not be empty; use write_file to create a file")
			}
			if a.OldString == a.NewString {
				return "", fmt.Errorf("old_string and new_string are identical")
			}
			abs, err := safeJoin(root, a.Path)
			if err != nil {
				return "", err
			}
			if protectTests && isTestFile(abs) {
				return "", errProtectedTest(abs)
			}
			info, err := os.Stat(abs)
			if err != nil {
				return "", err
			}
			b, err := os.ReadFile(abs)
			if err != nil {
				return "", err
			}
			content := string(b)
			switch strings.Count(content, a.OldString) {
			case 0:
				return "", fmt.Errorf("old_string not found in %s", a.Path)
			case 1:
				// unique — proceed
			default:
				return "", fmt.Errorf("old_string is not unique in %s; add more surrounding context", a.Path)
			}
			updated := strings.Replace(content, a.OldString, a.NewString, 1)
			if err := os.WriteFile(abs, []byte(updated), info.Mode().Perm()); err != nil {
				return "", err
			}
			return fmt.Sprintf("edited %s (1 replacement)", a.Path), nil
		},
	}
}

// ListDir returns a tool that lists a directory within root.
func ListDir(root string) Tool {
	return Tool{
		Name:        "list_dir",
		Description: "List the entries of a directory. Paths are relative to the workspace root; omit or use \".\" for the root. Directories end with \"/\".",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{"type": "string", "description": "Directory path relative to the workspace root."},
			},
		},
		Run: func(_ context.Context, raw json.RawMessage) (string, error) {
			var a struct {
				Path string `json:"path"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			if a.Path == "" {
				a.Path = "."
			}
			abs, err := safeJoin(root, a.Path)
			if err != nil {
				return "", err
			}
			entries, err := os.ReadDir(abs)
			if err != nil {
				return "", err
			}
			lines := make([]string, 0, len(entries))
			for _, e := range entries {
				if e.IsDir() {
					lines = append(lines, e.Name()+"/")
					continue
				}
				size := int64(-1)
				if info, err := e.Info(); err == nil {
					size = info.Size()
				}
				lines = append(lines, fmt.Sprintf("%s (%d bytes)", e.Name(), size))
			}
			if len(lines) == 0 {
				return "(empty)", nil
			}
			sort.Strings(lines)
			return strings.Join(lines, "\n"), nil
		},
	}
}

// isTestFile reports whether p names a Go test file. It folds case before the
// suffix compare because the target filesystem (APFS) is case-insensitive: "x_Test.go"
// names the same on-disk file as the protected x_test.go, so a byte-exact suffix
// would let a case variant slip through and clobber the spec. The security-critical
// callers (write_file/edit_file protection) must also pass the safeJoin-resolved
// path: "x_test.go/." has base "." and evades the check, yet Clean collapses it back
// to x_test.go. (shouldElide passes a raw path, which is benign — a missed match only
// forgoes an optimization, it cannot expose a write.)
func isTestFile(p string) bool {
	return strings.HasSuffix(strings.ToLower(filepath.Base(p)), "_test.go")
}

// errProtectedTest is returned when a write to a Go test file is refused; the
// message steers the model to change the implementation rather than the spec.
func errProtectedTest(path string) error {
	return fmt.Errorf("writing test files is not permitted (%s): the tests are the fixed specification — change the implementation to satisfy them, not the tests", filepath.Base(path))
}

// safeJoin resolves p against root and keeps the result inside root. Prefixing
// with "/" before Clean collapses any leading "../" so traversal cannot climb
// above root; the Rel check is a second line of defence. Confinement is purely
// lexical and does not resolve symlinks: a symlink under root pointing outside
// would still be followed. The fs tools cannot create symlinks, but adversary-
// authored code executed by `go test` could plant one that a later write then
// follows — the same accepted limitation as the forge boundary (executing the
// model's code in the deciding process), tolerable for the non-adversarial target.
func safeJoin(root, p string) (string, error) {
	abs := filepath.Join(root, filepath.Clean("/"+p))
	rel, err := filepath.Rel(root, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes the workspace", p)
	}
	return abs, nil
}
