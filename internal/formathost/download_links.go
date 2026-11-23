package formathost

import (
	"context"
	"net/http"
	"strings"

	"github.com/guided-traffic/quader/internal/api/edge"
	"github.com/guided-traffic/quader/internal/bastionmain"
	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/downloadlinks"
	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/httpserver"
	"github.com/guided-traffic/quader/internal/httpserver/content"
	"github.com/guided-traffic/quader/internal/items"
	"github.com/guided-traffic/quader/internal/limits"
	"github.com/guided-traffic/quader/internal/observability"
	"github.com/guided-traffic/quader/internal/qname"
)

// The delivery by signed links of the object store at the serve point (storage.md R-STOR-070 to R-STOR-073): the steps that read
// the request, the item row, the request's charge and the replica's snapshots are answered here, before edge.ServeItem; the
// conditionals (step 7) and Admit (step 11) are edge.ServeItem's as for every download, and the connection and the signing (steps
// 12 and 13) are internal/downloadlinks'. A step that does not hold leaves the download to Quader, which streams it; the steps that
// answer by themselves (a gate's refusal, 304, 412, Admit's 429) answer before any link. /files/ never gets here.

// linkOptions sets ServeOptions.Link and Linked on o when steps 1 to 6, 8 and 9 hold for the download of row; decided is whether
// every gate passed on a decided state (step 6, Q-STOR-284). Step 10 is no condition: a speed limit of the caller keeps no download
// from a link (usage-and-limits.md R-LIM-040, Q-LIM-99). A GET of a declaring route that is streamed while the switch is on is
// counted with the first condition that did not hold.
func (s itemStore) linkOptions(ctx context.Context, rq *formats.Request, it formats.Item, row items.Item, decided bool, o *edge.ServeOptions) {
	links := s.h.o.Links
	if links == nil || rq == nil || rq.Request == nil {
		return
	}
	set := links.Settings()
	// Step 1: the route declares that its clients check a digest, and the method is GET (a HEAD is answered from the row).
	if !set.Enabled || rq.Route == nil || rq.Route.Kind != formats.Download || !rq.Route.ClientDigest.Checks || rq.Method != http.MethodGet {
		return
	}
	charge := limits.FromContext(ctx)
	decline := func(reason string) { links.Metrics().Declined(reason) }
	switch {
	case recognizedBastion(ctx): // step 2: a Bastion follows no redirect and may get a handover instead (R-STOR-095)
		decline(downloadlinks.DeclinedBastion)
		return
	case systemRead(rq): // step 3: the scanners' reads are streamed
		return
	case s.optedOut(ctx, rq.Addressed(), it.Ref.Repo.Name): // step 4
		decline(downloadlinks.DeclinedOptOut)
		return
	case row.ContentStatus != blob.StatusOK: // step 5, beside Usable and ServeCheck, which ran before
		decline(downloadlinks.DeclinedConnection)
		return
	case !decided: // step 6
		decline(downloadlinks.DeclinedGate)
		return
	case rq.Header.Get("Range") != "": // step 8: Quader answers every range itself (Q-STOR-285)
		decline(downloadlinks.DeclinedRange)
		return
	case row.Size < set.MinSize: // step 9
		decline(downloadlinks.DeclinedSize)
		return
	}
	disposition := content.Headers(content.File, fileName(row.Path), false).Get("Content-Disposition")
	o.Link = func() (blob.Link, bool) {
		link, declined := links.Sign(ctx, row, disposition)
		if declined != "" {
			decline(declined)
			return blob.Link{}, false
		}
		return link, true
	}
	// Step 14: everything is counted when the link is issued (R-STOR-073): the size as cached bytes of the budget key, one download
	// in the statistics and the repository's activity, artifact.downloaded where downloads are audited, the request log's delivery,
	// the metrics.
	o.Linked = func(link blob.Link) {
		charge.ChargeLink(row.Size)
		links.Metrics().Issued(rq.Repo.Format, row.Size)
		if ri := observability.RequestInfoFrom(ctx); ri != nil {
			ri.SetDelivery("link")
		}
		s.h.o.Items.Served(ctx, items.ServedEvent{
			Item: row, Actor: edge.ActorFrom(httpserver.PrincipalFrom(ctx)), Source: httpserver.SourceFrom(ctx),
			Endpoint: rq.Subject.Endpoint, CountStats: true, Delivery: "link", LinkExpiresAt: link.ExpiresAt,
		})
	}
}

// handoverOptions sets ServeOptions.Handover and HandedOver on o for a recognized forward of a Bastion that offered to take a
// handover per blob (storage.md R-STOR-095): steps 1, 5, 7, 9 and the counting of 11 are internal/downloadlinks', 4 and 6
// edge.ServeItem's, and this serve point says what it decided of step 2 (a stored file of a route of the kind Download, never a
// system read) and step 3 (decided is whether every gate passed on a decided state). Unlike a link it does not read the route's
// ClientDigest: the Bastion checks every byte itself (R-BAS-074), so every format's artifact can be handed over.
func (s itemStore) handoverOptions(ctx context.Context, rq *formats.Request, row items.Item, decided bool, o *edge.ServeOptions) {
	links := s.h.o.Links
	if links == nil || rq == nil || rq.Request == nil || rq.Method != http.MethodGet {
		return
	}
	o.Handover, o.HandedOver = links.HandoverOptions(ctx, downloadlinks.HandoverDownload{
		Item: row, Artifact: rq.Route != nil && rq.Route.Kind == formats.Download && !systemRead(rq), Decided: decided,
		Record: func(e items.ServedEvent) {
			e.Actor, e.Source, e.Endpoint = edge.ActorFrom(httpserver.PrincipalFrom(ctx)), httpserver.SourceFrom(ctx), rq.Subject.Endpoint
			s.h.o.Items.Served(ctx, e)
		},
	})
}

// fileName is the last segment of a path, the name of Content-Disposition (as edge.ServeItem names it).
func fileName(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	return p
}

// recognizedBastion reports whether the request carries a recognized Bastion token (bastionmain.RecognitionFrom).
func recognizedBastion(ctx context.Context) bool {
	_, ok := bastionmain.RecognitionFrom(ctx)
	return ok
}

// optedOut reports whether the addressed repository or the repository that holds the item says storage.downloadLinks: false
// (R-STOR-014, Q-STOR-282), from the replica's snapshot. A repository the snapshot does not answer for streams.
func (s itemStore) optedOut(ctx context.Context, addressed, holder qname.Repo) bool {
	if s.h.o.Repos == nil {
		return false
	}
	for _, name := range []qname.Repo{holder, addressed} {
		if name.IsZero() {
			continue
		}
		r, err := s.h.o.Repos.ByName(ctx, name)
		if err != nil || !r.DownloadLinks() {
			return true
		}
	}
	return false
}
