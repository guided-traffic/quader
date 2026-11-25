package oci

import (
	"bytes"
	"testing"

	"github.com/guided-traffic/quader/internal/formats/oci/manifest"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// TestR_OCI_040_TheDescriptionReadsOnlyWhatTheRepositoryHolds: the job oci-describe reads a stored image manifest whose blobs are not
// recorded, then a configuration a reader reads that is not read and that the repository holds, and nothing else: no index, no
// configuration of an artifact, none the repository does not hold (it is read once a client pulls the image through the repository), and
// nothing of a manifest that is gone. The task remote-describe selects the same rows (OCIUndescribedManifests).
func TestR_OCI_040_TheDescriptionReadsOnlyWhatTheRepositoryHolds(t *testing.T) {
	config := bytes.Repeat([]byte{1}, 32)
	image := q.OCIDescribeTargetRow{MediaType: manifest.MediaTypeImageManifest, ManifestSize: 500, ConfigSize: -1}
	described := image
	described.BlobsUnread, described.FactsUnread = false, true
	described.ConfigDigest, described.ConfigMediaType = config, manifest.MediaTypeImageConfig
	with := func(r q.OCIDescribeTargetRow, f func(*q.OCIDescribeTargetRow)) q.OCIDescribeTargetRow {
		f(&r)
		return r
	}
	for name, c := range map[string]struct {
		row  q.OCIDescribeTargetRow
		want step
	}{
		"an image manifest stored before its blobs were kept": {with(image, func(r *q.OCIDescribeTargetRow) { r.BlobsUnread = true }), stepBlobs},
		"a Docker image manifest stored before": {with(image, func(r *q.OCIDescribeTargetRow) {
			r.BlobsUnread, r.MediaType = true, manifest.MediaTypeDockerManifest
		}), stepBlobs},
		"an index":                        {with(image, func(r *q.OCIDescribeTargetRow) { r.BlobsUnread, r.MediaType = true, manifest.MediaTypeImageIndex }), stepDone},
		"a manifest whose item is gone":   {with(image, func(r *q.OCIDescribeTargetRow) { r.BlobsUnread, r.ManifestSize = true, -1 }), stepDone},
		"a stored configuration not read": {with(described, func(r *q.OCIDescribeTargetRow) { r.ConfigSize = 900 }), stepConfig},
		"a stored chart's configuration": {with(described, func(r *q.OCIDescribeTargetRow) {
			r.ConfigSize, r.ConfigMediaType = 90, manifest.MediaTypeHelmConfig
		}), stepConfig},
		"a configuration the repository does not hold": {described, stepDone},
		"a configuration read already":                 {with(described, func(r *q.OCIDescribeTargetRow) { r.ConfigSize, r.FactsUnread = 900, false }), stepDone},
		"an artifact's configuration": {with(described, func(r *q.OCIDescribeTargetRow) {
			r.ConfigSize, r.ConfigMediaType = 2, "application/vnd.dev.cosign.artifact.sig.v1+json"
		}), stepDone},
	} {
		if got := next(c.row); got != c.want {
			t.Errorf("%s: step %d, want %d", name, got, c.want)
		}
	}
}
