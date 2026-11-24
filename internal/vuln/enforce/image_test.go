package enforce

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/repository"
	"github.com/guided-traffic/quader/internal/vuln"
	"github.com/guided-traffic/quader/internal/vuln/advisory"
	"github.com/guided-traffic/quader/internal/vuln/store"
)

type imageReader struct{ v store.ImageVerdict }

func (r imageReader) Verdict(context.Context, int64, string, [32]byte, string, bool) (store.ImageVerdict, error) {
	return r.v, nil
}

// R-VULN-015, R-VULN-016: an image held by onMissingData block is told the state of its scan and what to do next, never that advisory
// data is missing; the audit event names the same cause.
func TestR_VULN_016_AnImagesMissingDataRefusalNamesTheStateOfItsScan(t *testing.T) {
	limits, release := vuln.JudgedLimitsPrefix+"max_layers=128", vuln.JudgedReleasePrefix+"0.90.0"
	for _, tc := range []struct {
		name     string
		database bool
		v        store.ImageVerdict
		cause    vuln.MissingCause
		text     string
	}{
		{
			"no row yet", true,
			store.ImageVerdict{Image: true},
			vuln.MissingImageNotScanned,
			"the image has not been scanned for vulnerabilities yet, and the repository refuses unscanned images; try again in a minute",
		},
		{"pending", true, store.ImageVerdict{Image: true, ScanState: store.ImageScanPending}, vuln.MissingImageNotScanned, "try again in a minute"},
		{
			"failed, tried again", true,
			store.ImageVerdict{Image: true, ScanState: store.ImageScanFailed, ErrorCode: "timeout"},
			vuln.MissingImageScanRetried,
			"the vulnerability scan of the image did not finish (timeout) and is tried again",
		},
		{
			"failed at a bound", true,
			store.ImageVerdict{Image: true, ScanState: store.ImageScanFailed, ErrorCode: "image_too_large", Judged: limits},
			vuln.MissingImageOverLimits, "it exceeds the scanning's limits (image_too_large), and the repository refuses unscanned images; ask an administrator",
		},
		{
			"failed reading", true,
			store.ImageVerdict{Image: true, ScanState: store.ImageScanFailed, ErrorCode: "layer_format", Judged: release},
			vuln.MissingImageUnreadable, "its content could not be read (layer_format), and the repository refuses unscanned images; ask an administrator",
		},
		{
			"no database", false,
			store.ImageVerdict{Image: true, ScanState: store.ImageScanPending},
			vuln.MissingImageDatabase,
			"no vulnerability database for images has been imported yet",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, states{advisory.EcosystemOCI: {Complete: tc.database}}, vuln.EnforceBlocks)
			f.e.o.Images = imageReader{tc.v}
			err := f.e.gateImage(context.Background(), imagePolicy(true), imagePull())
			e, ok := errs.As(err)
			if !ok || e.Code != errs.CodeVulnerabilityDataMissing {
				t.Fatalf("gate = %v", err)
			}
			if !strings.Contains(e.Detail, tc.text) || strings.Contains(e.Detail, "advisory data") {
				t.Errorf("text = %q\nwant it to contain %q", e.Detail, tc.text)
			}
			if !strings.HasPrefix(e.Detail, "Quader blocked team/app@sha256:") || !strings.HasSuffix(e.Detail, "Details: https://quader.example.com/ui/vulnerabilities") {
				t.Errorf("text = %q", e.Detail)
			}
			var det map[string]any
			if len(f.rec.evs) != 1 || json.Unmarshal(f.rec.evs[0].Details, &det) != nil {
				t.Fatalf("events %+v", f.rec.evs)
			}
			if det["reason"] != "missing_data" || det["missingData"] != string(tc.cause) {
				t.Errorf("details %v, want missingData %s", det, tc.cause)
			}
			if got, _ := det["scanError"].(string); got != tc.v.ErrorCode {
				t.Errorf("scanError = %q, want %q", got, tc.v.ErrorCode)
			}
		})
	}
}

// R-VULN-015: what the missing-data rule does not hold passes the image gate: a manifest whose detection has finished, one that is no
// image to scan, and an image of a repository that is not covered.
func TestR_VULN_015_TheImageGatePassesWhatTheMissingDataRuleDoesNotHold(t *testing.T) {
	for _, tc := range []struct {
		name    string
		covered bool
		v       store.ImageVerdict
	}{
		{"detected", true, store.ImageVerdict{Image: true, ScanState: store.ImageScanPending, Detected: true}},
		{"no image to scan", true, store.ImageVerdict{}},
		{"not covered", false, store.ImageVerdict{Image: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, states{advisory.EcosystemOCI: {Complete: true}}, vuln.EnforceBlocks)
			f.e.o.Images = imageReader{tc.v}
			if err := f.e.gateImage(context.Background(), imagePolicy(tc.covered), imagePull()); err != nil {
				t.Errorf("gate = %v", err)
			}
		})
	}
}

// R-VULN-016: a package held by onMissingData block is told that its ecosystem's advisory data is not there yet, by the ecosystem's name.
func TestR_VULN_016_APackagesMissingDataRefusalNamesTheAdvisoryDataOfItsEcosystem(t *testing.T) {
	f := newFixture(t, states{}, vuln.EnforceBlocks)
	p := scanned
	p.OnMissingData = vuln.MissingBlock
	m := Miss{Repo: repository.Repository{Name: qr("npm-remote"), Format: "npm"}, Package: "left-pad", Version: "1.3.0"}
	err := f.e.gate(context.Background(), p, m)
	e, ok := errs.As(err)
	if !ok || e.Code != errs.CodeVulnerabilityDataMissing {
		t.Fatalf("gate = %v", err)
	}
	want := "Quader blocked left-pad@1.3.0 in it/npm-remote: no advisory data for npm has been imported yet, and the repository refuses what it " +
		"cannot check; try again later, and ask an administrator if it stays refused. Details: https://quader.example.com/ui/vulnerabilities"
	if e.Detail != want {
		t.Errorf("text = %q\nwant   %q", e.Detail, want)
	}
	var det map[string]any
	if len(f.rec.evs) != 1 || json.Unmarshal(f.rec.evs[0].Details, &det) != nil || det["missingData"] != "advisory_data" {
		t.Errorf("events %+v", f.rec.evs)
	}
	if _, has := det["scanError"]; has {
		t.Errorf("a package's event names a scan error: %v", det)
	}
}

func imagePolicy(covered bool) vuln.Policy {
	return vuln.Policy{
		Ecosystem: advisory.EcosystemOCI, Enabled: true, Covered: covered, Enforcement: vuln.EnforceBlocks, BlockMalicious: true,
		OnMissingData: vuln.MissingBlock,
	}
}

func imagePull() ImagePull {
	return ImagePull{
		Repo: repository.Repository{ID: "3", Name: qr("oci-local"), Format: "oci"}, Addressed: "team/oci-local", Name: "team/app",
		Digest: [32]byte{1}, Path: "team/app/manifests/sha256:01", Source: audit.Source{ClientAddr: "10.0.0.1"},
	}
}
