package formathost

import (
	"context"
	"testing"

	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/mirror"
	"github.com/guided-traffic/quader/internal/mirror/discover"
	"github.com/guided-traffic/quader/internal/proxy"
	"github.com/guided-traffic/quader/internal/qname"
	"github.com/guided-traffic/quader/internal/remotecuration"
	"github.com/guided-traffic/quader/internal/remotecuration/queue"
	"github.com/guided-traffic/quader/internal/repository"
)

// R-OPS-049: every fill of a background request names what caused its download from the upstream and the unit it belongs to. A queue
// row's run hands the engine the row's origin, id and version, the principal who started it and the run's unit; a probe's row has no
// principal; a mirror's pass is mirror with its job and the version of the plan item or of what the format reads for a version; the image
// database task is scanner_database with its run; a request without a cause is a client's (the engine's default).
func TestR_OPS_049_TheCauseOfABackgroundFill(t *testing.T) {
	repo := repository.Repository{Name: qname.Repo{Project: "upstream", Key: "docker-hub"}, Class: repository.Remote, Format: "oci"}
	rq := &formats.Request{Repo: &repo}
	person := queue.Row{
		ID: "01937d4d-1a2b-7c3d-9e4f-5a6b7c8d9e0f", Origin: remotecuration.OriginPerson, Version: "26.10",
		StartedBy: "0192f0c4-7a7e-7b51-9b1e-2f0d6c9e1a20",
	}
	probe := queue.Row{ID: "01937d4d-1a2b-7c3d-9e4f-5a6b7c8d9e10", Origin: remotecuration.OriginProbe, Version: "1.0.0"}
	tagged := discover.ForVersion(context.Background(), "26.10")
	for _, c := range []struct {
		name string
		ctx  context.Context
		u    passUpstream
		want proxy.DownloadCause
	}{
		{"a person's row", context.Background(), passUpstream{fill: fillDownloadAhead, queue: queueCause(&person, "job-1")}, proxy.DownloadCause{
			Origin: proxy.DownloadOriginQueue, QueueOrigin: remotecuration.OriginPerson, QueueRow: person.ID, Version: "26.10",
			Unit: "q|" + person.ID + "|job-1", Principal: person.StartedBy,
		}},
		{"a probe's row", context.Background(), passUpstream{fill: fillBackgroundDownload, queue: queueCause(&probe, "job-2")}, proxy.DownloadCause{
			Origin: proxy.DownloadOriginQueue, QueueOrigin: remotecuration.OriginProbe, QueueRow: probe.ID, Version: "1.0.0",
			Unit: "q|" + probe.ID + "|job-2",
		}},
		{"a mirror's pass", context.Background(), passUpstream{fill: fillMirror, pass: mirror.Pass{JobID: "job-3"}}, proxy.DownloadCause{
			Origin: proxy.DownloadOriginMirror, Unit: "job-3",
		}},
		{"a mirror's pass reading for a tag", tagged, passUpstream{fill: fillMirror, pass: mirror.Pass{JobID: "job-3"}}, proxy.DownloadCause{
			Origin: proxy.DownloadOriginMirror, Unit: "job-3", Version: "26.10",
		}},
		{
			"the image database task", tagged,
			passUpstream{fill: fillImageDatabase, pass: mirror.Pass{JobID: "job-4"}, queue: queueCause(&person, "job-4")},
			proxy.DownloadCause{Origin: proxy.DownloadOriginScannerDatabase, Unit: "s|job-4"},
		},
	} {
		c.u.m = &MirrorHost{}
		var preq proxy.Request
		c.u.facts().apply(c.ctx, &preq, rq, formats.PathInfo{Package: "library/ubuntu"}, formats.Path{Path: "v2/library/ubuntu/blobs/sha256:00"})
		if preq.Cause == nil || *preq.Cause != c.want {
			t.Errorf("%s: cause %+v, want %+v", c.name, preq.Cause, c.want)
		}
	}
	if (queueCause(nil, "job") != proxy.DownloadCause{}) {
		t.Errorf("no row: %+v", queueCause(nil, "job"))
	}
	// A pass's fill whose context carries no facts gets the mirror's (fillPath's default).
	if got := (&backgroundFacts{}).cause(""); got.Origin != proxy.DownloadOriginMirror {
		t.Errorf("the default: %+v", got)
	}
}
