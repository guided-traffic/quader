package report

import (
	"context"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/vuln/store"
)

// scanStates is an ImageReader that answers the groups of the scan rows and the database in use, and records what it was asked.
type scanStates struct {
	ImageReader
	db     store.ImageDB
	groups []store.ImageScanGroup
	last   *store.ImageScanFailure
	asked  string
	bound  time.Duration
}

func (s *scanStates) DB(context.Context) (store.ImageDB, bool, error) {
	return s.db, s.db.Digest != "", nil
}

func (s *scanStates) ScanStates(_ context.Context, db string, bound time.Duration) ([]store.ImageScanGroup, *store.ImageScanFailure, error) {
	s.asked, s.bound = db, bound
	return s.groups, s.last, nil
}

// R-VULN-053, Q-VULN-298: the image scanning's page reads what the scanning does now from the groups of its rows: waiting rows by why they
// were put off, the rows in a call as scanning, scanned ones with those of another database counted apart, failed ones by their code, the
// oldest waiting row of any code, and the last failure as the store answered it; the groups are asked against the database in use and the
// call's bound. Unit, microseconds, no rows.
func TestR_VULN_053_TheImageScanningSaysWhatItDoesNow(t *testing.T) {
	at := func(m int) *time.Time { t := time.Date(2026, 11, 27, 8, m, 0, 0, time.UTC); return &t }
	last := &store.ImageScanFailure{Repository: "team-a/images", Name: "base/debian", Digest: "sha256:ab", Code: "image_too_large", Attempts: 1, At: at(30)}
	fake := &scanStates{
		db: store.ImageDB{Digest: "sha256:db"},
		groups: []store.ImageScanGroup{
			{State: store.ImageScanPending, N: 66, Oldest: at(12)},
			{State: store.ImageScanPending, Code: "scanner_busy", N: 11, Oldest: at(5)},
			{State: store.ImageScanPending, Running: true, N: 2, Oldest: at(1)},
			{State: store.ImageScanScanned, N: 1200},
			{State: store.ImageScanScanned, Outdated: true, N: 4},
			{State: store.ImageScanFailed, Code: "image_too_large", N: 3},
			{State: store.ImageScanFailed, Code: "timeout", N: 1},
		},
		last: last,
	}
	s := &Service{images: fake, cfg: Config{ScanCallBound: 26 * time.Minute}}
	got, err := s.ImageScans(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if fake.asked != "sha256:db" || fake.bound != 26*time.Minute {
		t.Fatalf("asked against %q within %s", fake.asked, fake.bound)
	}
	if got.Waiting != 77 || got.Scanning != 2 || got.Scanned != 1204 || got.Outdated != 4 || got.Failed != 4 {
		t.Fatalf("counts %+v", got)
	}
	if got.OldestWaiting == nil || !got.OldestWaiting.Equal(*at(5)) {
		t.Fatalf("oldest waiting %v, want the row put off at :05 (a running row does not wait)", got.OldestWaiting)
	}
	want := []ScanReason{
		{State: ReasonFailed, Code: "image_too_large", N: 3},
		{State: ReasonFailed, Code: "timeout", N: 1},
		{State: ReasonWaiting, Code: "", N: 66},
		{State: ReasonWaiting, Code: "scanner_busy", N: 11},
	}
	if len(got.Reasons) != len(want) {
		t.Fatalf("reasons %+v", got.Reasons)
	}
	for i := range want {
		if got.Reasons[i] != want[i] {
			t.Fatalf("reason %d: %+v, want %+v", i, got.Reasons[i], want[i])
		}
	}
	if got.LastFailure != last {
		t.Fatalf("last failure %+v", got.LastFailure)
	}
	if empty, err := (&Service{}).ImageScans(t.Context()); err != nil || empty.Waiting != 0 || empty.Reasons != nil {
		t.Fatalf("without the image scanning wired: %+v, %v", empty, err)
	}
}
