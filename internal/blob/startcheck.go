package blob

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/guided-traffic/quader/internal/config"
)

// The start check of every connection (R-STOR-035, R-STOR-051, R-STOR-060): the check of StartupCheck with the connection's
// payload checksum, the marker that names the location's installation and connection, and the capabilities.

// markerName is the marker object below a connection's prefix (R-STOR-059): written once, never deleted by Quader.
const markerName = "_connection"

// MarkerKey is the key of a connection's marker.
func MarkerKey(prefix string) string {
	if prefix == "" {
		return markerName
	}
	return prefix + "/" + markerName
}

// Marker is the content of a connection's marker <prefix>/_connection (R-STOR-051): the installation and the connection the
// location belongs to.
type Marker struct {
	InstallationID string    `json:"installationId"`
	ConnectionID   string    `json:"connectionId"`
	CreatedAt      time.Time `json:"createdAt"`
	// Key is the object the marker was read from; it is not stored in the marker.
	Key string `json:"-"`
}

// ErrNotMarker is the error of ReadMarker for an object at the marker's key that is no marker of a connection.
var ErrNotMarker = errors.New("no marker of a connection")

// ReadMarker reads the marker below prefix (R-STOR-051): found is false when there is none, an object there that is not a
// marker is ErrNotMarker, and any other error is the store's.
func ReadMarker(ctx context.Context, s ObjectStore, prefix string) (m Marker, found bool, err error) {
	key := MarkerKey(prefix)
	body, _, err := s.Get(ctx, key, nil)
	switch {
	case errors.Is(err, ErrNotFound):
		return Marker{}, false, nil
	case err != nil:
		return Marker{}, false, fmt.Errorf("read the marker %s: %w", key, err)
	}
	data, err := io.ReadAll(io.LimitReader(body, 64<<10))
	_ = body.Close()
	if err != nil {
		return Marker{}, false, fmt.Errorf("read the marker %s: %w", key, err)
	}
	if json.Unmarshal(data, &m) != nil || m.InstallationID == "" || m.ConnectionID == "" {
		return Marker{Key: key}, true, fmt.Errorf("%s: %w", key, ErrNotMarker)
	}
	m.Key = key
	return m, true, nil
}

// StartCheckOptions configure the start check of the connections.
type StartCheckOptions struct {
	// Installation is the installation's id, which the marker names (system_state, R-STOR-051); empty checks no marker, for a
	// tool that knows no installation.
	Installation string
	// Replica names the probe objects: the replica's instance id.
	Replica string
	// HoldsCopies reports whether a connection holds a copy, for the warning of a missing marker; nil reports none.
	HoldsCopies func(ctx context.Context, conn string) (bool, error)
	// Record stores what the check of a connection found (storage_connections.capabilities); nil stores nothing.
	Record func(ctx context.Context, conn string, caps Capabilities) error
	// Log receives the failures that are retried and a draining connection's configuration error.
	Log *slog.Logger
	// Await tunes the backoff of the retries.
	Await []AwaitOption
}

func (o StartCheckOptions) log() *slog.Logger {
	if o.Log == nil {
		return slog.New(slog.DiscardHandler)
	}
	return o.Log
}

// ErrMetadataNotKept is the start check's refusal of an encrypted connection whose store does not keep an object's metadata
// (R-STOR-075, Q-STOR-306): a configuration error, which stops the start.
var ErrMetadataNotKept = errors.New("an encrypted connection needs a store that keeps object metadata")

// CheckConnection runs the start check of one connection once (R-STOR-035): write, read back, compare and delete a probe
// with the connection's payload checksum, list the blob prefix, the part-identity check, the marker, and the capabilities:
// whether a wrong payload checksum is refused, DeleteObjects (when batch_delete is true), whether metadata survives a write and
// a copy replaces it, and a server-side copy from each other connection on the same endpoint. A refused payload checksum, a
// failing DeleteObjects and a marker of another installation or connection are configuration errors naming the key; a missing
// capability is recorded with its stated consequence (Q-STOR-248). It changes nothing outside <prefix>/_probe/ and the marker.
func CheckConnection(ctx context.Context, c *Connection, peers []*Connection, o StartCheckOptions) (Capabilities, error) {
	var caps Capabilities
	objects, prefix := c.Objects(), c.Prefix()
	alg := c.PayloadChecksum()
	if alg == "none" {
		alg = ""
	}
	if err := startupCheck(ctx, objects, prefix, o.Replica, alg); err != nil {
		return caps, checksumRefusal(c, err)
	}
	if o.Installation != "" {
		if err := checkMarker(ctx, c, o); err != nil {
			return caps, err
		}
	}
	e := &ConformanceEnv{Store: objects, Opts: ConformanceOptions{Prefix: prefix, Replica: o.Replica, PayloadChecksum: alg}}
	defer e.cleanup(ctx)
	if alg != "" {
		enforced, err := probeChecksumEnforced(ctx, e)
		if err != nil {
			return caps, err
		}
		caps.PayloadChecksumEnforced = enforced
	}
	if c.BatchDelete() {
		if err := checkDeleteMany(ctx, e); err != nil {
			if fallsOver(err) {
				return caps, err
			}
			return caps, fmt.Errorf("%s batch_delete: DeleteObjects failed; set batch_delete: false for a store without it: %w", c.Element(), err)
		}
		caps.BatchDelete = true
	}
	kept, replaced, err := probeMetadata(ctx, e)
	if err != nil {
		return caps, err
	}
	caps.Metadata, caps.CopyReplacesMetadata = kept, replaced
	if c.Encrypted() && !kept {
		// the record lives in the object's metadata (R-STOR-077); without the copy that replaces it the finalize streams (Q-STOR-306)
		return caps, fmt.Errorf("%w: connection %s: the store did not keep the metadata of the probe object; an encrypted connection keeps each object's wrapped data key there (storage.md R-STOR-077)",
			ErrMetadataNotKept, c.ID())
	}
	for _, p := range peers {
		if p == c || p.Encrypted() || c.Encrypted() || !sameEndpoint(c, p) {
			continue // stage 1 copies server-side only between two connections without encryption (R-STOR-062)
		}
		ok, known, err := probePeerCopy(ctx, e, ConformancePeer{ID: p.ID(), Prefix: p.Prefix(), Store: p.Objects()})
		if err != nil || !known {
			continue
		}
		if caps.ServerSideCopyFrom == nil {
			caps.ServerSideCopyFrom = map[string]bool{}
		}
		caps.ServerSideCopyFrom[p.ID()] = ok
	}
	caps.CheckedAt = time.Now().UTC().Truncate(time.Second)
	return caps, nil
}

// sameEndpoint reports whether two connections lie on one endpoint as their clients report it.
func sameEndpoint(a, b *Connection) bool {
	ca, ok1 := a.Objects().(BucketCopier)
	cb, ok2 := b.Objects().(BucketCopier)
	if !ok1 || !ok2 {
		return false
	}
	ea, _ := ca.Location()
	eb, _ := cb.Location()
	return ea != "" && ea == eb
}

// checksumRefusal turns a store's refusal of the configured payload checksum into the configuration error of R-STOR-035 that
// names the keys and the values to try.
func checksumRefusal(c *Connection, err error) error {
	var se *StoreError
	if !errors.As(err, &se) || se.Kind == KindTransient {
		return err
	}
	switch se.Code {
	case "InvalidArgument", "InvalidRequest", "NotImplemented", "XAmzContentChecksumMismatch":
	default:
		if se.Status != 501 {
			return err
		}
	}
	cfg := c.Config()
	return &StoreError{Op: se.Op, Key: se.Key, Code: se.Code, Status: se.Status, Kind: KindConfig, Err: fmt.Errorf(
		"%s payload_checksum: the store refuses a write with payload_checksum: %s and request_checksum_calculation: %s; try payload_checksum: md5 or none, or request_checksum_calculation: when_required (R-STOR-035): %w",
		c.Element(), cfg.PayloadChecksum, cfg.RequestChecksumCalculation, se.Err)}
}

// checkMarker reads the connection's marker (R-STOR-051): absent, it is written, with a warning when the connection holds
// copies; present with this installation and this id, the check passes; present with another installation or id, or
// unreadable, the connection is refused as a configuration error naming both.
func checkMarker(ctx context.Context, c *Connection, o StartCheckOptions) error {
	key := MarkerKey(c.Prefix())
	m, found, err := ReadMarker(ctx, c.Objects(), c.Prefix())
	switch {
	case errors.Is(err, ErrNotMarker):
		return fmt.Errorf("%s %s/%s holds an object %s that is no marker of a connection; a location belongs to one connection of one installation (R-STOR-051)",
			c.Element(), c.Bucket(), c.Prefix(), key)
	case err != nil:
		return err
	case !found:
		data, _ := json.Marshal(Marker{InstallationID: o.Installation, ConnectionID: c.ID(), CreatedAt: time.Now().UTC().Truncate(time.Second)})
		alg := c.PayloadChecksum()
		if err := c.Objects().Put(ctx, key, bytes.NewReader(data), int64(len(data)), probeChecksum(alg, data), nil); err != nil {
			return fmt.Errorf("write the marker %s: %w", key, err)
		}
		if o.HoldsCopies != nil {
			if holds, herr := o.HoldsCopies(ctx, c.ID()); herr == nil && holds {
				o.log().Warn("the marker of a connection that holds copies was missing and is written again; the scrub will find what the location lacks",
					"connection", c.ID(), "marker", key)
			}
		}
		return nil
	}
	if m.InstallationID == o.Installation && m.ConnectionID == c.ID() {
		return nil
	}
	return fmt.Errorf("%s %s/%s is connection '%s' of installation %s; a location belongs to one connection of one installation. Remove the object %s only if that installation is gone (R-STOR-051)",
		c.Element(), c.Bucket(), c.Prefix(), m.ConnectionID, m.InstallationID, key)
}

// probeChecksumEnforced writes a probe with a wrong payload checksum: refused, the store enforces the checksum; accepted, it
// does not, and the connection's writes are not verified (R-STOR-052). A transient failure is returned.
func probeChecksumEnforced(ctx context.Context, e *ConformanceEnv) (bool, error) {
	key := e.Key("wrong-checksum")
	data := payload(64)
	wrong := probeChecksum(e.Opts.PayloadChecksum, payload(64))
	err := e.Store.Put(ctx, key, bytes.NewReader(data), int64(len(data)), wrong, nil)
	switch {
	case err == nil:
		return false, nil
	case fallsOver(err):
		return false, err
	}
	return true, nil
}

// probeMetadataKey is the key of the metadata the probes write; the prefix quader- is Quader's (R-STOR-062).
const probeMetadataKey = "quader-probe"

// probeMetadata writes a probe with one metadata key and reads it back, then copies the probe onto itself with another value
// and reads object and metadata back (R-STOR-035). A copy replaces the metadata only if the object keeps its bytes: a gateway
// is known to have lost an object's data in a copy onto itself, which no stage may build on. A refusal records the capability
// as absent; a transient failure is returned.
func probeMetadata(ctx context.Context, e *ConformanceEnv) (kept, replaced bool, err error) {
	key := e.Key("metadata")
	data := payload(32)
	if err := e.Store.Put(ctx, key, bytes.NewReader(data), int64(len(data)), probeChecksum(e.Opts.PayloadChecksum, data), Metadata{probeMetadataKey: "written"}); err != nil {
		if fallsOver(err) {
			return false, false, err
		}
		return false, false, nil
	}
	info, err := e.Store.Head(ctx, key)
	if err != nil {
		return false, false, err
	}
	kept = info.Metadata[probeMetadataKey] == "written"
	if err := e.Store.Copy(ctx, key, key, int64(len(data)), Metadata{probeMetadataKey: "replaced"}); err != nil {
		if fallsOver(err) {
			return kept, false, err
		}
		return kept, false, nil
	}
	body, info, err := e.Store.Get(ctx, key, nil)
	if err != nil {
		if fallsOver(err) {
			return kept, false, err
		}
		return kept, false, nil // the copy lost the object
	}
	got, err := io.ReadAll(io.LimitReader(body, int64(len(data))+1))
	_ = body.Close()
	if err != nil {
		return kept, false, err
	}
	return kept, bytes.Equal(got, data) && info.Metadata[probeMetadataKey] == "replaced", nil
}

// probePeerCopy copies a probe object of a peer's bucket, written with the peer's client, into this store's bucket with this
// store's credential (R-STOR-035, R-STOR-056). known is false when the peer could not be asked or the store has no copy
// between buckets; ok is whether the copy yielded the source's bytes.
func probePeerCopy(ctx context.Context, e *ConformanceEnv, peer ConformancePeer) (ok, known bool, err error) {
	copier, isCopier := e.Store.(BucketCopier)
	source, isSource := peer.Store.(BucketCopier)
	if !isCopier || !isSource {
		return false, false, nil
	}
	_, srcBucket := source.Location()
	src := probePrefix(peer.Prefix) + e.Opts.Replica + ".peer-source." + e.Opts.Replica
	data := payload(48)
	if err := putBytes(ctx, peer.Store, src, data); err != nil {
		return false, false, err
	}
	defer func() {
		cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		_ = peer.Store.Delete(cctx, src)
	}()
	dst := e.Key("peer-copy")
	if err := copier.CopyFrom(ctx, srcBucket, src, dst, int64(len(data)), nil); err != nil {
		if fallsOver(err) {
			return false, false, err
		}
		return false, true, nil
	}
	got, err := readAll(ctx, e.Store, dst, nil)
	if err != nil {
		return false, true, nil
	}
	return bytes.Equal(got, data), true, nil
}

func checkChecksumEnforced(ctx context.Context, e *ConformanceEnv) error {
	if e.Opts.PayloadChecksum == "" || e.Opts.PayloadChecksum == "none" {
		return note("skipped: the connection sends no payload checksum")
	}
	enforced, err := probeChecksumEnforced(ctx, e)
	switch {
	case err != nil:
		return err
	case !enforced:
		return errors.New("a write with a wrong payload checksum was accepted: the connection's writes are not verified, and the scrub reads them first (payloadChecksumEnforced: false)")
	}
	return note("enforced")
}

func checkMetadata(ctx context.Context, e *ConformanceEnv) error {
	kept, _, err := probeMetadata(ctx, e)
	switch {
	case err != nil:
		return err
	case !kept:
		return errors.New("the metadata of a write was not returned by HeadObject (metadata: false); stage 2 cannot keep its wrapped key in the object")
	}
	return nil
}

func checkCopyReplacesMetadata(ctx context.Context, e *ConformanceEnv) error {
	kept, replaced, err := probeMetadata(ctx, e)
	switch {
	case err != nil:
		return err
	case !kept:
		return note("skipped: the store keeps no metadata")
	case !replaced:
		return errors.New("a copy onto itself with replaced metadata kept the old value or lost the object's bytes (copyReplacesMetadata: false)")
	}
	return nil
}

func checkPeerCopies(ctx context.Context, e *ConformanceEnv) error {
	copier, ok := e.Store.(BucketCopier)
	if !ok {
		return note("skipped: the client copies only inside its bucket")
	}
	endpoint, _ := copier.Location()
	var failed []string
	asked := 0
	for _, p := range e.Opts.Peers {
		source, ok := p.Store.(BucketCopier)
		if !ok {
			continue
		}
		if pe, _ := source.Location(); pe == "" || pe != endpoint {
			continue
		}
		ok, known, err := probePeerCopy(ctx, e, p)
		if err != nil {
			return err
		}
		if !known {
			continue
		}
		asked++
		if !ok {
			failed = append(failed, p.ID)
		}
	}
	switch {
	case len(failed) > 0:
		return fmt.Errorf("a server-side copy from %v failed: the reconciliation streams between them", failed)
	case asked == 0:
		return note("skipped: no other connection on this endpoint")
	}
	return nil
}

// AwaitConnections runs the start check of every connection at once, each repeated with backoff while it fails transiently
// (R-STOR-035), stores what each found (StartCheckOptions.Record) and returns when the replica may become ready (R-STOR-060,
// Q-STOR-242): the check passed on one connection without drain and every connection without drain has passed or failed
// transiently once, so it has no configuration error. A configuration error of a connection without drain is returned at once
// and stops the process; a draining connection's is logged and changes nothing else. A check that still fails transiently goes
// on after the return until it passes or ctx ends.
func AwaitConnections(ctx context.Context, conns *Connections, o StartCheckOptions) error {
	type outcome struct {
		c     *Connection
		err   error
		final bool
	}
	list := conns.List()
	results := make(chan outcome, 2*len(list))
	// the checks that go on after the return end with ctx, the replica's life
	bg, cancel := context.WithCancel(ctx)
	var ready atomic.Bool
	defer func() {
		if !ready.Load() {
			cancel() // a refused start stops the other checks; a ready replica keeps them
		}
	}()
	for _, c := range list {
		go func() {
			first := true
			err := await(bg, func(ctx context.Context) error {
				caps, err := CheckConnection(ctx, c, list, o)
				if err == nil {
					c.SetCapabilities(caps)
					if o.Record != nil {
						if rerr := o.Record(ctx, c.ID(), caps); rerr != nil {
							o.log().Warn("could not record the capabilities of a connection", "connection", c.ID(), "error", rerr)
						}
					}
					return nil
				}
				if first && (IsTransient(err) || errors.Is(err, ErrCircuitOpen)) && bg.Err() == nil {
					first = false
					results <- outcome{c: c, err: err}
				}
				return err
			}, o.log().With("connection", c.ID()), o.Await...)
			switch {
			case err == nil || bg.Err() != nil:
			case c.Drain():
				o.log().Error("the start check of a draining connection failed; it is used for nothing but its drain", "connection", c.ID(), "error", err)
			case ready.Load():
				o.log().Error("the start check of a connection failed after the replica became ready; the connection serves no copy until its configuration is fixed",
					"connection", c.ID(), "error", err)
			}
			results <- outcome{c: c, err: err, final: true}
		}()
	}
	passed, settled := map[string]bool{}, map[string]bool{}
	need := len(conns.Targets())
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case r := <-results:
			switch {
			case r.c.Drain():
			case r.final && r.err == nil:
				passed[r.c.ID()], settled[r.c.ID()] = true, true
			case r.final && bg.Err() == nil:
				return fmt.Errorf("object-store connection %s (%s/%s): %w", r.c.ID(), r.c.Bucket(), r.c.Prefix(), r.err)
			case !r.final:
				settled[r.c.ID()] = true
				o.log().Warn("the start check of a connection fails transiently; it is retried, and the replica serves from the others", "connection", r.c.ID(), "error", r.err)
			}
			if len(passed) > 0 && len(settled) >= need {
				ready.Store(true)
				return nil
			}
		}
	}
}

// RecordCapabilities stores what the start check found for a connection (storage_connections.capabilities, R-STOR-035).
func (s *Store) RecordCapabilities(ctx context.Context, conn string, caps Capabilities) error {
	data, err := json.Marshal(caps)
	if err != nil {
		return err
	}
	return s.recordCapabilities(ctx, conn, string(data))
}

// ComparableLocation is how the location rule compares an endpoint (R-STOR-050): with its scheme, lower case, without a
// trailing slash, the AWS endpoint of its region when empty. The S3 adapter's Location reports it.
func ComparableLocation(endpoint, region string) string {
	return config.ComparableEndpoint(endpoint, region)
}
