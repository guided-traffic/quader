package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// The committed vectors are exactly what this command writes, file for file: the vectors and the writer cannot drift apart,
// and a change of the format shows as a change of the committed files, which the format's rule forbids (R-STOR-079: a new
// format is a new identifier with vectors of its own).
func TestTheCommittedVectorsAreWhatTheWriterWrites(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := write(dir); err != nil {
		t.Fatal(err)
	}
	committed := filepath.Join("..", "..", "internal", "blob", "blobcrypt", "testdata")
	written := map[string]bool{}
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		written[rel] = true
		got, _ := os.ReadFile(path)
		want, err := os.ReadFile(filepath.Join(committed, rel))
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("%s differs from the committed file (%v)", rel, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	objects, _ := filepath.Glob(filepath.Join(committed, "objects", "*"))
	for _, path := range append(objects, filepath.Join(committed, "vectors.json")) {
		rel, _ := filepath.Rel(committed, path)
		if !written[rel] {
			t.Errorf("the committed %s is not written by the command", rel)
		}
	}
}
