package formathost

import (
	"testing"

	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/proxy"
	"github.com/guided-traffic/quader/internal/qname"
	"github.com/guided-traffic/quader/internal/remotecuration"
	"github.com/guided-traffic/quader/internal/remotecuration/queue"
	"github.com/guided-traffic/quader/internal/repository"
)

// R-OPS-049: every fill of a background request names what caused its download from the upstream. A queue row's run hands the engine
// the row's origin, id and version and the principal who started it; a probe's row has no principal; a mirror's pass is mirror; the image
// database task is scanner_database; a request without a cause is a client's (the engine's default).
func TestR_OPS_049_TheCauseOfABackgroundFill(t *testing.T) {
	repo := repository.Repository{Name: qname.Repo{Project: "upstream", Key: "docker-hub"}, Class: repository.Remote, Format: "oci"}
	rq := &formats.Request{Repo: &repo}
	person := queue.Row{
		ID: "01937d4d-1a2b-7c3d-9e4f-5a6b7c8d9e0f", Origin: remotecuration.OriginPerson, Version: "26.10",
		StartedBy: "0192f0c4-7a7e-7b51-9b1e-2f0d6c9e1a20",
	}
	probe := queue.Row{ID: "01937d4d-1a2b-7c3d-9e4f-5a6b7c8d9e10", Origin: remotecuration.OriginProbe, Version: "1.0.0"}
	for _, c := range []struct {
		name string
		u    passUpstream
		want proxy.DownloadCause
	}{
		{"a person's row", passUpstream{fill: fillDownloadAhead, queue: queueCause(&person)}, proxy.DownloadCause{
			Origin: proxy.DownloadOriginQueue, QueueOrigin: remotecuration.OriginPerson, QueueRow: person.ID, QueueVersion: "26.10",
			Principal: person.StartedBy,
		}},
		{"a probe's row", passUpstream{fill: fillBackgroundDownload, queue: queueCause(&probe)}, proxy.DownloadCause{
			Origin: proxy.DownloadOriginQueue, QueueOrigin: remotecuration.OriginProbe, QueueRow: probe.ID, QueueVersion: "1.0.0",
		}},
		{"a mirror's pass", passUpstream{fill: fillMirror}, proxy.DownloadCause{Origin: proxy.DownloadOriginMirror}},
		{
			"the image database task",
			passUpstream{fill: fillImageDatabase, queue: queueCause(&person)},
			proxy.DownloadCause{Origin: proxy.DownloadOriginScannerDatabase},
		},
	} {
		c.u.m = &MirrorHost{}
		var preq proxy.Request
		c.u.facts().apply(&preq, rq, formats.PathInfo{Package: "library/ubuntu"}, formats.Path{Path: "v2/library/ubuntu/blobs/sha256:00"})
		if preq.Cause == nil || *preq.Cause != c.want {
			t.Errorf("%s: cause %+v, want %+v", c.name, preq.Cause, c.want)
		}
	}
	if (queueCause(nil) != proxy.DownloadCause{}) {
		t.Errorf("no row: %+v", queueCause(nil))
	}
	// A pass's fill whose context carries no facts gets the mirror's (fillPath's default).
	if got := (&backgroundFacts{}).cause(); got.Origin != proxy.DownloadOriginMirror {
		t.Errorf("the default: %+v", got)
	}
}
