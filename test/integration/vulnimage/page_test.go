//go:build integration

package vulnimage

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/vuln/imagescan"
	"github.com/guided-traffic/quader/test/integration/testenv"
)

// imageScans is the answer of GET /api/v1/vulnerabilities/image-scans (R-VULN-053).
type imageScans struct {
	Enabled                                      bool
	Waiting, Scanning, Scanned, Outdated, Failed int64
	Reasons                                      []struct {
		State, Code string
		Count       int64
	}
	OldestWaitingAt *string `json:"oldestWaitingAt"`
	LastFailure     *struct {
		Repository, Image, Digest, Code, Detail string
		Attempts                                int
		At                                      *string
	} `json:"lastFailure"`
}

func (f *flow) imageScans() imageScans {
	f.t.Helper()
	a := f.do("GET", "/api/v1/vulnerabilities/image-scans", nil)
	if a.Status != http.StatusOK {
		f.t.Fatalf("image scans: %d %s", a.Status, a.Body)
	}
	var s imageScans
	if err := json.Unmarshal(a.Body, &s); err != nil {
		f.t.Fatal(err)
	}
	return s
}

// TestR_VULN_053_TheImageScanningSaysWhatItDoesNowAndAnImageSaysWhyItWaits: before a database is in use, an image whose analysis failed
// says its failure and not "No database", and one that was analysed says "No database"; the page's read counts the one waiting and the
// one failed with its code, names the waiting one's age and the failure with its repository, image, code, text, attempts and time, and is
// refused to an anonymous caller. About 3 s; two images of a few hundred bytes.
func TestR_VULN_053_TheImageScanningSaysWhatItDoesNowAndAnImageSaysWhyItWaits(t *testing.T) {
	f := newFlowWith(t, options{noDatabase: true})
	f.an.fail.Store(&imagescan.ScanError{Code: imagescan.CodeImageTooLarge, Detail: "the image is larger than the bound", Terminal: true})
	f.push("big", "1.0", newImage(t))
	f.runScan()
	testenv.WaitFor(t, "the analysis of big to fail", 30*time.Second, func() bool {
		return f.st.Number(`SELECT count(*) FROM vuln_image_scan WHERE state = 'failed'`) == 1
	})
	f.waitIdle()
	f.an.fail.Store(nil)
	// The same manifest under another name is a row of its own; its analysis is stored by its digest.
	f.push("app", "1.0", newImage(t))
	f.runScan()
	testenv.WaitFor(t, "the analysis of app stored without a database", 30*time.Second, func() bool {
		return f.st.Number(`SELECT count(*) FROM vuln_image_sbom`) == 1
	})
	f.waitIdle()

	if g := f.versionReport("big", "1.0").Repositories[0].Grade; g.State != "over_limits" {
		t.Fatalf("a failed image without a database reads %q, want over_limits", g.State)
	}
	if g := f.versionReport("app", "1.0").Repositories[0].Grade; g.State != "no_database" {
		t.Fatalf("a waiting image without a database reads %q, want no_database", g.State)
	}

	s := f.imageScans()
	if !s.Enabled || s.Waiting != 1 || s.Scanning != 0 || s.Scanned != 0 || s.Failed != 1 || s.OldestWaitingAt == nil {
		t.Fatalf("the counts: %+v", s)
	}
	failed := false
	for _, r := range s.Reasons {
		failed = failed || (r.State == "failed" && r.Code == imagescan.CodeImageTooLarge && r.Count == 1)
	}
	if !failed {
		t.Fatalf("the reasons: %+v", s.Reasons)
	}
	l := s.LastFailure
	if l == nil || l.Repository != testenv.DefaultProject+"/"+repoKey || l.Image != "big" || l.Code != imagescan.CodeImageTooLarge ||
		l.Detail == "" || l.Attempts != 1 || l.At == nil || len(l.Digest) != len("sha256:")+64 {
		t.Fatalf("the last failure: %+v", l)
	}
	if a := f.st.Do(f.api, "GET", "/api/v1/vulnerabilities/image-scans", "", nil); a.Status != http.StatusUnauthorized {
		t.Fatalf("an anonymous caller: %d", a.Status)
	}
}
