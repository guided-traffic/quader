package oci

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats/oci/manifest"
	"github.com/guided-traffic/quader/internal/formats/untrusted"
	"github.com/guided-traffic/quader/internal/jobs"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// The description of a stored manifest (formats/oci.md R-OCI-040, R-OCI-042, Q-OCI-271): the job oci-describe writes what a push writes
// with the manifest's row, for a manifest that was stored another way. It reads only what the repository holds, never an upstream:
//
//   - a manifest stored before its blobs were kept (migration 00132): the manifest's own item, parsed as a push parses it, for the
//     config's and the layers' descriptors and the configuration's digest;
//   - a configuration not read yet whose blob the repository holds: a remote's manifest is stored before its config, and a mirror's
//     download and a download ahead store the config without a client's fetch.
//
// The commit of a config blob queues it for the manifests that wait for it (QueueConfigRead), the commit of a manifest whose config is
// stored already queues it (PutManifest), and the task remote-describe queues it for every manifest left (QueueUndescribed). The job
// holds no transaction while it reads a blob, and logs nothing of a configuration.

// DescribeJobKind is the job that describes one stored manifest (queue index, coalesce, key "oci-describe:<repository id>:<name>:<hex>",
// see [DescribeKey]).
const DescribeJobKind = "oci-describe"

// The bounds of one run: the attempts and the time of one attempt; a manifest is read up to the size a push takes (the format's
// MaxManifestBytes, R-OCI-020), with the nesting and the values the format's descriptor allows.
const (
	describeAttempts = 5
	describeTimeout  = 2 * time.Minute
	maxManifestBytes = 4 << 20
)

// manifestLimits are the limits the job parses a stored manifest under: those of a push (the format's parseLimits).
var manifestLimits = untrusted.Limits{JSONDepth: 32, JSONMembers: 100_000, DocumentBytes: maxManifestBytes}

// Jobs is the job queue the data port enqueues oci-describe into; *jobs.Queue implements it.
type Jobs interface {
	Enqueue(ctx context.Context, tx meta.Tx, e jobs.EnqueueSpec) (jobs.Result, error)
}

// DescribeArgs are the arguments of the job oci-describe.
type DescribeArgs struct {
	RepoID int64  `json:"repoId"`
	Name   string `json:"name"`
	// Manifest is the hex of the manifest's SHA-256.
	Manifest string `json:"manifest"`
}

// DescribeKey is the dedup key of the job of a manifest of an image name of a repository.
func DescribeKey(repoID int64, name string, manifest [32]byte) string {
	return "oci-describe:" + strconv.FormatInt(repoID, 10) + ":" + name + ":" + hex.EncodeToString(manifest[:])
}

// RegisterDescribeJob registers the kind oci-describe in the registry the composition root freezes; a nil handler answers the permanent
// failure of a job this replica cannot run.
func RegisterDescribeJob(r *jobs.Registry, describe jobs.Handler) {
	if describe == nil {
		describe = func(context.Context, *jobs.Run) error {
			return jobs.Permanent(errors.New("oci-describe: this replica has no OCI data port"))
		}
	}
	r.RegisterKind(DescribeJobKind, jobs.KindSpec{
		Queue: "index", DedupMode: jobs.Coalesce, MaxAttempts: describeAttempts, Timeout: describeTimeout, Handler: describe,
	})
}

// DescribeHandler is the handler of oci-describe over the data port.
func DescribeHandler(s *Store) jobs.Handler {
	return func(ctx context.Context, run *jobs.Run) error {
		var a DescribeArgs
		if err := json.Unmarshal(run.Args, &a); err != nil {
			return jobs.Permanent(fmt.Errorf("oci-describe: the arguments do not read: %w", err))
		}
		raw, err := hex.DecodeString(a.Manifest)
		d, ok := digestOf(raw)
		if err != nil || !ok {
			return jobs.Permanent(errors.New("oci-describe: the manifest is no SHA-256"))
		}
		return s.Describe(ctx, a.RepoID, a.Name, d)
	}
}

// queueDescribe queues oci-describe of a manifest in the transaction tx; it reports whether a job was created (one that waits already
// is not queued again).
func (s *Store) queueDescribe(ctx context.Context, tx meta.Tx, repoID int64, name string, digest [32]byte, priority int) (bool, error) {
	r, err := s.o.Jobs.Enqueue(ctx, tx, jobs.EnqueueSpec{
		Kind: DescribeJobKind, Args: DescribeArgs{RepoID: repoID, Name: name, Manifest: hex.EncodeToString(digest[:])},
		DedupKey: DescribeKey(repoID, name, digest), Priority: priority,
	})
	return r.Created, err
}

// configPath is the item path of a configuration's blob under an image name (the format's BlobPath).
func configPath(name string, config [32]byte) string {
	return name + "/_blobs/sha256:" + hex.EncodeToString(config[:])
}

// step is what one look at a manifest's row leaves to do.
type step int

const (
	stepDone step = iota
	// stepBlobs reads the stored manifest for its blobs and its configuration's digest.
	stepBlobs
	// stepConfig reads the stored configuration.
	stepConfig
)

// next is what the job does with a manifest's row: its blobs first, for an image manifest the repository holds and whose blobs are not
// recorded; then its configuration, when one that a reader reads is named, not read and held. Anything else is done: an index, a
// manifest that is gone, a configuration the repository does not hold (read when a client pulls it through the repository).
func next(t q.OCIDescribeTargetRow) step {
	switch {
	case t.BlobsUnread && t.ManifestSize >= 0 &&
		(t.MediaType == manifest.MediaTypeImageManifest || t.MediaType == manifest.MediaTypeDockerManifest):
		return stepBlobs
	case len(t.ConfigDigest) == 32 && t.FactsUnread && t.ConfigSize >= 0 && manifest.ConfigKind(t.ConfigMediaType) != "":
		return stepConfig
	}
	return stepDone
}

// Describe describes one stored manifest of an image name (R-OCI-040): its blobs, then its configuration, each read from the
// repository's own items. A blob store that fails is an error, and the job tries again; a manifest or a configuration the reader
// refuses is recorded as such and not read again.
func (s *Store) Describe(ctx context.Context, repoID int64, name string, digest [32]byte) error {
	if s.o.Blobs == nil {
		return jobs.Permanent(errors.New("oci-describe: this replica reads no blob"))
	}
	for range 3 {
		var t q.OCIDescribeTargetRow
		var found bool
		err := s.read(ctx, nil, func(qs *q.Queries) error {
			rs, err := qs.OCIDescribeTarget(ctx, q.OCIDescribeTargetParams{RepoID: repoID, Name: name, Digest: digest[:]})
			if len(rs) > 0 {
				t, found = rs[0], true
			}
			return err
		})
		if err != nil || !found {
			return err
		}
		switch next(t) {
		case stepBlobs:
			err = s.describeBlobs(ctx, repoID, name, digest, t)
		case stepConfig:
			err = s.describeConfig(ctx, repoID, name, t)
		default:
			return nil
		}
		if err != nil {
			return describeErr(err)
		}
	}
	return nil
}

// describeBlobs reads the stored manifest and records its blobs and its configuration's digest. A manifest that does not parse now
// passed the parser when it was stored; it is recorded with no layers, so that it is not read again.
func (s *Store) describeBlobs(ctx context.Context, repoID int64, name string, digest [32]byte, t q.OCIDescribeTargetRow) error {
	body, err := s.o.Blobs.Open(ctx, digest, t.ManifestSize)
	if err != nil {
		return err
	}
	data, err := untrusted.ReadAll(body, maxManifestBytes)
	_ = body.Close()
	if err != nil && !untrusted.IsTooLarge(err) {
		return err
	}
	blobs := []byte(`{"layers":[]}`)
	var config []byte
	if m, perr := manifest.Parse(data, t.MediaType, manifestLimits); err == nil && perr == nil {
		if b := manifest.ImageBlobs(m); b != nil {
			blobs = b
		}
		if c, ok := manifest.ConfigDigest(m); ok {
			config = c[:]
		}
	}
	return s.o.DB.InTx(ctx, func(tx meta.Tx) error {
		qs, err := txQueries(tx)
		if err != nil {
			return err
		}
		return qs.OCIPutBlobs(ctx, q.OCIPutBlobsParams{Blobs: blobs, ConfigDigest: config, RepoID: repoID, Name: name, Digest: digest[:]})
	})
}

// describeConfig reads the stored configuration and records what the reader read of it in every manifest of the name that names it
// and has none yet.
func (s *Store) describeConfig(ctx context.Context, repoID int64, name string, t q.OCIDescribeTargetRow) error {
	config, _ := digestOf(t.ConfigDigest)
	facts, err := manifest.ReadStoredConfig(manifest.ConfigKind(t.ConfigMediaType), t.ConfigSize, func() (io.ReadCloser, error) {
		return s.o.Blobs.Open(ctx, config, t.ConfigSize)
	})
	if err != nil {
		return err
	}
	raw, err := json.Marshal(facts)
	if err != nil {
		return err
	}
	return s.o.DB.InTx(ctx, func(tx meta.Tx) error {
		qs, err := txQueries(tx)
		if err != nil {
			return err
		}
		return qs.OCIPutConfigFacts(ctx, q.OCIPutConfigFactsParams{RepoID: repoID, Name: name, ConfigDigest: config[:], ConfigFacts: raw})
	})
}

// describeErr classifies an error of a read: a stored object that is lost or refused is permanent (a second read does not change it);
// a blob store that is unavailable and an error nobody classified are returned as they came, and the job tries again.
func describeErr(err error) error {
	if e, ok := errs.As(err); ok && e.Kind != errs.Unavailable {
		return jobs.Permanent(fmt.Errorf("oci-describe: %w", err))
	}
	return err
}

// QueueUndescribed queues oci-describe for at most limit stored manifests after the cursor (the id of the manifest's item) that are left
// to describe, at the priority given, in one transaction (the task remote-describe). It returns the cursor of the last manifest read, how
// many it queued, and done when it read fewer than limit.
func (s *Store) QueueUndescribed(ctx context.Context, after int64, limit, priority int) (next int64, queued int, done bool, err error) {
	err = s.o.DB.InTx(ctx, func(tx meta.Tx) error {
		next, queued, done = after, 0, false
		qs, err := txQueries(tx)
		if err != nil {
			return err
		}
		list, err := qs.OCIUndescribedManifests(ctx, q.OCIUndescribedManifestsParams{After: after, RowLimit: int32(min(limit, 1<<20))}) //nolint:gosec // bounded above
		if err != nil {
			return err
		}
		done = len(list) < limit
		for _, m := range list {
			d, ok := digestOf(m.Digest)
			if ok && s.o.Jobs != nil {
				created, err := s.queueDescribe(ctx, tx, m.RepoID, m.Name, d, priority)
				if err != nil {
					return err
				}
				if created {
					queued++
				}
			}
			next = m.ID
		}
		return nil
	})
	return next, queued, done, err
}

// HasUndescribed reports whether a stored manifest is left to describe (the start of the task remote-describe).
func (s *Store) HasUndescribed(ctx context.Context) (bool, error) {
	var left bool
	err := s.read(ctx, nil, func(qs *q.Queries) error {
		list, err := qs.OCIUndescribedManifests(ctx, q.OCIUndescribedManifestsParams{After: 0, RowLimit: 1})
		left = len(list) > 0
		return err
	})
	return left, err
}
