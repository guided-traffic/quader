package formathost

import (
	"context"
	"testing"

	"github.com/guided-traffic/quader/internal/authz"
	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/proxy"
	"github.com/guided-traffic/quader/internal/qname"
	"github.com/guided-traffic/quader/internal/repository"
)

// R-AUTHZ-002, R-VULN-044: what the image scanning's database task stores is recorded for system:image-database with its reason, as a
// pass's fill is for system:mirror and a probe's background download for system:background-download; the request carries no charge, so
// the engine records the system's (Q-LIM-19).
func TestR_VULN_044_TheDatabaseTasksFillsAreRecordedForSystemImageDatabase(t *testing.T) {
	repo := repository.Repository{Name: qname.Repo{Project: "upstream", Key: "scanner-trivy"}, Class: repository.Remote, Format: "oci"}
	rq := &formats.Request{Repo: &repo}
	for _, c := range []struct {
		fill          filler
		actor, reason string
	}{
		{fillMirror, "mirror", authz.ReasonMirror},
		{fillBackgroundDownload, "background-download", authz.ReasonBackgroundDownload},
		{fillImageDatabase, "image-database", authz.ReasonImageDatabase},
		{fillDownloadAhead, "background-download", authz.ReasonDownloadAhead}, // R-CUR-010
	} {
		var preq proxy.Request
		(&backgroundFacts{fill: c.fill, ceiling: 2, jobID: "job-1"}).apply(context.Background(), &preq, rq, formats.PathInfo{Package: "aquasec/trivy-db"}, formats.Path{Path: "aquasec/trivy-db/_blobs/sha256:00"})
		pop, ok := preq.Populate.(*mirrorPopulate)
		if !ok || preq.Caller.Actor.Kind != "system" || preq.Caller.Actor.Name != c.actor || pop.subject.Name != c.actor || pop.reason != c.reason {
			t.Errorf("%s: actor %+v, populate %+v", c.actor, preq.Caller.Actor, preq.Populate)
		}
		if preq.Priority != proxy.PriorityBackground || preq.Charge != nil || preq.JobID != "job-1" {
			t.Errorf("%s: priority %v, charge %v, job %q", c.actor, preq.Priority, preq.Charge, preq.JobID)
		}
	}
	if authz.ReasonImageDatabase != "image-database" {
		t.Errorf("reason %q", authz.ReasonImageDatabase)
	}
}
