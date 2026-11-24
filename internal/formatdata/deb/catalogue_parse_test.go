package deb

import (
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/formats/deb/control"
)

// TestR_DEB_036_TheParseKeepsWhatTheCatalogueSearches: a binary row keeps its Provides, the first line of its Description and its
// Description-md5 (R-DEB-036, Q-CUR-16), read from paragraphs of a real Packages index (download.docker.com's
// trixie/stable/binary-amd64, recorded on 2026-11-24, eight paragraphs of 8 KB). Milliseconds.
func TestR_DEB_036_TheParseKeepsWhatTheCatalogueSearches(t *testing.T) {
	raw, err := os.ReadFile("../../formats/deb/testdata/catalogue/Packages")
	if err != nil {
		t.Fatal(err)
	}
	cr := control.NewReader(strings.NewReader(string(raw)), false)
	seen := map[string]entryFacts{}
	var b batch
	for {
		p, err := cr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if binaryEntry(p, &b) != 1 {
			t.Fatalf("the paragraph of %s is not read", p.Value("Package"))
		}
		seen[p.Value("Package")] = factsOf(p)
	}
	if b.n != 8 || len(b.arg.Provides) != 8 || len(b.arg.Descriptions) != 8 || len(b.arg.DescriptionMd5s) != 8 {
		t.Fatalf("the batch holds %d rows and %d/%d/%d facts", b.n, len(b.arg.Provides), len(b.arg.Descriptions), len(b.arg.DescriptionMd5s))
	}
	c := seen["containerd.io"]
	if c.provides != "containerd, runc" || c.description != "An open and reliable container runtime" {
		t.Errorf("containerd.io: %+v", c)
	}
	if d := seen["cagent"].description; strings.Contains(d, "\n") || !strings.HasPrefix(d, "cagent is a powerful") {
		t.Errorf("cagent's short description: %q", d)
	}
	if s := seen["docker-secrets-engine"]; s.provides != "" || s.description != "Docker secrets engine" {
		t.Errorf("docker-secrets-engine: %+v", s)
	}
}
