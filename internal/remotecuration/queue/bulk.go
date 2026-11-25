package queue

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/mirror/discover"
	"github.com/guided-traffic/quader/internal/remotecuration"
)

// Planner names what a bulk expands to (R-CUR-010). The composition root implements it on a worker over the format's catalogue (R3),
// reading through the worker's upstream.
type Planner interface {
	// Versions lists the versions of a package the selection takes, newest first by the format's order, each with the size the
	// upstream announces (0 when it does not): the versions of the selection's range, without prereleases unless asked, at most its
	// Max, applied by a [Picker] over the format's [Order] (Q-CUR-191). A package the upstream does not have is an empty list.
	Versions(ctx context.Context, repoID int64, name string, sel Selection) ([]PlannedVersion, error)
	// Packages lists one page of the packages of a sub-section, from cursor ("" for the first page); done is true on the last page.
	// A repository whose upstream lists no levels answers [discover.ErrNotOffered].
	Packages(ctx context.Context, repoID int64, section, cursor string) (names []string, next string, done bool, err error)
}

// PlannedVersion is one version a bulk may queue.
type PlannedVersion struct {
	Version string
	// Size is the size the upstream announces, 0 when it does not.
	Size int64
}

// BulkSpec is what a person starts: every version of a package, or a sub-section, with the bounds of the preview (R-CUR-010).
type BulkSpec struct {
	RepoID             int64
	Level              Level
	Subject            string
	MaxVersions        int
	MaxBytes           int64
	IncludePrereleases bool
	// Range is the bulk's range of versions of a package, canonical and checked by the caller (Q-CUR-190); the zero value bounds
	// nothing.
	Range     Range
	StartedBy string
	// Origin, RequestID and Ticket are those of a bulk a request or an allow starts for the newest versions of a package (Q-CUR-141):
	// its rows take the origin, the request and the charge; the zero values are a person's bulk.
	Origin    remotecuration.Origin
	RequestID string
	Ticket    json.RawMessage
}

// StartBulk records a bulk in the state expanding; the dispatch task expands it step by step (R-CUR-010). tx is the caller's
// transaction (the audit event of the act).
func (s *Service) StartBulk(ctx context.Context, tx meta.Tx, b BulkSpec) (Bulk, error) {
	var out Bulk
	err := s.within(ctx, tx, func(t Tx) (err error) {
		out, err = t.InsertBulk(ctx, Bulk{
			RepoID: b.RepoID, Level: b.Level, Subject: b.Subject, MaxVersions: b.MaxVersions, MaxBytes: b.MaxBytes,
			IncludePrereleases: b.IncludePrereleases, State: BulkExpanding, StartedBy: b.StartedBy, Origin: b.Origin, RequestID: b.RequestID,
			Ticket: b.Ticket, FromVersion: b.Range.From, ToVersion: b.Range.To, ToInclusive: b.Range.To == "" || b.Range.ToInclusive,
		})
		return err
	})
	return out, err
}

// CancelBulk stops a bulk's expansion and cancels its waiting rows (R-CUR-012); running rows end on their own.
func (s *Service) CancelBulk(ctx context.Context, tx meta.Tx, repoID int64, id string) (Bulk, error) {
	b, _, found, err := s.o.Store.GetBulk(ctx, id, repoID)
	if err != nil || !found {
		return Bulk{}, firstErr(err, ErrNotFound)
	}
	switch b.State {
	case BulkExpanding, BulkQueued:
	default:
		return b, nil
	}
	err = s.within(ctx, tx, func(t Tx) error { return t.CancelBulk(ctx, b) })
	b.State = BulkCancelled
	return b, err
}

// bulkCursor is the position of a bulk's expansion (remote_download_bulks.cursor, at most 4 KiB): the page of a section's packages
// and the index of the next package on it; for a package bulk, the index of the next version.
type bulkCursor struct {
	Page  string `json:"page,omitempty"`
	Index int    `json:"index,omitempty"`
	// Next is the index of the next version of the current package.
	Next int `json:"next,omitempty"`
	// Bytes are the announced sizes of the versions queued so far.
	Bytes int64 `json:"bytes,omitempty"`
}

// packagesPerStep bounds the packages of a section one step expands, so that a step's upstream reads stay short.
const packagesPerStep = 20

// expand runs one step of a bulk's expansion (R-CUR-010): it queues versions until the bulk's bounds, the repository's bound of
// waiting rows (the expansion pauses and the next dispatch goes on) or the end of what the planner names. A planner's failure that a
// later step can overcome leaves the bulk expanding; any other ends it failed with the failure in words.
func (s *Service) expand(ctx context.Context, b Bulk, describe func(error) remotecuration.Failure) error {
	if s.o.Planner == nil {
		return s.endBulk(ctx, b, BulkFailed, &remotecuration.Failure{
			Class: remotecuration.ClassUpstreamUnexpected, Step: remotecuration.StepPlan, Code: CodeCatalogueUnavailable,
			Detail: "This process cannot list what the bulk names.", At: s.o.Now(),
		})
	}
	var cur bulkCursor
	if len(b.Cursor) > 0 {
		_ = json.Unmarshal(b.Cursor, &cur) // a cursor that does not read starts again: the rows already queued are not queued twice
	}
	st := s.o.Settings()
	waiting, err := s.o.Store.Waiting(ctx, b.RepoID)
	if err != nil {
		return err
	}
	room := max(st.MaxWaiting, 1) - waiting
	if room <= 0 {
		return nil // paused until rows drain
	}
	names, next, done, err := s.bulkNames(ctx, b, cur)
	if err != nil {
		return s.bulkFailure(ctx, b, err, describe)
	}
	if stored, err := s.storedBytes(ctx, b); err != nil {
		return err
	} else if stored >= b.MaxBytes {
		return s.endBulk(ctx, b, BulkQueued, boundFailure(s.o.Now(), CodeBulkStoredBytes))
	}
	var add []NewRow
	bounded := ""
	for cur.Index < len(names) && len(add) < room {
		name := names[cur.Index]
		vs, err := s.o.Planner.Versions(ctx, b.RepoID, name, Selection{Range: b.Range(), IncludePrereleases: b.IncludePrereleases, Max: b.MaxVersions})
		if err != nil {
			return s.bulkFailure(ctx, b, err, describe)
		}
		if b.Level == LevelPackage {
			b.VersionsFound = len(vs)
		} else if cur.Next == 0 {
			b.VersionsFound += len(vs)
		}
		for cur.Next < len(vs) && len(add) < room {
			if b.VersionsQueued+len(add) >= b.MaxVersions {
				bounded = CodeBulkMaxVersions
				break
			}
			v := vs[cur.Next]
			if v.Size > 0 && cur.Bytes+v.Size > b.MaxBytes {
				bounded = CodeBulkMaxBytes
				break
			}
			cur.Bytes += v.Size
			add = append(add, NewRow{
				RepoID: b.RepoID, Name: name, Version: v.Version, Origin: b.RowOrigin(), BulkID: b.ID, StartedBy: b.StartedBy,
				RequestID: b.RequestID, Ticket: b.Ticket,
			})
			cur.Next++
		}
		if bounded != "" || cur.Next < len(vs) {
			break
		}
		cur.Index, cur.Next = cur.Index+1, 0
		if b.Level == LevelSection && cur.Index%packagesPerStep == 0 {
			break
		}
	}
	return s.o.Store.InTx(ctx, func(t Tx) error {
		locked, ok, err := t.LockBulk(ctx, b.ID)
		if err != nil || !ok {
			return err // another step holds it, or it was cancelled meanwhile
		}
		queued := locked.VersionsQueued
		for _, n := range add {
			if _, err := s.add(ctx, t, n); err != nil {
				var qe *QueuedError
				var fe *FullError
				switch {
				case errors.As(err, &qe):
					continue // the version is queued already by someone else: it is fetched once
				case errors.As(err, &fe):
					bounded = ""
				default:
					return err
				}
				break
			}
			queued++
		}
		locked.VersionsQueued, locked.VersionsFound = queued, b.VersionsFound
		switch {
		case bounded != "":
			locked.State, locked.Failure = BulkQueued, boundFailure(s.o.Now(), bounded)
			locked.Cursor = nil
		case cur.Index >= len(names) && done:
			locked.State, locked.Cursor = BulkQueued, nil
		default:
			if cur.Index >= len(names) {
				cur = bulkCursor{Page: next, Bytes: cur.Bytes}
			}
			raw, err := json.Marshal(cur)
			if err != nil {
				return err
			}
			locked.Cursor = raw
		}
		return t.UpdateBulk(ctx, locked)
	})
}

// bulkNames are the packages of the step: the package of a package bulk, one page of a section's.
func (s *Service) bulkNames(ctx context.Context, b Bulk, cur bulkCursor) (names []string, next string, done bool, err error) {
	if b.Level == LevelPackage {
		return []string{b.Subject}, "", true, nil
	}
	return s.o.Planner.Packages(ctx, b.RepoID, b.Subject, cur.Page)
}

func (s *Service) storedBytes(ctx context.Context, b Bulk) (n int64, err error) {
	err = s.o.Store.InTx(ctx, func(t Tx) (e error) {
		n, e = t.BulkBytes(ctx, b.ID)
		return e
	})
	return n, err
}

// bulkFailure ends a bulk failed, unless a later step can overcome the failure: then the bulk stays expanding.
func (s *Service) bulkFailure(ctx context.Context, b Bulk, err error, describe func(error) remotecuration.Failure) error {
	if errors.Is(err, discover.ErrNotOffered) {
		return s.endBulk(ctx, b, BulkFailed, &remotecuration.Failure{
			Class: remotecuration.ClassUpstreamUnexpected, Step: remotecuration.StepPlan, Code: "catalogue_not_offered",
			Detail: "The upstream of this repository lists no sub-sections, so a sub-section cannot be downloaded.", At: s.o.Now(),
		})
	}
	f := describe(err)
	f.At = s.o.Now()
	if isLimit(&f) {
		// The upstream limits requests: the repository's queue waits for it, the expansion included, and goes on after (Q-CUR-170).
		s.o.Log.Info("a step of a bulk download met the upstream's rate limit; the repository's queue waits for it", "bulk", b.ID)
		return s.holdLimit(ctx, b.RepoID, f)
	}
	if f.Class.Retryable() {
		s.o.Log.Warn("a step of a bulk download failed; the next dispatch goes on", "bulk", b.ID, "class", f.Class, "error", err)
		return nil
	}
	return s.endBulk(ctx, b, BulkFailed, &f)
}

func (s *Service) endBulk(ctx context.Context, b Bulk, state BulkState, f *remotecuration.Failure) error {
	return s.o.Store.InTx(ctx, func(t Tx) error {
		locked, ok, err := t.LockBulk(ctx, b.ID)
		if err != nil || !ok {
			return err
		}
		locked.State, locked.Failure, locked.Cursor = state, f, nil
		return t.UpdateBulk(ctx, locked)
	})
}

// The codes of a bulk's own ends (R-CUR-010, Q-CUR-169): one per sentence, so that a screen writes the sentence in its language from
// the code; the failure's detail keeps the English one.
const (
	// CodeBulkMaxVersions: the bulk queued its maxVersions.
	CodeBulkMaxVersions = "bulk_max_versions"
	// CodeBulkMaxBytes: the announced sizes of the queued versions reach the bulk's maxBytes.
	CodeBulkMaxBytes = "bulk_max_bytes"
	// CodeBulkStoredBytes: the rows of the bulk stored its maxBytes.
	CodeBulkStoredBytes = "bulk_stored_bytes"
	// CodeCatalogueUnavailable: this process has no planner, so it cannot list what a bulk names.
	CodeCatalogueUnavailable = "catalogue_unavailable"
)

// boundDetail is the English sentence of a bound.
func boundDetail(code string) string {
	switch code {
	case CodeBulkMaxVersions:
		return "The bulk queued its maxVersions; it adds no more."
	case CodeBulkMaxBytes:
		return "The announced sizes of the queued versions reach the bulk's maxBytes; it adds no more."
	default:
		return "The rows of the bulk stored its maxBytes; it adds no more."
	}
}

// boundFailure is the note of a bulk a bound cut: the class bound, which the bulk's end reads as bounded, with the bound's code.
func boundFailure(now time.Time, code string) *remotecuration.Failure {
	return &remotecuration.Failure{Class: remotecuration.ClassBound, Step: remotecuration.StepPlan, Code: code, Detail: boundDetail(code), At: now}
}
