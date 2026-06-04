package main

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// scratchFiles are agent-maintained bookkeeping files excluded from the
// fingerprint. The system prompt tells the agent to rewrite PROGRESS.md every
// pass — it is the cross-pass memory — so churn there is expected and is NOT
// progress toward verification. Counting it would change the digest every pass
// and reset the stagnation guard's stale counter, blinding it precisely to a
// model that is stuck but still dutifully taking notes. The fingerprint therefore
// tracks the code/spec, the surface that decides the verify outcome.
var scratchFiles = map[string]bool{"PROGRESS.md": true}

// staleTracker counts consecutive Ralph passes that left the workspace
// fingerprint byte-for-byte unchanged. The outer loop folds each pass's
// fingerprint in with update, which reports whether the run has stalled for
// limit passes running (limit 0 disables the guard). Keeping this state in one
// type keeps the loop body — and its cyclomatic complexity — small.
type staleTracker struct {
	limit int
	prev  string
	count int
}

// update folds fp into the tracker and reports whether the workspace has now
// gone unchanged for limit consecutive passes. The first fingerprint only
// establishes a baseline (prev is empty), so it can never trip the guard.
func (s *staleTracker) update(fp string) (stalled bool) {
	if s.prev != "" && fp == s.prev {
		s.count++
	} else {
		s.count = 0
	}
	s.prev = fp
	return s.limit > 0 && s.count >= s.limit
}

// wipeScratch removes the agent scratch files (scratchFiles) from root, ignoring
// any that are absent. The Ralph loop calls it before each pass when cross-pass
// memory is ablated (-memory=false): deleting PROGRESS.md guarantees the model
// cannot carry plan notes across the context reset, so the run measures
// resumption from the persisted code alone. Because scratchFiles are excluded
// from the fingerprint, wiping them never disturbs the stagnation guard.
func wipeScratch(root string) error {
	for name := range scratchFiles {
		if err := os.Remove(filepath.Join(root, name)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

// fingerprint returns a SHA-256 digest over every regular file under root except
// the agent scratch files in scratchFiles — each remaining file's path and bytes
// folded into one hash, files visited in lexical order. The Ralph loop compares
// fingerprints across passes to detect a pass that changed nothing on disk: the
// objective signal that the model is stuck.
func fingerprint(root string) (string, error) {
	h := sha256.New()
	fsys := os.DirFS(root)
	err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		if scratchFiles[d.Name()] {
			return nil // agent bookkeeping: its churn is not progress
		}
		f, err := fsys.Open(path)
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()
		h.Write([]byte(path))
		h.Write([]byte{0}) // frame each entry so path/content boundaries can't alias
		if _, err := io.Copy(h, f); err != nil {
			return err
		}
		h.Write([]byte{0})
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
