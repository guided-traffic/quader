package v1

import (
	"errors"
	"net/http"
	"time"

	"github.com/guided-traffic/quader/internal/api/v1/gen"
	"github.com/guided-traffic/quader/internal/blob/summary"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/httpserver"
)

// The object-store connections and the storage summary (storage.md R-STOR-061, R-STOR-028; operations-and-observability.md
// R-OPS-023): package F of the build plan of several connections. Both read PostgreSQL and the answering replica's configuration only;
// the summary is computed by the task storage-summary, never by a read.

// ListStorageConnections implements GET /api/v1/system/storage-connections. The caller holds settings:read (declared).
func (s *Server) ListStorageConnections(w http.ResponseWriter, r *http.Request) {
	if s.o.Storage.Connections == nil {
		httpserver.Problem(w, r, unavailable("The list of the object-store connections"))
		return
	}
	views, err := s.o.Storage.Connections(r.Context())
	if err != nil {
		httpserver.Problem(w, r, errs.Wrap(err, "api.listStorageConnections"))
		return
	}
	out := gen.StorageConnectionList{Connections: make([]gen.StorageConnection, 0, len(views))}
	for _, v := range views {
		out.Connections = append(out.Connections, storageConnectionView(v))
	}
	writeJSON(w, r, http.StatusOK, out)
}

// GetStorageConnection implements GET /api/v1/system/storage-connections/{id}. The caller holds settings:read (declared).
func (s *Server) GetStorageConnection(w http.ResponseWriter, r *http.Request, id string) {
	if s.o.Storage.Connections == nil {
		httpserver.Problem(w, r, unavailable("The list of the object-store connections"))
		return
	}
	if !damagedConnectionPattern.MatchString(id) {
		httpserver.Problem(w, r, malformed("/id", "pattern", "The id is the id of a connection.", nil))
		return
	}
	views, err := s.o.Storage.Connections(r.Context())
	if err != nil {
		httpserver.Problem(w, r, errs.Wrap(err, "api.getStorageConnection"))
		return
	}
	for _, v := range views {
		if v.ID == id {
			writeJSON(w, r, http.StatusOK, storageConnectionView(v))
			return
		}
	}
	httpserver.Problem(w, r, errs.E(errs.NotFound, errs.CodeNotFound, "No object-store connection has this id."))
}

// GetStorageSummary implements GET /api/v1/storage/summary. The caller holds settings:read (declared), the storage.read of R-OPS-023
// (Q-STOR-267).
func (s *Server) GetStorageSummary(w http.ResponseWriter, r *http.Request) {
	if s.o.Storage.Summary == nil {
		httpserver.Problem(w, r, unavailable("The storage summary"))
		return
	}
	sum, err := s.o.Storage.Summary(r.Context())
	if errors.Is(err, summary.ErrNone) {
		httpserver.Problem(w, r, errs.E(errs.NotFound, errs.CodeNotFound,
			"No storage summary has been computed yet; the task storage-summary computes one (POST /api/v1/system/tasks/storage-summary/run)."))
		return
	}
	if err != nil {
		httpserver.Problem(w, r, errs.Wrap(err, "api.getStorageSummary"))
		return
	}
	writeJSON(w, r, http.StatusOK, storageSummaryView(sum))
}

// storageConnectionView is one connection of the API: what the answering replica configures only where it configures the connection.
func storageConnectionView(v summary.View) gen.StorageConnection {
	out := gen.StorageConnection{
		Id: v.ID, State: v.State, Bucket: v.Bucket, Prefix: v.Prefix, Encrypted: v.Encrypted, KeyedNames: v.KeyedNames,
		FilledAt: v.FilledAt, DrainedAt: v.DrainedAt, RetiredAt: v.RetiredAt, LastFullPassAt: v.LastFullPassAt,
		FirstSeenAt: v.FirstSeenAt.UTC(), PassStartedAt: v.PassStartedAt, EncryptedSince: v.EncryptedSince(),
		StorageKeys: make([]gen.StorageConnectionKey, 0, len(v.Keys)),
	}
	out.Replicas.Configured, out.Replicas.Live, out.Replicas.CircuitOpen = v.Configured, v.LiveReplicas, v.CircuitOpen
	out.Queue.Blobs, out.Queue.OldestQueuedAt = v.Queued, v.OldestQueuedAt
	if l := v.Local; l != nil {
		pos, endpoint, source, drain, period := l.Position, l.Endpoint, l.CredentialSource, l.Drain, int64(l.ScrubPeriod/time.Second)
		out.Position, out.Endpoint, out.CredentialSource, out.Drain, out.ScrubPeriodSeconds = &pos, &endpoint, &source, &drain, &period
		if l.CapacityBytes > 0 {
			capacity := l.CapacityBytes
			out.CapacityBytes = &capacity
		}
		out.Settings = &gen.StorageConnectionSettings{
			Region: l.Region, ForcePathStyle: l.Settings.ForcePathStyle, PayloadChecksum: l.Settings.PayloadChecksum,
			RequestChecksumCalculation: l.Settings.RequestChecksumCalculation, ResponseChecksumValidation: l.Settings.ResponseChecksumValidation,
			BatchDelete: l.Settings.BatchDelete, StorageClass: l.Settings.StorageClass, SseMode: l.Settings.SSEMode,
			SseKmsKeyId: l.Settings.SSEKMSKeyID, SseBucketKey: l.Settings.SSEBucketKey, ScrubBytesPerSecond: l.Settings.ScrubBytesPerSecond,
		}
	}
	if c := v.Capabilities; c != nil {
		caps := gen.StorageConnectionCapabilities{
			PayloadChecksumEnforced: c.PayloadChecksumEnforced, BatchDelete: c.BatchDelete, Metadata: c.Metadata,
			CopyReplacesMetadata: c.CopyReplacesMetadata, ServerSideCopyFrom: map[string]bool{},
		}
		for id, ok := range c.ServerSideCopyFrom {
			caps.ServerSideCopyFrom[id] = ok
		}
		if !c.CheckedAt.IsZero() {
			caps.CheckedAt = optionalTime(c.CheckedAt.UTC())
		}
		if a := c.SSEApplied; a != nil {
			caps.SseApplied = &gen.StorageConnectionSSEApplied{Algorithm: a.Algorithm, KmsKeyId: a.KMSKeyID}
		}
		out.Capabilities = &caps
	}
	if f := v.Figures; f != nil {
		out.Summary = &gen.StorageConnectionFigures{
			Id: f.ID, ComputedAt: v.ComputedAt.UTC(), PresentCopies: f.PresentCopies, PresentBytes: f.PresentBytes, MissingCopies: f.MissingCopies,
			CorruptCopies: f.CorruptCopies, VerifiedWithinPeriod: f.VerifiedWithinPeriod, ScrubOverdue: f.ScrubOverdue,
			BacklogBlobs: f.BacklogBlobs, BacklogBytes: f.BacklogBytes, FillRatio: f.FillRatio, SoleCopies: f.SoleCopies, SoleBytes: f.SoleBytes,
			KeyedCopies: f.KeyedCopies,
		}
	}
	for _, k := range v.Keys {
		gk := gen.StorageConnectionKey{Id: k.ID, Generation: k.Generation, State: k.State, CreatedAt: k.CreatedAt.UTC(), RetiredAt: utcOrNil(k.RetiredAt)}
		if k.DestroyedAt != nil {
			gk.DestroyedAt = optionalTime(k.DestroyedAt.UTC())
		}
		if v.Figures != nil {
			n := v.Figures.KeyCopies[k.ID]
			gk.Copies = &n
		}
		out.StorageKeys = append(out.StorageKeys, gk)
	}
	return out
}

// storageSummaryView is the summary of the API (R-STOR-028).
func storageSummaryView(s summary.Summary) gen.StorageSummary {
	out := gen.StorageSummary{
		ComputedAt: s.ComputedAt.UTC(), DurationMs: s.DurationMs, DeduplicationRatio: s.DeduplicationRatio,
		Connections:  make([]gen.StorageSummaryConnection, 0, len(s.Connections)),
		Repositories: make([]gen.StorageSummaryRepository, 0, len(s.Repositories)),
	}
	out.Blobs.Count, out.Blobs.Bytes = s.Blobs.Count, s.Blobs.Bytes
	out.Logical.Files, out.Logical.Folders, out.Logical.Bytes = s.Logical.Files, s.Logical.Folders, s.Logical.Bytes
	out.Trash.Items, out.Trash.Bytes = s.Trash.Items, s.Trash.Bytes
	out.PendingCollection.Blobs, out.PendingCollection.Bytes = s.PendingCollection.Blobs, s.PendingCollection.Bytes
	out.Damaged.Missing, out.Damaged.Corrupt = s.Damaged.Missing, s.Damaged.Corrupt
	out.Copies.Target, out.Copies.PerBlob = s.Copies.Target, storageCopyCounts(s.Copies.PerBlob)
	out.Copies.BelowTarget.Blobs, out.Copies.BelowTarget.Bytes = s.Copies.BelowTarget.Blobs, s.Copies.BelowTarget.Bytes
	for _, c := range s.Connections {
		out.Connections = append(out.Connections, gen.StorageSummaryConnection{
			Id: c.ID, PresentCopies: c.PresentCopies, PresentBytes: c.PresentBytes, MissingCopies: c.MissingCopies, CorruptCopies: c.CorruptCopies,
			VerifiedWithinPeriod: c.VerifiedWithinPeriod, ScrubOverdue: c.ScrubOverdue, BacklogBlobs: c.BacklogBlobs, BacklogBytes: c.BacklogBytes,
			FillRatio: c.FillRatio, SoleCopies: c.SoleCopies, SoleBytes: c.SoleBytes, KeyedCopies: c.KeyedCopies,
		})
	}
	for _, rp := range s.Repositories {
		out.Repositories = append(out.Repositories, gen.StorageSummaryRepository{
			Project: rp.Project, Key: rp.Key, Class: gen.StorageSummaryRepositoryClass(rp.Class), Format: rp.Format, Files: rp.Files,
			Folders: rp.Folders, Bytes: rp.Bytes, Packages: rp.Packages, Versions: rp.Versions, ShareOfLogicalBytes: rp.ShareOfLogicalBytes,
		})
	}
	return out
}
