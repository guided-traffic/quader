package v1

import (
	"net/http"
	"strings"
	"time"

	"github.com/guided-traffic/quader/internal/api/v1/gen"
	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/httpserver"
)

// The rows of the audit log's screen (operations-and-observability.md R-OPS-055, R-OPS-056): an addition beside the events list, the
// read of one event and the export, which keep one event per item (R-OPS-054). Both operations take audit:read, as the events do, declared
// in the document and enforced before the handler runs: the reader of a row may read every event in it.

type auditGroupItemView struct {
	ID         string    `json:"id"`
	OccurredAt time.Time `json:"occurredAt"`
	Action     string    `json:"action"`
	Path       string    `json:"path"`
	SHA256     string    `json:"sha256"`
	Version    *string   `json:"version"`
	Size       *int64    `json:"size"`
	Replica    *string   `json:"replica"`
	Delivery   *string   `json:"delivery"`
}

type auditGroupView struct {
	Act      string               `json:"act"`
	Unit     string               `json:"unit"`
	Package  *string              `json:"package"`
	Version  *string              `json:"version"`
	Versions int                  `json:"versions"`
	Items    int                  `json:"items"`
	More     bool                 `json:"more"`
	Size     *int64               `json:"size"`
	LastAt   time.Time            `json:"lastAt"`
	First    []auditGroupItemView `json:"first"`
}

type auditRowView struct {
	Event auditEventView  `json:"event"`
	Group *auditGroupView `json:"group"`
}

func toAuditRow(r audit.Row) auditRowView {
	v := auditRowView{Event: toAuditEvent(r.Event)}
	if g := r.Group; g != nil {
		gv := &auditGroupView{
			Act: g.Act, Unit: g.Unit, Package: nz(g.Package), Version: nz(g.Version), Versions: g.Versions, Items: g.Items, More: g.More,
			LastAt: g.Last.UTC(), First: make([]auditGroupItemView, 0, len(g.First)),
		}
		if g.SizeKnown {
			size := g.Size
			gv.Size = &size
		}
		for _, it := range g.First {
			iv := auditGroupItemView{
				ID: it.ID, OccurredAt: it.OccurredAt.UTC(), Action: it.Action, Path: it.Path, SHA256: it.SHA256, Version: nz(it.Version),
				Replica: nz(it.Replica), Delivery: nz(it.Delivery),
			}
			if it.SizeKnown {
				size := it.Size
				iv.Size = &size
			}
			gv.First = append(gv.First, iv)
		}
		v.Group = gv
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

// ListAuditRowEvents implements GET /api/v1/audit/rows/{id}/events (R-OPS-055, R-OPS-056): the events of one row of several events,
// oldest first. The cursor names the row it was issued for, so that a cursor of another row is refused.
func (s *Server) ListAuditRowEvents(w http.ResponseWriter, r *http.Request, id string, _ gen.ListAuditRowEventsParams) {
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
	res, err := store.GroupItems(r.Context(), id, audit.Page{Limit: lq.Limit, After: after})
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
