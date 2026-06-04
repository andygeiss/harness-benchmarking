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

// ReadFile returns a tool that reads a text file within root.
func ReadFile(root string) Tool {
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
			b, err := os.ReadFile(abs)
			if err != nil {
				return "", err
			}
			return clip(string(b), maxFileBytes), nil
		},
	}
}

// WriteFile returns a tool that creates or replaces a file within root.
func WriteFile(root string) Tool {
	return Tool{
		Name:        "write_file",
		Description: "Create or overwrite a file. Writes the ENTIRE file, so provide the complete intended contents. Paths are relative to the workspace root.",
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
func EditFile(root string) Tool {
	return Tool{
		Name:        "edit_file",
		Description: "Replace an exact, unique snippet of text in a file. old_string must occur EXACTLY once — include enough surrounding context to make it unique. Prefer this over write_file for small changes. Paths are relative to the workspace root.",
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

// safeJoin resolves p against root and keeps the result inside root. Prefixing
// with "/" before Clean collapses any leading "../" so traversal cannot climb
// above root; the Rel check is a second line of defence.
func safeJoin(root, p string) (string, error) {
	abs := filepath.Join(root, filepath.Clean("/"+p))
	rel, err := filepath.Rel(root, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes the workspace", p)
	}
	return abs, nil
}
