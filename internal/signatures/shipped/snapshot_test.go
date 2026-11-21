package shipped

import (
	"bytes"
	"context"
	"io"
	"io/fs"
	"net/http"
	"path"
	"regexp"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/signatures/anchors"
)

// regenerate is the hint of every failure of the snapshot: the embedded data is made by the generator, not by hand.
const regenerate = "; run `make shipped-anchors` and commit internal/signatures/shipped/data"

// TestR_SIG_017_EveryShippedDocumentParses holds the shipped anchors (R-SIG-017, Q-SIG-4, Q-SIG-29): the six keys in their order, each of
// its kind and preset, and each anchor's material parses as stored material (anchors.Parse: nuget.org's certificates of 2018 and 2021
// have expired and still judge the countersignatures made while they were valid).
func TestR_SIG_017_EveryShippedDocumentParses(t *testing.T) {
	want := []struct {
		key, kind, preset string
		pieces            int // fingerprints the anchor shows
	}{
		{KeySigstorePublic, anchors.KindSigstore, "", 1},
		{KeyNPMJS, anchors.KindPublicKey, "npmjs", 2},
		{KeyDebianArchive, anchors.KindOpenPGP, "debian", 9},
		{KeyUbuntuArchive, anchors.KindOpenPGP, "ubuntu", 3},
		{KeyNuGetOrg, anchors.KindCA, "nuget-org", 3},
		{KeyNuGetOrgTimestamping, anchors.KindCA, "nuget-org", 3},
	}
	got := Anchors()
	if len(got) != len(want) {
		t.Fatalf("%d shipped anchors, want %d", len(got), len(want))
	}
	for i, w := range want {
		a := got[i]
		if a.Key != w.key || a.Kind != w.kind || a.Preset != w.preset || a.Title == "" {
			t.Fatalf("anchor %d = %s/%s/%q %q, want %s/%s/%q", i, a.Key, a.Kind, a.Preset, a.Title, w.key, w.kind, w.preset)
		}
		p, err := anchors.Parse(a.Kind, a.Material)
		if err != nil {
			t.Fatalf("%s does not parse: %v%s", a.Key, err, regenerate)
		}
		if len(p.Describes) != w.pieces {
			t.Fatalf("%s shows %d fingerprints, want %d: %+v", a.Key, len(p.Describes), w.pieces, p.Describes)
		}
	}
	if p, _ := anchors.Parse(anchors.KindCA, got[5].Material); p.CA.Use != anchors.UseTimestamping {
		t.Fatalf("nuget-org-timestamping has the use %q", p.CA.Use)
	}
}

// TestR_SIG_017_TheSnapshotVerifiesAgainstTheEmbeddedTUFRoot replays the TUF metadata the release embeds through Refresh from the TUF root
// sigstore-go embeds (R-SIG-017): the root chain, the timestamp, the snapshot, the targets and npm's delegated role verify by their
// signatures, and both targets by their lengths and hashes, and they are the release's documents at the release's TUF version. The
// expiry is judged at the snapshot's build time, so the test does not depend on the date.
func TestR_SIG_017_TheSnapshotVerifiesAgainstTheEmbeddedTUFRoot(t *testing.T) {
	snap := Release()
	if len(snap.TrustedRoot) == 0 || len(snap.NPMKeys) == 0 || snap.TUFVersion == 0 || snap.BuiltAt.IsZero() {
		t.Fatalf("the release's snapshot is incomplete (version %d, built %s)%s", snap.TUFVersion, snap.BuiltAt, regenerate)
	}
	got, err := Refresh(context.Background(), RefreshOptions{
		URL: "https://tuf.replay.test", Client: &http.Client{Transport: replay{}}, Now: snap.BuiltAt,
	})
	if err != nil {
		t.Fatalf("the embedded TUF metadata does not verify: %v%s", err, regenerate)
	}
	if !bytes.Equal(got.TrustedRoot, snap.TrustedRoot) || !bytes.Equal(got.NPMKeys, snap.NPMKeys) || got.TUFVersion != snap.TUFVersion {
		t.Fatalf("the verified targets (version %d) are not the release's (version %d)%s", got.TUFVersion, snap.TUFVersion, regenerate)
	}
}

// replay serves the embedded TUF metadata and targets as the repository served them: data/tuf/<path>, and a target's hash-prefixed
// name from data/targets/<name>. Anything else is 404, which ends the root chain.
type replay struct{}

var hashPrefix = regexp.MustCompile(`^[0-9a-f]{64}\.`)

func (replay) RoundTrip(req *http.Request) (*http.Response, error) {
	rel := strings.TrimPrefix(req.URL.Path, "/")
	name := path.Join(dataTUF, rel)
	if t, ok := strings.CutPrefix(rel, "targets/"); ok {
		dir, file := path.Split(t)
		name = path.Join(dataTargets, dir, hashPrefix.ReplaceAllString(file, ""))
	}
	b, err := fs.ReadFile(data, name)
	status := http.StatusOK
	if err != nil {
		status, b = http.StatusNotFound, nil
	}
	return &http.Response{StatusCode: status, Body: io.NopCloser(bytes.NewReader(b)), Header: http.Header{}, Request: req}, nil
}
