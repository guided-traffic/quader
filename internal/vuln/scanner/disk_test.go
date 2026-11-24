package scanner

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"syscall"
	"testing"

	"github.com/guided-traffic/quader/internal/vuln/imagescan"
)

// fullDisk is a spool file whose volume is full: every write fails with ENOSPC; failFlush makes the writes pass and the flush fail.
type fullDisk struct{ failFlush bool }

func (d fullDisk) Write(p []byte) (int, error) {
	if d.failFlush {
		return len(p), nil
	}
	return 0, syscall.ENOSPC
}

func (d fullDisk) Sync() error {
	if d.failFlush {
		return syscall.EIO
	}
	return nil
}

// Q-VULN-237: a write or a flush of the scanner's own disk that fails is the scanner's, not the image's and not the caller's: the call
// is answered 503 scanner_disk_failed, which the worker puts off without an attempt on another scanner, never 400 malformed_request.
func TestR_VULN_041_AFailedWriteOfTheScannersDiskIsTheScannersAndNotTheCallers(t *testing.T) {
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	_, _ = zw.Write([]byte(`{"bomFormat":"CycloneDX","specVersion":"1.6"}`))
	_ = zw.Close()
	s := &Service{o: Options{Log: slog.New(slog.DiscardHandler)}}
	for _, d := range []fullDisk{{}, {failFlush: true}} {
		err := decodeAnalysis(bytes.NewReader(gz.Bytes()), d)
		ce := s.callErrorOf(context.Background(), err)
		if ce.status != http.StatusServiceUnavailable || ce.code != imagescan.CodeScannerDiskFailed || !errors.Is(err, syscall.ENOSPC) && !errors.Is(err, syscall.EIO) {
			t.Fatalf("flush fails %t: %v answered %d %s", d.failFlush, err, ce.status, ce.code)
		}
	}
	// A body that is no gzip stays the caller's.
	if ce := s.callErrorOf(context.Background(), decodeAnalysis(bytes.NewReader([]byte("plain")), fullDisk{})); ce.code != imagescan.CodeMalformedRequest {
		t.Fatalf("a body that is no gzip: %d %s", ce.status, ce.code)
	}
}
