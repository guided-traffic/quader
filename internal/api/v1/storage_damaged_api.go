package v1

import (
	"context"
	"encoding/hex"
	"errors"
	"net/http"
	"regexp"

	"github.com/guided-traffic/quader/internal/api/v1/gen"
	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/blob/scrub"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/httpserver"
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
// (declared).
func (s *Server) RecheckDamagedBlob(w http.ResponseWriter, r *http.Request, digest string) {
	if s.o.Storage.Recheck == nil {
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
	writeJSON(w, r, http.StatusOK, out)
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
