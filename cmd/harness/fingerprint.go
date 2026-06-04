package main

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"os"
)

// fingerprint returns a SHA-256 digest over every regular file under root —
// each file's path and bytes folded into one hash, files visited in lexical
// order. The Ralph loop compares fingerprints across passes to detect a pass
// that changed nothing on disk: the objective signal that the model is stuck.
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
		f, err := fsys.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		io.WriteString(h, path)
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
