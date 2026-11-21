package formathost

import (
	"context"
	"testing"

	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/formattest/testformat"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/signatures"
	sigstore "github.com/guided-traffic/quader/internal/signatures/store"
)

// noSignatureFormat is a format whose ecosystem has no signatures, as the ClamAV definitions declare (Q-SIG-96).
type noSignatureFormat struct{ testformat.Format }

func (noSignatureFormat) Signatures() formats.SignatureAdapter {
	return formats.NoSignatures("no signatures")
}

type discoveryItems []sigstore.DiscoveryItem

func (d discoveryItems) DiscoveryItems(context.Context, int64, int) ([]sigstore.DiscoveryItem, error) {
	return d, nil
}

// countingRunner counts the transactions discovery opens; a transaction fails the test's expectation by being counted.
type countingRunner struct{ n int }

func (c *countingRunner) InTx(_ context.Context, _ func(meta.Tx) error) error { c.n++; return nil }

type nopRecorder struct{}

func (nopRecorder) Resolve(context.Context, meta.Tx, int64, []string) (map[string]signatures.Stored, error) {
	return map[string]signatures.Stored{}, nil
}
func (nopRecorder) Record(context.Context, meta.Tx, signatures.Batch) error { return nil }
func (nopRecorder) SubjectArrived(context.Context, meta.Tx, [32]byte) (int64, error) {
	return 0, nil
}
func (nopRecorder) RecordOrigin(context.Context, meta.Tx, signatures.Origin) error { return nil }

// TestR_SIG_011_DiscoveryAsksNoFormatWithoutSignatures: the files of a format that declares no signatures (the ClamAV definitions) cost
// discovery no transaction and no read, and the cursor passes them; a format that declares signatures is asked in a transaction of its
// own per file (R-SIG-011, Q-SIG-96). Unit, milliseconds.
func TestR_SIG_011_DiscoveryAsksNoFormatWithoutSignatures(t *testing.T) {
	plain := testformat.New()
	run := &countingRunner{}
	x := &Discoverer{
		Deps: Deps{Signatures: nopRecorder{}, Formats: func(id string) (formats.Format, bool) {
			if id == "clamav" {
				return noSignatureFormat{plain}, true
			}
			return plain, true
		}},
		Items: discoveryItems{{ID: 5, Path: "main.cvd", Format: "clamav", Size: 1}, {ID: 9, Path: "a/b.bin", Format: "generic", Size: 1}},
		DB:    run,
	}
	last, err := x.Discover(context.Background(), 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if last != 9 || run.n != 1 {
		t.Fatalf("last %d, %d transactions; want 9 and one (the file of the format that declares signatures)", last, run.n)
	}
}
