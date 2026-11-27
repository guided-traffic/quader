package v1

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"time"

	"github.com/guided-traffic/quader/internal/api/v1/gen"
	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/blob/history"
	"github.com/guided-traffic/quader/internal/blob/scrub"
	"github.com/guided-traffic/quader/internal/blob/summary"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/httpserver"
	"github.com/guided-traffic/quader/internal/meta"
)

// The list of damaged blobs and the recheck of one blob (storage.md R-STOR-027, Q-API-386, Q-STOR-267). The list reads
// PostgreSQL only; the recheck reads every copy of the blob through the connections of the answering replica.

// StorageServices are the services of the storage operations. A nil member answers its operation with 503.
type StorageServices struct {
	// Damaged reads one page of the list of damaged blobs (scrub.ListDamaged).
	Damaged func(ctx context.Context, q scrub.DamagedQuery) ([]scrub.DamagedBlob, error)
	// Recheck reads every copy of a blob whole now and returns the copies after the reads; scrub.ErrNoBlob when no live
	// blob has the digest.
	Recheck func(ctx context.Context, d blob.Digest) ([]scrub.CopyRow, error)
	// Connections lists the object-store connections with what the answering replica configures of each and the figures of the
	// latest storage summary (summary.List, R-STOR-061; storage_connections_api.go).
	Connections func(ctx context.Context) ([]summary.View, error)
	// Summary reads the latest storage summary; summary.ErrNone before the first (R-STOR-028).
	Summary func(ctx context.Context) (summary.Summary, error)
	// Overview reads what the storage overview is derived from (R-STOR-107): the connections' list, the latest summary (nil before the
	// first), the folded totals and the answering replica's min_copies and download check; the handler adds the scrub task's runs.
	Overview func(ctx context.Context) (summary.OverviewInput, error)
	// Problems reads the records the problems of [from, to) are derived from (history.ReadProblems, R-STOR-111).
	Problems func(ctx context.Context, from, to time.Time) (history.ProblemRecords, error)
	// Cells reads the records the cells of a period ending at to are derived from (history.ReadCells).
	Cells func(ctx context.Context, p history.Period, to time.Time) (history.CellRecords, error)
	// Findings reads one page of findings with the items behind them (history.ListFindings).
	Findings func(ctx context.Context, q history.FindingQuery) ([]history.Finding, error)
	// Acknowledge acknowledges an unresolved finding in tx (history.Acknowledge): history.ErrNoFinding for an unknown id,
	// history.ErrFindingResolved for a resolved one.
	Acknowledge func(ctx context.Context, tx meta.Tx, id int64, by, note string) (history.Finding, error)
}

// damagedConnectionPattern is the pattern of a connection's id (storage.md R-STOR-050).
var damagedConnectionPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,30}[a-z0-9]$`)

// ListDamagedBlobs implements GET /api/v1/storage/damaged-blobs. The caller holds settings:read (declared).
func (s *Server) ListDamagedBlobs(w http.ResponseWriter, r *http.Request, p gen.ListDamagedBlobsParams) {
	if s.o.Storage.Damaged == nil {
		httpserver.Problem(w, r, unavailable("The list of damaged blobs"))
		return
	}
	lq, err := ParseList(r, ListSpec{Filters: []string{"scope", "connection", "status"}}, s.o.API.Config)
	dq := scrub.DamagedQuery{Scope: scrub.ScopeBlobs, Limit: lq.FetchLimit()}
	if err == nil && lq.After != nil {
		var after []byte
		if len(lq.After) == 1 {
			after, err = hex.DecodeString(lq.After[0])
		}
		if len(lq.After) != 1 || err != nil || len(after) != len(blob.Digest{}) {
			err = invalidCursor()
		}
		dq.After = after
	}
	switch {
	case err != nil:
	case p.Scope != nil && !p.Scope.Valid():
		err = malformed("/scope", "enum", "The scope is blobs or copies.", nil)
	case p.Status != nil && !p.Status.Valid():
		err = malformed("/status", "enum", "The status is missing or corrupt.", nil)
	case p.Connection != nil && !damagedConnectionPattern.MatchString(*p.Connection):
		err = malformed("/connection", "pattern", "The connection is the id of a connection.", nil)
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	if p.Scope != nil {
		dq.Scope = string(*p.Scope)
	}
	if p.Connection != nil {
		dq.Connection = *p.Connection
	}
	if p.Status != nil {
		dq.Status = string(*p.Status)
	}
	rows, err := s.o.Storage.Damaged(r.Context(), dq)
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	page := NewPage(lq, rows, func(b scrub.DamagedBlob) []string { return []string{b.Digest.Hex()} })
	WritePage(w, r, MapPage(page, damagedBlobView))
}

// RecheckDamagedBlob implements POST /api/v1/storage/damaged-blobs/{sha256}/recheck. The caller holds settings:write
// (declared). The recheck is audited as storage.blob.rechecked in a transaction of its own before the answer (Q-API-477): an
// answer the audit log does not hold is not given.
func (s *Server) RecheckDamagedBlob(w http.ResponseWriter, r *http.Request, digest string) {
	if s.o.Storage.Recheck == nil || s.o.API.Store == nil {
		httpserver.Problem(w, r, unavailable("The recheck of a blob"))
		return
	}
	d, ok := blob.ParseDigest(digest)
	if !ok {
		httpserver.Problem(w, r, malformed("/sha256", "format", "The digest is 64 lower-case hexadecimal characters.", nil))
		return
	}
	copies, err := s.o.Storage.Recheck(r.Context(), d)
	if errors.Is(err, scrub.ErrNoBlob) {
		httpserver.Problem(w, r, errs.E(errs.NotFound, errs.CodeNotFound, "No stored blob has this digest."))
		return
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	plain := make([]blob.Copy, 0, len(copies))
	out := gen.DamagedBlobRecheck{Sha256: d.Hex(), Copies: make([]gen.DamagedBlobCopy, 0, len(copies))}
	for _, cp := range copies {
		plain = append(plain, cp.Copy)
		out.Copies = append(out.Copies, gen.DamagedBlobCopy{
			Connection: cp.Connection, State: gen.DamagedBlobCopyState(cp.State), StateChangedAt: cp.StateChangedAt.UTC(),
			VerifiedAt: optionalTime(cp.VerifiedAt.UTC()), CheckedAt: optionalTime(cp.CheckedAt.UTC()),
		})
	}
	out.ContentStatus = gen.DamagedBlobRecheckContentStatus(blob.DeriveContentStatus(plain))
	if err := s.auditRecheck(r, out); err != nil {
		httpserver.Problem(w, r, errs.Wrap(err, "api.recheckDamagedBlob"))
		return
	}
	writeJSON(w, r, http.StatusOK, out)
}

// recheckCopy is one copy of the event storage.blob.rechecked: its connection and its state after the reads.
type recheckCopy struct {
	Connection string `json:"connection"`
	State      string `json:"state"`
}

// auditRecheck records storage.blob.rechecked (R-STOR-027, Q-API-477, operations-and-observability.md R-OPS-014): the digest, the
// content state the copies give and each copy's state after the reads, the caller as actor.
func (s *Server) auditRecheck(r *http.Request, out gen.DamagedBlobRecheck) error {
	copies := make([]recheckCopy, 0, len(out.Copies))
	for _, c := range out.Copies {
		copies = append(copies, recheckCopy{Connection: c.Connection, State: string(c.State)})
	}
	details, err := json.Marshal(map[string]any{"sha256": out.Sha256, "contentStatus": string(out.ContentStatus), "copies": copies})
	if err != nil {
		return err
	}
	ctx := r.Context()
	return s.o.API.Store.InTx(ctx, func(tx meta.Tx) error {
		return s.recorder().RecordTx(ctx, tx, audit.Event{
			Type: audit.EventStorageBlobRechecked, Actor: ActorFrom(httpserver.PrincipalFrom(ctx)), Outcome: audit.Success, Source: auditSource(r),
			Subject: audit.Subject{Kind: "blob", ID: out.Sha256}, Details: details,
		})
	})
}

func damagedBlobView(b scrub.DamagedBlob) gen.DamagedBlob {
	v := gen.DamagedBlob{
		Sha256: b.Digest.Hex(), Size: b.Size, ContentStatus: gen.DamagedBlobContentStatus(b.ContentStatus), ItemCount: b.ItemCount,
		Copies: make([]gen.DamagedBlobCopy, 0, len(b.Copies)), Items: make([]gen.DamagedBlobItem, 0, len(b.Items)),
	}
	for _, c := range b.Copies {
		dc := gen.DamagedBlobCopy{Connection: c.Connection, State: gen.DamagedBlobCopyState(c.State), StateChangedAt: c.StateChangedAt.UTC()}
		if c.VerifiedAt != nil {
			dc.VerifiedAt = optionalTime(c.VerifiedAt.UTC())
		}
		if c.CheckedAt != nil {
			dc.CheckedAt = optionalTime(c.CheckedAt.UTC())
		}
		v.Copies = append(v.Copies, dc)
	}
	for _, it := range b.Items {
		v.Items = append(v.Items, gen.DamagedBlobItem{Project: it.Project, Repository: it.Repository, Path: it.Path, Trashed: it.Trashed})
	}
	return v
}
