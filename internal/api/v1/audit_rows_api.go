package v1

import (
	"net/http"
	"strings"
	"time"

	"github.com/guided-traffic/quader/internal/api/v1/gen"
	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/httpserver"
)

// The rows of the audit log's screen (operations-and-observability.md R-OPS-055): an addition beside the events list, the read of
// one event and the export, which keep one event per item (R-OPS-054). Both operations take audit:read, as the events do, declared
// in the document and enforced before the handler runs: the reader of a row may read every event in it.

type auditDownloadItemView struct {
	ID         string    `json:"id"`
	OccurredAt time.Time `json:"occurredAt"`
	Path       string    `json:"path"`
	SHA256     string    `json:"sha256"`
	Size       *int64    `json:"size"`
	Replica    *string   `json:"replica"`
	Delivery   *string   `json:"delivery"`
}

type auditDownloadView struct {
	Unit    string                  `json:"unit"`
	Package *string                 `json:"package"`
	Version *string                 `json:"version"`
	Items   int                     `json:"items"`
	More    bool                    `json:"more"`
	Size    *int64                  `json:"size"`
	LastAt  time.Time               `json:"lastAt"`
	First   []auditDownloadItemView `json:"first"`
}

type auditRowView struct {
	Event    auditEventView     `json:"event"`
	Download *auditDownloadView `json:"download"`
}

func toAuditRow(r audit.Row) auditRowView {
	v := auditRowView{Event: toAuditEvent(r.Event)}
	if d := r.Download; d != nil {
		dv := &auditDownloadView{
			Unit: d.Unit, Package: nz(d.Package), Version: nz(d.Version), Items: d.Items, More: d.More, LastAt: d.Last.UTC(),
			First: make([]auditDownloadItemView, 0, len(d.First)),
		}
		if d.SizeKnown {
			size := d.Size
			dv.Size = &size
		}
		for _, it := range d.First {
			iv := auditDownloadItemView{
				ID: it.ID, OccurredAt: it.OccurredAt.UTC(), Path: it.Path, SHA256: it.SHA256, Replica: nz(it.Replica), Delivery: nz(it.Delivery),
			}
			if it.SizeKnown {
				size := it.Size
				iv.Size = &size
			}
			dv.First = append(dv.First, iv)
		}
		v.Download = dv
	}
	return v
}

// ListAuditRows implements GET /api/v1/audit/rows (R-OPS-055): the filters, the range and the paging of GET /api/v1/audit/events,
// a page of rows.
func (s *Server) ListAuditRows(w http.ResponseWriter, r *http.Request, _ gen.ListAuditRowsParams) {
	store, err := s.auditStore()
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	cfg := s.o.API.Config
	cfg.PageSizeDefault, cfg.PageSizeMax = audit.DefaultLimit, audit.MaxLimit
	lq, err := ParseList(r, ListSpec{Filters: append([]string{"from", "to"}, auditFilters()...)}, cfg)
	var params map[string]string
	if err == nil {
		params, err = onceEach(r.URL.Query(), append([]string{"from", "to"}, auditFilters()...)...)
	}
	var filter audit.Filter
	if err == nil {
		filter, err = auditFilter(params)
	}
	var after *audit.Cursor
	if err == nil && lq.After != nil {
		after, err = auditCursor(lq.After)
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	res, err := store.Rows(r.Context(), filter, audit.Page{Limit: lq.Limit, After: after})
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	page := Page[auditRowView]{Items: make([]auditRowView, 0, len(res.Items))}
	for _, row := range res.Items {
		page.Items = append(page.Items, toAuditRow(row))
	}
	if res.Next != nil {
		c := lq.EncodeCursor([]string{res.Next.OccurredAt.UTC().Format(time.RFC3339Nano), res.Next.ID})
		page.NextCursor = &c
	}
	WritePage(w, r, page)
}

// ListAuditDownloadEvents implements GET /api/v1/audit/downloads/{id}/events (R-OPS-055): the events of one download, oldest first.
// The cursor names the download it was issued for, so that a cursor of another download is refused.
func (s *Server) ListAuditDownloadEvents(w http.ResponseWriter, r *http.Request, id string, _ gen.ListAuditDownloadEventsParams) {
	store, err := s.auditStore()
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	cfg := s.o.API.Config
	cfg.PageSizeDefault, cfg.PageSizeMax = audit.DefaultLimit, audit.MaxLimit
	lq, err := ParseList(r, ListSpec{}, cfg)
	var after *audit.Cursor
	if err == nil && lq.After != nil {
		if len(lq.After) != 3 || !strings.EqualFold(lq.After[2], id) {
			err = invalidCursor()
		} else {
			after, err = auditCursor(lq.After[:2])
		}
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	res, err := store.DownloadItems(r.Context(), id, audit.Page{Limit: lq.Limit, After: after})
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	page := Page[auditEventView]{Items: make([]auditEventView, 0, len(res.Items))}
	for _, e := range res.Items {
		page.Items = append(page.Items, toAuditEvent(e))
	}
	if res.Next != nil {
		c := lq.EncodeCursor([]string{res.Next.OccurredAt.UTC().Format(time.RFC3339Nano), res.Next.ID, strings.ToLower(id)})
		page.NextCursor = &c
	}
	WritePage(w, r, page)
}
