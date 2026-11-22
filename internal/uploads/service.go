package uploads

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/errs"
)

// maxBinding is the longest binding the table takes (the CHECK of migration 00054).
const maxBinding = 512

// maxParts is the most parts an S3 multipart upload has.
const maxParts = 10_000

// Scope is what a request must match for a session to be its session: the repository the client addressed, the binding the format
// started the session with (the OCI image name) and the principal. A session that differs in any of them does not exist for the
// request (R-STOR-011 step 1, Q-OCI-12).
type Scope struct {
	// Addressed is the id of the repository the client addressed.
	Addressed int64
	// Binding is the format's string, repeated by every request.
	Binding string
	// Principal is the id of the principal of the request, empty for an anonymous one.
	Principal string
}

// Session is a session as the host reads it.
type Session struct {
	ID uuid.UUID
	// Target is the id of the repository the session writes to.
	Target int64
	// Addressed is the id of the repository the client addressed.
	Addressed int64
	Binding   string
	Received  int64
	PartSize  int64
	State     State
}

// ObjectStore is the part of the object-store client the service uses.
type ObjectStore = blob.ObjectStore

// Options configure [New].
type Options struct {
	// DB runs the service's transactions on the main pool.
	DB blob.DB
	// Connections is the replica's ordered set of object-store connections: a session starts on the first writable one, records it
	// and stays on it (R-STOR-054).
	Connections *blob.Connections
	// Spool holds the segment of a part while it is sent.
	Spool *blob.Spool
	// Storage is the storage section: the maximum upload size, storage.uploads and the circuit breaker's open duration.
	Storage config.Storage
	// Metrics counts the sessions by how they ended (quader_storage_upload_sessions_total); nil counts nothing.
	Metrics *blob.Metrics
	Log     *slog.Logger
}

// Service is the session service of one replica. It holds no state of a session: every call reads the row.
type Service struct {
	rows    rows
	conns   *blob.Connections
	spool   *blob.Spool
	cfg     config.StorageUploads
	maxSize int64
	// retry is the Retry-After of a session whose connection this replica does not configure: the circuit's open duration, as for
	// one whose circuit is open (R-STOR-054).
	retry   time.Duration
	log     *slog.Logger
	metrics *blob.Metrics

	// Fault is a test hook called at named points of a request. A non-nil error ends the request at once as if the process had been
	// killed there: no release of the claim, no clean-up. The error is returned wrapped in [blob.ErrFault]. A hook may also block,
	// to hold a request at a point while others run.
	Fault func(point string) error
}

// New builds the service over PostgreSQL.
func New(o Options) (*Service, error) {
	if o.DB == nil || o.Connections == nil || o.Spool == nil {
		return nil, errors.New("uploads: the service needs a database, the object-store connections and a spool")
	}
	return newService(pgRows{db: o.DB}, o), nil
}

func newService(r rows, o Options) *Service {
	log := o.Log
	if log == nil {
		log = slog.Default()
	}
	retry := max(5*time.Second, (o.Storage.ObjectStore.CircuitBreaker.OpenDuration+time.Second-1)/time.Second*time.Second)
	return &Service{
		rows: r, conns: o.Connections, spool: o.Spool, cfg: o.Storage.Uploads, maxSize: int64(o.Storage.Upload.MaxSize), retry: retry,
		log: log, metrics: o.Metrics,
	}
}

// pin is the connection a session lives on (R-STOR-054): its multipart upload, its tails and the server-side copy of its finalize
// are there, because a multipart upload exists in one store only and a server-side copy stays inside one.
type pin struct {
	conn      *blob.Connection
	algorithm string // the payload checksum of its writes, empty for none
}

func pinOf(c *blob.Connection) pin {
	a := c.PayloadChecksum()
	if a == "none" {
		a = ""
	}
	return pin{conn: c, algorithm: a}
}

func (p pin) objects() ObjectStore { return p.conn.Objects() }

func (p pin) dataKey(id uuid.UUID) string {
	return blob.SessionKey(p.conn.Prefix(), id.String(), "data")
}

func (p pin) tailKey(id uuid.UUID, writer string) string {
	return blob.SessionKey(p.conn.Prefix(), id.String(), "tail-"+writer)
}

// checksumOf is the payload checksum of a segment written to the session's connection (R-STOR-006).
func (p pin) checksumOf(seg *blob.Spooled) blob.Checksum {
	switch p.algorithm {
	case "sha256":
		return blob.Checksum{Algorithm: "sha256", Value: bytes.Clone(seg.SHA256[:])}
	case "md5":
		return blob.Checksum{Algorithm: "md5", Value: bytes.Clone(seg.MD5[:])}
	}
	return blob.Checksum{}
}

// unreachable is the answer to a request of a session whose connection cannot be asked (R-STOR-054): 503 with the Retry-After of the
// connection's circuit; the session stays as it is, and a client that keeps retrying continues once the connection is back.
func unreachable(conn string, after time.Duration) error {
	return errs.E(errs.Unavailable, errs.CodeObjectStoreUnavail,
		"The object-store connection of this upload is unavailable; retry, or start the upload again.", errs.WithRetryAfter(after),
		errs.WithCause(fmt.Errorf("the connection %q of the upload session is not writable on this replica", conn)))
}

// connection is the connection of a session that a request may use now: configured on this replica and its circuit closed; else the
// 503 of unreachable.
func (s *Service) connection(r row) (pin, error) {
	c, ok := s.conns.Get(r.Conn)
	if !ok {
		return pin{}, unreachable(r.Conn, s.retry)
	}
	if after, rejecting := c.Rejecting(); rejecting {
		return pin{}, unreachable(r.Conn, after)
	}
	return pinOf(c), nil
}

// writable is the connection a request that holds the claim writes to: the session's, or, for a session that has not written an
// object yet whose connection is not writable, the first writable one, to which the session moves (R-STOR-054). r is updated.
func (s *Service) writable(ctx context.Context, r *row, writer string) (pin, error) {
	p, err := s.connection(*r)
	if r.Received != 0 || r.UploadID != "" || len(r.Tails) != 0 {
		// a session that has written stays where its objects are, also on a connection that drains meanwhile, and waits while that
		// connection cannot be asked
		return p, err
	}
	if err == nil && !p.conn.Drain() {
		return p, nil
	}
	next := s.conns.Writable()
	if len(next) == 0 {
		if err == nil {
			err = unreachable(r.Conn, s.retry) // its connection drains and no other takes uploads now
		}
		return pin{}, err
	}
	rctx, cancel := recordCtx(ctx)
	merr := s.rows.move(rctx, r.ID, writer, next[0].ID())
	cancel()
	if merr != nil {
		return pin{}, merr
	}
	s.log.Info("an upload session moves to another connection before its first write", "session", r.ID, "from", r.Conn, "to", next[0].ID())
	r.Conn = next[0].ID()
	return pinOf(next[0]), nil
}

// startConnection is the connection a session starts on: the first writable one, or, when none is writable, the first that takes
// uploads at all, which the session leaves at its first write if it is still not writable then (R-STOR-054). A start touches no store.
func (s *Service) startConnection() string {
	if w := s.conns.Writable(); len(w) > 0 {
		return w[0].ID()
	}
	if t := s.conns.Targets(); len(t) > 0 {
		return t[0].ID()
	}
	return s.conns.First().ID()
}

// PartSize is the size of the parts a session started now cuts the blob into (storage.uploads.part_size).
func (s *Service) PartSize() int64 { return int64(s.cfg.PartSize) }

// unknown is the answer for a session that does not exist for the request: absent, over, of another principal, repository or
// binding (OCI BLOB_UPLOAD_UNKNOWN). The cases are not told apart, so a session id tells a caller nothing.
func unknown() error {
	return errs.E(errs.NotFound, errs.CodeOCIBlobUploadUnknown, "The upload does not exist.")
}

// outOfOrder is the 416 of a chunk that is not at the received count or of a session another request holds.
func outOfOrder(detail string) error {
	return errs.E(errs.RangeNotSatisfiable, errs.CodeOCIBlobUploadInvalid, detail)
}

func digestInvalid() error {
	return errs.E(errs.Invalid, errs.CodeOCIDigestInvalid, "The digest of the content differs from the digest the request names.")
}

func (s *Service) fault(point string) error {
	if s.Fault == nil {
		return nil
	}
	if err := s.Fault(point); err != nil {
		return fmt.Errorf("%w at %s: %w", blob.ErrFault, point, err)
	}
	return nil
}

// canonicalPrincipal is the stored form of a principal id: the lower-case UUID text, empty for none.
func canonicalPrincipal(p string) (string, error) {
	if p == "" {
		return "", nil
	}
	u, err := uuid.Parse(p)
	if err != nil {
		return "", errs.E(errs.Internal, errs.CodeInternal, "The principal of the request has no valid id.", errs.WithCause(err))
	}
	return u.String(), nil
}

// matches reports that the row is a session of the scope.
func (r row) matches(sc Scope, principal string) bool {
	return r.AddressedID == sc.Addressed && r.Binding == sc.Binding && r.Principal == principal
}

// Start opens a session that writes to the repository target for the scope: it records the session (R-STOR-011 State) and nothing
// else; the multipart upload of the staging object is created with the first part. The decisions of who may start it are the host's.
func (s *Service) Start(ctx context.Context, target int64, sc Scope) (Session, error) {
	principal, err := canonicalPrincipal(sc.Principal)
	if err != nil {
		return Session{}, err
	}
	if len(sc.Binding) > maxBinding {
		return Session{}, errs.E(errs.Invalid, errs.CodeInvalid, "The binding of an upload is at most "+strconv.Itoa(maxBinding)+" bytes.")
	}
	hs, err := newHashes().marshal()
	if err != nil {
		return Session{}, errs.E(errs.Internal, errs.CodeInternal, "", errs.WithCause(err))
	}
	// A session costs a row and nothing in the bucket: its multipart upload is created with the first full part, so a session that
	// is started and never continued leaves nothing for the store to keep (Q-STOR-174).
	r := row{
		ID: uuid.New(), RepoID: target, AddressedID: sc.Addressed, Binding: sc.Binding, Principal: principal, HashState: hs,
		PartSize: s.PartSize(), State: StateOpen, Conn: s.startConnection(),
	}
	started, err := s.rows.insert(ctx, r, s.cfg.MaxOpenSessions)
	if err != nil {
		return Session{}, err
	}
	if !started {
		return Session{}, errs.E(errs.RateLimited, errs.CodeRateLimited, "Too many uploads are open for this caller on this repository; finish or cancel one, or wait for them to expire.",
			errs.WithRetryAfter(30*time.Second))
	}
	return sessionOf(r), nil
}

func sessionOf(r row) Session {
	return Session{
		ID: r.ID, Target: r.RepoID, Addressed: r.AddressedID, Binding: r.Binding, Received: r.Received, PartSize: r.PartSize, State: r.State,
	}
}

// lookup reads the session of the scope. A session that is absent, of another scope, over, or past its maximum age is unknown;
// the last is also set to aborted, so that the collection run removes what it staged (R-STOR-011 step 2).
func (s *Service) lookup(ctx context.Context, id uuid.UUID, sc Scope) (row, error) {
	principal, err := canonicalPrincipal(sc.Principal)
	if err != nil {
		return row{}, err
	}
	r, found, err := s.rows.get(ctx, id, s.cfg.SessionMaxAge)
	if err != nil {
		return row{}, err
	}
	if !found || !r.matches(sc, principal) {
		return row{}, unknown()
	}
	switch {
	case r.State == StateAborted:
		return row{}, unknown()
	case r.State == StateOpen && r.TooOld:
		live, _, err := s.rows.abort(ctx, id)
		if err != nil {
			return row{}, err
		}
		if live {
			s.metrics.UploadSession(blob.SessionExpired, 1)
		}
		return row{}, unknown()
	}
	return r, nil
}

// Lookup reads the session of the scope in any state but aborted, a finished one included: the host resolves the repository a
// session writes to from it before it decides what the caller may do, and a repeated closing request must find a session that is
// done. A session that is absent, of another scope, aborted or past its maximum age is unknown.
func (s *Service) Lookup(ctx context.Context, id uuid.UUID, sc Scope) (Session, error) {
	r, err := s.lookup(ctx, id, sc)
	if err != nil {
		return Session{}, err
	}
	return sessionOf(r), nil
}

// Get reads the session of the scope: the received count a client continues from (OCI GET of the upload location). A finished
// session does not exist any more for it.
func (s *Service) Get(ctx context.Context, id uuid.UUID, sc Scope) (Session, error) {
	r, err := s.lookup(ctx, id, sc)
	if err != nil {
		return Session{}, err
	}
	if r.State == StateDone {
		return Session{}, unknown()
	}
	return sessionOf(r), nil
}

// Abort ends the session of the scope and removes what it staged. A session that is done or over does not exist for the caller.
func (s *Service) Abort(ctx context.Context, id uuid.UUID, sc Scope) error {
	r, err := s.lookup(ctx, id, sc)
	if err != nil {
		return err
	}
	if r.State == StateDone {
		return unknown()
	}
	if err := s.fault("abort.read"); err != nil {
		return err
	}
	live, uploadID, err := s.rows.abort(ctx, id)
	if err != nil {
		return err
	}
	if !live {
		return unknown()
	}
	r.UploadID = uploadID // the upload as it was at the abort, not as it was when the row was read
	s.metrics.UploadSession(blob.SessionAborted, 1)
	s.endAndRemove(ctx, r)
	return nil
}

// cleanup removes the staging objects of a session that is over from its connection, with a context of its own because the request's
// may be what ended. A failure, and a connection that cannot be asked, is logged: the collection run removes what is left (R-STOR-011
// Expiry, R-STOR-054).
func (s *Service) cleanup(ctx context.Context, r row) bool {
	p, err := s.connection(r)
	if err != nil {
		s.log.Warn("the connection of an ended upload session cannot be asked; the collection run removes its objects", "session", r.ID, "connection", r.Conn)
		return false
	}
	cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	if err := blob.RemoveSession(cctx, p.objects(), p.conn.Prefix(), r.ID.String(), r.UploadID); err != nil {
		s.log.Warn("could not remove the staging objects of an upload session; the collection run will", "session", r.ID, "error", err)
		return false
	}
	return true
}

// endAndRemove removes what an ended session staged and, when that succeeded, its row: the request that ended it has nothing more to wait
// for, and a row left behind is work for the collection run and a place in the table (Q-STOR-176). A failed removal leaves the row for the run.
func (s *Service) endAndRemove(ctx context.Context, r row) {
	if !s.cleanup(ctx, r) {
		return
	}
	dctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	if err := s.rows.deleteCleaned(dctx, r.ID); err != nil {
		s.log.Warn("could not delete the row of an ended upload session; the collection run will", "session", r.ID, "error", err)
	}
}

// release gives a claim back after a request that recorded nothing more, with a context of its own because the request's may be
// what ended (a drain, R-STOR-004 step 8).
func (s *Service) release(ctx context.Context, id uuid.UUID, writer string) {
	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if err := s.rows.release(rctx, id, writer); err != nil {
		s.log.Warn("could not release the claim on an upload session; it expires with its lease", "session", id, "error", err)
	}
}
