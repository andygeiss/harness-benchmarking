package main

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"os"
)

// scratchFiles are agent-maintained bookkeeping files excluded from the
// fingerprint. The system prompt tells the agent to rewrite PROGRESS.md every
// pass — it is the cross-pass memory — so churn there is expected and is NOT
// progress toward verification. Counting it would change the digest every pass
// and reset the stagnation guard's stale counter, blinding it precisely to a
// model that is stuck but still dutifully taking notes. The fingerprint therefore
// tracks the code/spec, the surface that decides the verify outcome.
var scratchFiles = map[string]bool{"PROGRESS.md": true}

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
