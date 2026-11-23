package nugettest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"

	"github.com/guided-traffic/quader/internal/formats/nuget/nupkg"
	"github.com/guided-traffic/quader/internal/formats/nuget/rows"
)

// The embedded files of the twin (R-NUGET-024, Q-NUGET-135): what the .nuspec of a recorded version names, from its metadata as
// nuget_embedded.sql reads it, and the bytes of one file out of a package the test put, through the reader the database store uses
// (nupkg.Open). A package nobody put reads as none, as an object the store does not hold would fail: Reads counts the reads, so a
// test can show that an answer was given without one.

// PutPackage keeps the bytes of a package under their SHA-256, for EmbeddedFile.
func (s *Store) PutPackage(data []byte) {
	sum := sha256.Sum256(data)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.packages == nil {
		s.packages = map[string][]byte{}
	}
	s.packages[hex.EncodeToString(sum[:])] = append([]byte(nil), data...)
}

// PackageReads is the number of EmbeddedFile calls, each of which stands for the ranged reads of an object.
func (s *Store) PackageReads() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.packageReads
}

// EmbeddedNames reads the package paths the metadata of a live version names for its icon, its license file and its README.
func (s *Store) EmbeddedNames(_ context.Context, repo int64, lowerID, lowerVersion string) (rows.Embedded, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.versions[vkey{repo, lowerID, lowerVersion}]
	if !ok {
		return rows.Embedded{}, false, nil
	}
	return rows.EmbeddedOf(rec.in.Metadata), true, nil
}

// EmbeddedFile reads one file of a package the test put, at most max bytes.
func (s *Store) EmbeddedFile(ctx context.Context, sha []byte, size int64, path string, max int64) ([]byte, error) {
	s.mu.Lock()
	s.packageReads++
	data, ok := s.packages[hex.EncodeToString(sha)]
	s.mu.Unlock()
	if !ok || int64(len(data)) != size {
		return nil, nil
	}
	a, err := nupkg.Open(ctx, bytes.NewReader(data), size, nupkg.Limits{})
	if err != nil {
		if nupkg.IsRefusal(err) {
			return nil, nil
		}
		return nil, err
	}
	e, ok := a.Lookup(path)
	if !ok || e.Dir || e.Size > max {
		return nil, nil
	}
	b, err := a.Read(ctx, e, max)
	if err != nil && nupkg.IsRefusal(err) {
		return nil, nil
	}
	return b, err
}
