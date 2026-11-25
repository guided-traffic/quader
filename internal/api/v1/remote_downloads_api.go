package v1

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/guided-traffic/quader/internal/api/edge"
	"github.com/guided-traffic/quader/internal/api/v1/gen"
	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/authz"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/catalog"
	"github.com/guided-traffic/quader/internal/httpserver"
	"github.com/guided-traffic/quader/internal/limits"
	"github.com/guided-traffic/quader/internal/limits/httplimit"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/mirror/discover"
	"github.com/guided-traffic/quader/internal/qname"
	"github.com/guided-traffic/quader/internal/remotecuration"
	"github.com/guided-traffic/quader/internal/remotecuration/catalogue"
	"github.com/guided-traffic/quader/internal/remotecuration/queue"
	"github.com/guided-traffic/quader/internal/repository"
)

// The download queue's operations (remote-curation.md R-CUR-010, R-CUR-012; work package R4 of docs/developer/remote-curation.md):
// the preview and the start of a download ahead of a client, the list of a repository's queue, one row, its retry and cancel, the
// retry of every failed row that matches, and a bulk with its cancel. Every operation that R-CUR-005 gives to "manage, or admin" is
// declared authenticated and decided here, as the mirror's run is (Q-API-199).

// RemoteDownloadsServices are the services of the download queue's operations; a nil Queue answers them with 503.
type RemoteDownloadsServices struct {
	// Queue is the queue's service.
	Queue *queue.Service
	// Decider decides whether the rules deliver a version (R-CUR-001); nil delivers everything (remotecuration.DeliverAll).
	Decider remotecuration.Decider
	// Limits charges a person's download of one version (R-CUR-010); nil charges nobody.
	Limits *limits.Engine
	// Settings are the bounds of a bulk (proxy.downloads).
	Settings config.ProxyDownloads
	// Principals names the person who started a row; nil names nobody.
	Principals LimitsPrincipals
	// Catalogue is the upstream catalogue, which counts a preview and lists a sub-section (R-CUR-010); nil counts nothing and refuses a
	// sub-section with 400 catalogue_not_offered.
	Catalogue *catalogue.Engine
	// Stored reads which versions a repository holds, for the preview's stored count; nil counts none.
	Stored StoredVersions
}

// downloadTarget is the remote repository an operation addresses.
type downloadTarget struct {
	ID  int64
	Key qname.Repo
	// Format is the repository's format.
	Format string
	// Upstream is the remote's upstream URL: the name a limit's sentence says (Q-CUR-175).
	Upstream string
	// RaisesLimit is the limit's accountRaisesLimit: the preset of Upstream says an account allows more, and the remote sends no
	// credential, stored or declared on this replica (Q-UI-1160).
	RaisesLimit bool
}

// downloadAccess resolves the request's decisions on the repository; 503 without a decision point.
func (s *Server) downloadAccess(ctx context.Context, key qname.Repo) (context.Context, *edge.Access, error) {
	ctx, acc, err := requestHolder(ctx, s.o.Repositories.Authz, key)
	if err != nil {
		return ctx, nil, err
	}
	if acc == nil {
		return ctx, nil, errNoDecisionPoint()
	}
	return ctx, acc, nil
}

// downloadReader is R-CUR-005's "see the queue": read on the repository; a repository hidden from the caller answers 404.
func downloadReader(ctx context.Context, acc *edge.Access, key qname.Repo) error {
	return acc.Decide(ctx, authz.Coordinate{Repository: key, Kind: authz.KindMetadata, Actions: []authz.Action{authz.Read}}).Err()
}

// downloadManager reports whether the caller holds manage on the repository (the roles Maintainer and Owner) or the system role
// admin (R-CUR-005).
func downloadManager(ctx context.Context, acc *edge.Access, key qname.Repo) bool {
	return acc.Decide(ctx, manageCoordinate(key)).Allowed()
}

// downloadPopulate is the person's populate right on one version (R-CUR-010, Q-CUR-27): read and the action of cacheFillAction, with
// the endpoint's cache_fill, asked without the session cookie's refusal: the POST needs the session's CSRF token, so it is an act the
// person meant, not a navigation or an image.
func downloadPopulate(ctx context.Context, acc *edge.Access, key qname.Repo, name, version string) bool {
	c := authz.Coordinate{
		Repository: key, Class: repository.Remote, Kind: authz.KindArtifact, Package: name, Version: version,
		Actions: []authz.Action{authz.Read},
	}
	return acc.MayPopulateAct(ctx, c).Allowed()
}

// downloadRemote loads the repository: 400 not_applicable for a local or virtual one.
func (s *Server) downloadRemote(ctx context.Context, key qname.Repo) (downloadTarget, error) {
	svc, err := s.repositoryService()
	if err != nil {
		return downloadTarget{}, err
	}
	st, err := svc.Get(ctx, key)
	if err != nil {
		return downloadTarget{}, err
	}
	if st.Repository.Class != repository.Remote {
		return downloadTarget{}, errs.E(errs.Invalid, errs.CodeNotApplicable,
			"Only a remote repository has a download queue; "+strconv.Quote(key.String())+" is a "+string(st.Repository.Class)+" repository.")
	}
	id, err := parseRepositoryID(st.Repository.ID)
	if err != nil {
		return downloadTarget{}, err
	}
	t := downloadTarget{ID: id, Key: key, Format: st.Repository.Format}
	if rs, ok := st.Repository.Remote(); ok {
		t.Upstream = rs.URL
		_, declared := svc.ConfiguredAccountFor(key.Project, key.Key)
		t.RaisesLimit = svc.AccountRaisesLimit(t.Format, rs.URL, declared || rs.AuthMode != repository.AuthNone)
	}
	return t, nil
}

// downloadsQueue is the queue's service; 503 without it.
func (s *Server) downloadsQueue() (*queue.Service, error) {
	if s.o.RemoteDownloads.Queue == nil {
		return nil, unavailable("The download queue")
	}
	return s.o.RemoteDownloads.Queue, nil
}

func (s *Server) downloadDecider() remotecuration.Decider {
	if d := s.o.RemoteDownloads.Decider; d != nil {
		return d
	}
	return remotecuration.DeliverAll{}
}

// downloadSetup is what every operation of the queue starts with: the queue, the decisions, read on the repository (R-CUR-005: who
// sees the queue; a hidden repository answers 404) and the remote repository. A write decides its own right after it.
func (s *Server) downloadSetup(ctx context.Context, key qname.Repo) (context.Context, *queue.Service, *edge.Access, downloadTarget, error) {
	svc, err := s.downloadsQueue()
	if err != nil {
		return ctx, nil, nil, downloadTarget{}, err
	}
	ctx, acc, err := s.downloadAccess(ctx, key)
	if err != nil {
		return ctx, nil, nil, downloadTarget{}, err
	}
	if err := downloadReader(ctx, acc, key); err != nil {
		return ctx, nil, nil, downloadTarget{}, err
	}
	t, err := s.downloadRemote(ctx, key)
	return ctx, svc, acc, t, err
}

// downloadRequest is the body of a preview and a start (R-CUR-010).
type downloadRequest struct {
	Level              string  `json:"level"`
	Name               string  `json:"name,omitempty"`
	Version            string  `json:"version,omitempty"`
	Section            string  `json:"section,omitempty"`
	MaxVersions        *int    `json:"maxVersions,omitempty"`
	MaxBytes           *int64  `json:"maxBytes,omitempty"`
	IncludePrereleases *bool   `json:"includePrereleases,omitempty"`
	FromVersion        *string `json:"fromVersion,omitempty"`
	ToVersion          *string `json:"toVersion,omitempty"`
	ToInclusive        *bool   `json:"toInclusive,omitempty"`
}

// downloadBounds are a bulk's bounds: the configuration's, lowered by the request, and a package's range of versions (R-CUR-010,
// Q-CUR-190): fromVersion and toVersion null for no bound on that side, in the format's canonical form once checked.
type downloadBounds struct {
	MaxVersions        int     `json:"maxVersions"`
	MaxBytes           int64   `json:"maxBytes"`
	IncludePrereleases bool    `json:"includePrereleases"`
	FromVersion        *string `json:"fromVersion"`
	ToVersion          *string `json:"toVersion"`
	ToInclusive        bool    `json:"toInclusive"`
}

// versionRange is the bounds' range of versions.
func (b downloadBounds) versionRange() queue.Range {
	var r queue.Range
	if b.FromVersion != nil {
		r.From = *b.FromVersion
	}
	if b.ToVersion != nil {
		r.To = *b.ToVersion
	}
	r.ToInclusive = b.ToInclusive
	return r
}

// boundsOf are the bounds of a stored bulk.
func boundsOf(b queue.Bulk) downloadBounds {
	return downloadBounds{
		MaxVersions: b.MaxVersions, MaxBytes: b.MaxBytes, IncludePrereleases: b.IncludePrereleases,
		FromVersion: optionalString(b.FromVersion), ToVersion: optionalString(b.ToVersion), ToInclusive: b.ToInclusive || b.ToVersion == "",
	}
}

func optionalString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// parseDownloadRequest reads and checks the body: the level's members present, the bounds no higher than the configuration's.
func (s *Server) parseDownloadRequest(w http.ResponseWriter, r *http.Request) (downloadRequest, downloadBounds, []byte, error) {
	var in downloadRequest
	body, err := readOptionalBody(w, r, s.bodyLimit())
	if err == nil && body == nil {
		err = errs.E(errs.Invalid, errs.CodeInvalid, "The request needs a body with the level of the download.")
	}
	if err == nil {
		err = requireJSON(r, MediaJSON)
	}
	if err == nil {
		err = StrictUnmarshal(body, &in)
	}
	if err != nil {
		return in, downloadBounds{}, nil, err
	}
	cfg := s.o.RemoteDownloads.Settings
	b := downloadBounds{
		MaxVersions: max(cfg.MaxVersionsPerBulk, 1), MaxBytes: max(int64(cfg.MaxBytesPerBulk), 1), IncludePrereleases: true, ToInclusive: true,
	}
	field := func(ptr, detail string) error {
		return errs.E(errs.Invalid, errs.CodeValidation, detail, errs.WithFields(errs.FieldError{Pointer: ptr, Code: errs.CodeValidation, Detail: detail}))
	}
	switch in.Level {
	case string(queue.LevelVersion):
		if in.Name == "" || in.Version == "" {
			return in, b, nil, field("/version", "A download of one version names the package and the version.")
		}
	case string(queue.LevelPackage):
		if in.Name == "" {
			return in, b, nil, field("/name", "A download of every version names the package.")
		}
	case string(queue.LevelSection):
		if in.Section == "" {
			return in, b, nil, field("/section", "A download of a sub-section names the section.")
		}
	default:
		return in, b, nil, field("/level", "The level is version, package or section.")
	}
	if in.MaxVersions != nil {
		if *in.MaxVersions < 1 || *in.MaxVersions > b.MaxVersions {
			return in, b, nil, field("/maxVersions", "maxVersions is 1 to "+strconv.Itoa(b.MaxVersions)+" (proxy.downloads.max_versions_per_bulk).")
		}
		b.MaxVersions = *in.MaxVersions
	}
	if in.MaxBytes != nil {
		if *in.MaxBytes < 1 || *in.MaxBytes > b.MaxBytes {
			return in, b, nil, field("/maxBytes", "maxBytes is 1 to "+strconv.FormatInt(b.MaxBytes, 10)+" (proxy.downloads.max_bytes_per_bulk).")
		}
		b.MaxBytes = *in.MaxBytes
	}
	if in.IncludePrereleases != nil {
		b.IncludePrereleases = *in.IncludePrereleases
	}
	if err := rangeSyntax(in, &b); err != nil {
		return in, b, nil, err
	}
	return in, b, body, nil
}

// maxVersionBound is the longest bound of a range, the longest version a row of the queue holds (R-CUR-011).
const maxVersionBound = 256

// rangeSyntax reads a package's range of versions (R-CUR-010, Q-CUR-190): a bound names a version of at most 256 bytes, and only a
// package's bulk has one. Whether the format ranks the bounds is checked against the repository's format (downloadRange).
func rangeSyntax(in downloadRequest, b *downloadBounds) error {
	for _, f := range []struct {
		ptr string
		v   *string
	}{{"/fromVersion", in.FromVersion}, {"/toVersion", in.ToVersion}} {
		if f.v == nil {
			continue
		}
		if in.Level != string(queue.LevelPackage) {
			return rangeField(errs.Invalid, errs.CodeValidation, f.ptr, errs.CodeValidation,
				"A range of versions bounds a download of every version of one package; a version or a sub-section has none.")
		}
		if v := strings.TrimSpace(*f.v); v == "" || len(v) > maxVersionBound {
			return rangeField(errs.Invalid, errs.CodeValidation, f.ptr, "out_of_range", "A bound of the range names a version of 1 to 256 bytes.")
		}
	}
	if in.FromVersion != nil {
		v := strings.TrimSpace(*in.FromVersion)
		b.FromVersion = &v
	}
	if in.ToVersion != nil {
		v := strings.TrimSpace(*in.ToVersion)
		b.ToVersion = &v
	}
	if in.ToInclusive != nil {
		b.ToInclusive = *in.ToInclusive
	}
	return nil
}

func rangeField(kind errs.Kind, code, ptr, fieldCode, detail string) error {
	return errs.E(kind, code, detail, errs.WithFields(errs.FieldError{Pointer: ptr, Code: fieldCode, Detail: detail}))
}

// versionOrder is the order a range of the format's versions is read by (Q-CUR-190): the registered format's view adapter, whose
// Compare and sort key order every list of the format, and its canon (R-CUR-006). ok is false for a format without a view adapter.
// The format host's planner builds the same order (formathost.rangeOrder).
func versionOrder(format string) (queue.Order, bool) {
	f, ok := formats.Lookup(format)
	if !ok || f.View() == nil {
		return queue.Order{}, false
	}
	o := queue.Order{View: f.View()}
	if c := f.Descriptor().VersionCanon; c != nil {
		o.Canon = c
	}
	return o, true
}

// downloadRange checks a package's range of versions in the repository's format and writes its bounds in the format's canonical form
// (R-CUR-010, R-CUR-006, Q-CUR-192): a bound the format does not rank (a string that is no version, an OCI tag such as latest) answers
// 422 invalid_version at its pointer; a range that holds no version answers 422 validation_failed at /toVersion with the field code
// out_of_range and a sentence. The order is the format's view adapter's,
// the order of every list of the format; a bound need not be a version the upstream has.
func downloadRange(format string, b *downloadBounds) error {
	if b.FromVersion == nil && b.ToVersion == nil {
		return nil
	}
	o, ok := versionOrder(format)
	if !ok {
		return rangeField(errs.Invalid, errs.CodeValidation, "/fromVersion", errs.CodeValidation,
			"The format "+format+" has no order of versions to bound a download by.")
	}
	for _, f := range []struct {
		ptr, name string
		v         **string
	}{{"/fromVersion", "fromVersion", &b.FromVersion}, {"/toVersion", "toVersion", &b.ToVersion}} {
		if *f.v == nil {
			continue
		}
		c, ok := o.Rank(**f.v)
		if !ok {
			return rangeField(errs.Unprocessable, errs.CodeInvalidVersion, f.ptr, errs.CodeInvalidVersion,
				f.name+" "+strconv.Quote(**f.v)+" is no version the format "+format+
					" orders: a range is bounded by versions, not by a tag such as latest or by a range expression.")
		}
		*f.v = &c
	}
	if r := b.versionRange(); r.Empty(o) {
		why := "lies below fromVersion " + r.From
		if o.View.Compare(r.From, r.To) == 0 {
			why = "equals fromVersion " + r.From + " and is excluded (toInclusive false)"
		}
		return rangeField(errs.Unprocessable, errs.CodeValidation, "/toVersion", "out_of_range",
			"toVersion "+r.To+" "+why+": the range holds no version.")
	}
	return nil
}

// mayStart decides who starts a download (R-CUR-005, R-CUR-010): one version the rules deliver, whoever may populate the repository;
// one version they do not deliver, a package and a section, the holders of manage and the administrator. charged reports a version a
// person starts by the populate right, which is charged to the person.
func (s *Server) mayStart(ctx context.Context, acc *edge.Access, t downloadTarget, in downloadRequest) (charged bool, err error) {
	if in.Level == string(queue.LevelVersion) && s.downloadDecider().Decide(ctx, strconv.FormatInt(t.ID, 10), in.Name, in.Version).Delivered {
		if downloadPopulate(ctx, acc, t.Key, in.Name, in.Version) {
			return true, nil
		}
		if downloadManager(ctx, acc, t.Key) {
			return true, nil
		}
		return false, errs.E(errs.Forbidden, errs.CodePopulateNotPermitted,
			"You may not fill the cache of "+strconv.Quote(t.Key.String())+": downloading ahead needs the right to populate it.")
	}
	if downloadManager(ctx, acc, t.Key) {
		return in.Level == string(queue.LevelVersion), nil
	}
	return false, errs.E(errs.Forbidden, errs.CodeForbidden,
		"Downloading every version, a sub-section, or a version this repository does not deliver needs manage on "+strconv.Quote(t.Key.String())+".")
}

// PreviewRemoteDownload implements POST …/downloads/preview (R-CUR-010).
func (s *Server) PreviewRemoteDownload(w http.ResponseWriter, r *http.Request, projectKey gen.ProjectKey, repoKey string) {
	key := qname.Repo{Project: projectKey, Key: repoKey}
	ctx, _, acc, t, err := s.downloadSetup(r.Context(), key)
	var in downloadRequest
	var b downloadBounds
	if err == nil {
		in, b, _, err = s.parseDownloadRequest(w, r)
	}
	if err == nil {
		_, err = s.mayStart(ctx, acc, t, in)
	}
	if err == nil {
		err = downloadRange(t.Format, &b)
	}
	var out map[string]any
	if err == nil {
		out, err = s.preview(ctx, t, in, b)
	}
	if err != nil {
		httpserver.Problem(w, r, errs.Wrap(err, "api.previewRemoteDownload"))
		return
	}
	writeJSON(w, r, http.StatusOK, out)
}

// sectionNotOffered answers a sub-section of an upstream whose catalogue lists no levels.
func sectionNotOffered(reason string) error {
	if reason == "" {
		reason = "The upstream of this repository lists no sub-sections; download a package or a version."
	}
	return errs.E(errs.Invalid, errs.CodeCatalogueNotOffered, reason)
}

// StartRemoteDownload implements POST …/downloads (R-CUR-010).
func (s *Server) StartRemoteDownload(w http.ResponseWriter, r *http.Request, projectKey gen.ProjectKey, repoKey string, _ gen.StartRemoteDownloadParams) {
	key := qname.Repo{Project: projectKey, Key: repoKey}
	ctx, svc, acc, t, err := s.downloadSetup(r.Context(), key)
	if err == nil && s.idem == nil {
		err = unavailable("The download queue")
	}
	var in downloadRequest
	var b downloadBounds
	var body []byte
	if err == nil {
		in, b, body, err = s.parseDownloadRequest(w, r)
	}
	var charged bool
	if err == nil {
		charged, err = s.mayStart(ctx, acc, t, in)
	}
	if err == nil {
		err = downloadRange(t.Format, &b)
	}
	if err == nil && in.Level == string(queue.LevelSection) {
		err = s.sectionOffered(ctx, t, in.Section)
	}
	var ticket []byte
	if err == nil && charged {
		ticket, err = s.downloadCharge(r, ctx, t)
	}
	if err != nil {
		httpserver.Problem(w, r, errs.Wrap(err, "api.startRemoteDownload"))
		return
	}
	err = s.idem.Do(w, r, body, IdempotentOp{Run: func(ctx context.Context, tx meta.Tx) (Outcome, error) {
		return s.startDownload(ctx, r, tx, svc, acc, t, in, b, ticket)
	}})
	if err != nil {
		httpserver.Problem(w, r, err)
	}
}

// downloadCharge takes step 9a for the caller's budget key at the remote (R-CUR-010, R-LIM-037): a refusal answers 429 and queues
// nothing; the ticket travels with the row to the worker.
func (s *Server) downloadCharge(r *http.Request, ctx context.Context, t downloadTarget) ([]byte, error) { //nolint:revive // the request carries the caller
	eng := s.o.RemoteDownloads.Limits
	if eng == nil {
		return nil, nil
	}
	admin, _ := s.holds(ctx, string(authz.ContentBypass))
	tk, ok, err := eng.StartDownload(ctx, httplimit.CallerOf(r, httpserver.PrincipalFrom(ctx), admin), t.Key, t.ID)
	if err != nil || !ok || !tk.Charged() {
		return nil, err
	}
	return json.Marshal(tk)
}

func (s *Server) startDownload(ctx context.Context, r *http.Request, tx meta.Tx, svc *queue.Service, acc *edge.Access, t downloadTarget,
	in downloadRequest, b downloadBounds, ticket []byte,
) (Outcome, error) {
	details := map[string]any{"level": in.Level, "subject": in.Name, "bounds": b}
	var out Outcome
	if in.Level == string(queue.LevelVersion) {
		details["version"] = in.Version
		row, err := svc.Add(ctx, tx, queue.NewRow{
			RepoID: t.ID, Name: in.Name, Version: in.Version, Origin: remotecuration.OriginPerson, StartedBy: callerID(ctx), Ticket: ticket,
		})
		if err != nil {
			return Outcome{}, s.downloadError(ctx, acc, t, err)
		}
		details["row"], details["count"] = row.ID, 1
		out = Outcome{
			Status: http.StatusAccepted, Body: map[string]any{"download": s.downloadView(ctx, acc, t, row)},
			Header: map[string]string{"Location": downloadsBase(t.Key) + "/downloads/" + row.ID},
		}
	} else {
		subject := in.Name
		if in.Level == string(queue.LevelSection) {
			subject = in.Section
		}
		details["subject"] = subject
		bulk, err := svc.StartBulk(ctx, tx, queue.BulkSpec{
			RepoID: t.ID, Level: queue.Level(in.Level), Subject: subject, MaxVersions: b.MaxVersions, MaxBytes: b.MaxBytes,
			IncludePrereleases: b.IncludePrereleases, Range: b.versionRange(), StartedBy: callerID(ctx),
		})
		if err != nil {
			return Outcome{}, err
		}
		details["bulk"], details["count"] = bulk.ID, 0
		out = Outcome{
			Status: http.StatusAccepted, Body: map[string]any{"bulk": s.bulkView(ctx, acc, t, bulk, nil)},
			Header: map[string]string{"Location": downloadsBase(t.Key) + "/download-bulks/" + bulk.ID},
		}
	}
	if err := s.downloadEvent(ctx, r, tx, t, audit.EventRemoteDownloadStarted, details); err != nil {
		return Outcome{}, err
	}
	return out, nil
}

func downloadsBase(key qname.Repo) string {
	return "/api/v1/projects/" + key.Project + "/repositories/" + key.Key
}

// downloadEvent records one of the queue's events in the operation's transaction (R-CUR-019).
func (s *Server) downloadEvent(ctx context.Context, r *http.Request, tx meta.Tx, t downloadTarget, typ string, details map[string]any) error {
	raw, err := json.Marshal(details)
	if err != nil {
		return err
	}
	return s.recorder().RecordTx(ctx, tx, audit.Event{
		Type: typ, Actor: ActorFrom(httpserver.PrincipalFrom(ctx)), Outcome: audit.Success, Source: auditSource(r),
		Subject: audit.Subject{Kind: "repository", ID: strconv.FormatInt(t.ID, 10), Name: t.Key.String()}, Repository: t.Key.String(), Details: raw,
	})
}

// downloadError maps the queue's refusals to the API's codes.
func (s *Server) downloadError(ctx context.Context, acc *edge.Access, t downloadTarget, err error) error {
	var qe *queue.QueuedError
	var fe *queue.FullError
	switch {
	case errors.As(err, &qe):
		return errs.E(errs.Conflict, errs.CodeAlreadyQueued,
			"A download of "+qe.Row.Name+" "+qe.Row.Version+" is "+string(qe.Row.State)+" in this repository's queue: row "+qe.Row.ID+".",
			errs.WithExtension("download", s.downloadView(ctx, acc, t, qe.Row)))
	case errors.As(err, &fe):
		return errs.E(errs.RateLimited, errs.CodeDownloadQueueFull,
			"The download queue of "+strconv.Quote(t.Key.String())+" holds "+strconv.Itoa(fe.Waiting)+" waiting downloads, as many as it may.",
			errs.WithRetryAfter(time.Minute), errs.WithExtension("waiting", fe.Waiting))
	case errors.Is(err, queue.ErrNotRetryable):
		return errs.E(errs.Conflict, errs.CodeNotRetryable, "Only a failed or cancelled download can be retried.")
	case errors.Is(err, queue.ErrNotFound):
		return errs.E(errs.NotFound, errs.CodeNotFound, "The download does not exist in this repository.")
	}
	return err
}

// downloadView is a row as the API answers it (R-CUR-012): startedBy names a person only to those who may set rules and to that
// person; delivery says whether the rules deliver the version.
func (s *Server) downloadView(ctx context.Context, acc *edge.Access, t downloadTarget, r queue.Row) map[string]any {
	out := map[string]any{
		"id": r.ID, "name": r.Name, "version": r.Version, "origin": r.Origin, "state": r.State, "outcome": nullString(string(r.Outcome)),
		"attempts": r.Attempts, "maxAttempts": r.MaxAttempts, "nextAttemptAt": nullTime(r.NotBefore),
		"startedBy": s.starter(ctx, acc, t, r.StartedBy), "bulk": nullString(r.BulkID), "request": nullString(r.RequestID),
		"files": r.Files, "stored": r.Stored, "present": r.Present, "refused": r.Refused, "bytes": r.Bytes, "occurrences": r.Occurrences,
		"failure": failureView(r.Failure), "createdAt": r.CreatedAt.UTC(), "startedAt": nullTime(r.StartedAt), "finishedAt": nullTime(r.FinishedAt),
	}
	d := s.downloadDecider().Decide(ctx, strconv.FormatInt(t.ID, 10), r.Name, r.Version)
	switch {
	case d.Delivered:
		out["delivery"] = "delivered"
	case d.Reason == remotecuration.ReasonBlocked:
		out["delivery"] = "blocked"
	default:
		out["delivery"] = "notAllowed"
	}
	return out
}

// starter names the person who started a row or a bulk to the holders of manage and to that person; to other readers both members
// are null (R-CUR-012). A row no person started has none.
func (s *Server) starter(ctx context.Context, acc *edge.Access, t downloadTarget, id string) any {
	if id == "" {
		return nil
	}
	if id != callerID(ctx) && !downloadManager(ctx, acc, t.Key) {
		return map[string]any{"id": nil, "name": nil}
	}
	var name any
	if p := s.o.RemoteDownloads.Principals; p != nil {
		if pr, err := p.ByID(ctx, id); err == nil {
			name = pr.Name
		}
	}
	return map[string]any{"id": id, "name": name}
}

func failureView(f *remotecuration.Failure) any {
	if f == nil {
		return nil
	}
	return map[string]any{
		"class": f.Class, "step": f.Step, "code": f.Code, "file": nullString(f.File), "address": nullString(f.Address), "target": nullString(f.Target),
		"status": nullInt(f.Status), "excerpt": nullString(f.Excerpt), "retryAfter": nullInt(f.RetryAfter), "headers": nullHeaders(f.Headers),
		"detail": f.Detail, "attempt": f.Attempt, "at": f.At.UTC(),
	}
}

func nullHeaders(h map[string]string) any {
	if len(h) == 0 {
		return nil
	}
	return h
}

// limitView is the upstream's rate limit that holds the repository's queue, or held it last, as the list answers it (R-CUR-012,
// Q-CUR-175): since when the upstream limits, until when no row starts, whether that time is the upstream's own, the waits in a row,
// the upstream's name ("Docker Hub", else its host) and its last answer in a failure's form; nil for none.
func limitView(t downloadTarget, l queue.Limit, found bool, now time.Time) any {
	if !found {
		return nil
	}
	return map[string]any{
		"since": l.Since.UTC(), "until": l.Until.UTC(), "holds": l.Holds(now), "hinted": l.Hinted, "waits": l.Waits,
		"upstream": upstreamName(t.Format, t.Upstream), "failure": failureView(&l.Failure), "accountRaisesLimit": t.RaisesLimit,
	}
}

func (s *Server) bulkView(ctx context.Context, acc *edge.Access, t downloadTarget, b queue.Bulk, rows map[string]int64) map[string]any {
	if rows == nil {
		rows = map[string]int64{}
	}
	origin := "person"
	if o := b.RowOrigin(); o != remotecuration.OriginBulk {
		origin = string(o)
	}
	return map[string]any{
		"id": b.ID, "level": b.Level, "subject": b.Subject, "origin": origin, "state": b.State,
		"bounds":        boundsOf(b),
		"versionsFound": b.VersionsFound, "versionsQueued": b.VersionsQueued, "failure": failureView(b.Failure),
		"startedBy": s.starter(ctx, acc, t, b.StartedBy), "createdAt": b.CreatedAt.UTC(), "finishedAt": nullTime(b.FinishedAt), "rows": rows,
	}
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullInt(n int) any {
	if n == 0 {
		return nil
	}
	return n
}

func nullTime(t *time.Time) any {
	if t == nil || t.IsZero() {
		return nil
	}
	return t.UTC()
}

// ListRemoteDownloads implements GET …/downloads (R-CUR-012).
func (s *Server) ListRemoteDownloads(w http.ResponseWriter, r *http.Request, projectKey gen.ProjectKey, repoKey string, _ gen.ListRemoteDownloadsParams) {
	key := qname.Repo{Project: projectKey, Key: repoKey}
	ctx, svc, acc, t, err := s.downloadSetup(r.Context(), key)
	var lq ListQuery
	var p map[string]string
	if err == nil {
		filters := []string{"state", "class", "origin", "bulk", "name"}
		lq, err = ParseList(r, ListSpec{Filters: filters}, s.o.API.Config)
		if err == nil {
			p, err = onceEach(r.URL.Query(), filters...)
		}
	}
	f := queue.Filter{RepoID: t.ID, Limit: lq.Limit + 1}
	if err == nil {
		f.State, f.Class, f.Origin, f.BulkID, f.Name = p["state"], p["class"], p["origin"], p["bulk"], p["name"]
		if lq.After != nil {
			f.After, err = downloadCursor(lq.After)
		}
	}
	var rows []queue.Row
	if err == nil {
		rows, err = svc.Reader().List(ctx, f)
	}
	var counts queue.Counts
	if err == nil {
		counts, err = svc.Reader().StateCounts(ctx, t.ID, f.BulkID)
	}
	var limit queue.Limit
	var limited bool
	if err == nil {
		limit, limited, err = svc.Limit(ctx, t.ID)
	}
	if err != nil {
		httpserver.Problem(w, r, errs.Wrap(err, "api.listRemoteDownloads"))
		return
	}
	var next *string
	if len(rows) > lq.Limit {
		rows = rows[:lq.Limit]
		last := rows[len(rows)-1]
		c := lq.EncodeCursor([]string{string(last.State), last.CreatedAt.UTC().Format(time.RFC3339Nano), last.ID})
		next = &c
		w.Header().Set("Link", "<"+nextURL(r, c)+`>; rel="next"`)
	}
	items := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		items = append(items, s.downloadView(ctx, acc, t, row))
	}
	writeJSON(w, r, http.StatusOK, map[string]any{
		"items": items, "nextCursor": next,
		"counts": map[string]int64{"waiting": counts.Waiting, "running": counts.Running, "failed": counts.Failed, "done": counts.Done},
		"limit":  limitView(t, limit, limited, time.Now()),
	})
}

// downloadCursor reads the keyset position of the list: the last row's state, creation time and ID.
func downloadCursor(key []string) (*queue.Row, error) {
	if len(key) != 3 {
		return nil, invalidCursor()
	}
	at, err := time.Parse(time.RFC3339Nano, key[1])
	if err != nil {
		return nil, invalidCursor()
	}
	return &queue.Row{State: remotecuration.State(key[0]), CreatedAt: at, ID: key[2]}, nil
}

// GetRemoteDownload implements GET …/downloads/{downloadId} (R-CUR-012).
func (s *Server) GetRemoteDownload(w http.ResponseWriter, r *http.Request, projectKey gen.ProjectKey, repoKey, downloadID string) {
	key := qname.Repo{Project: projectKey, Key: repoKey}
	ctx, svc, acc, t, err := s.downloadSetup(r.Context(), key)
	var row queue.Row
	if err == nil {
		var found bool
		row, found, err = svc.Reader().GetRow(ctx, downloadID, t.ID)
		if err == nil && !found {
			err = queue.ErrNotFound
		}
	}
	if err != nil {
		httpserver.Problem(w, r, errs.Wrap(s.downloadError(ctx, acc, t, err), "api.getRemoteDownload"))
		return
	}
	writeJSON(w, r, http.StatusOK, s.downloadView(ctx, acc, t, row))
}

// mayActOnRow is R-CUR-005's retry and cancel: the person who started the row, the holders of manage and the administrator; a row a
// probe or a client caused, who may download that version.
func (s *Server) mayActOnRow(ctx context.Context, acc *edge.Access, t downloadTarget, row queue.Row) error {
	switch {
	case row.StartedBy != "" && row.StartedBy == callerID(ctx):
		return nil
	case downloadManager(ctx, acc, t.Key):
		return nil
	case (row.Origin == remotecuration.OriginProbe || row.Origin == remotecuration.OriginClient) &&
		s.downloadDecider().Decide(ctx, strconv.FormatInt(t.ID, 10), row.Name, row.Version).Delivered &&
		downloadPopulate(ctx, acc, t.Key, row.Name, row.Version):
		return nil
	}
	return errs.E(errs.Forbidden, errs.CodeForbidden, "Only the person who started the download, or who manages "+strconv.Quote(t.Key.String())+", may change it.")
}

// rowAct runs a retry or a cancel of one row in an idempotent operation.
func (s *Server) rowAct(w http.ResponseWriter, r *http.Request, key qname.Repo, id, op string,
	act func(ctx context.Context, tx meta.Tx, svc *queue.Service, acc *edge.Access, t downloadTarget, row queue.Row) (queue.Row, string, error),
) {
	ctx, svc, acc, t, err := s.downloadSetup(r.Context(), key)
	if err == nil && s.idem == nil {
		err = unavailable("The download queue")
	}
	var row queue.Row
	if err == nil {
		var found bool
		row, found, err = svc.Reader().GetRow(ctx, id, t.ID)
		if err == nil && !found {
			err = queue.ErrNotFound
		}
	}
	if err == nil {
		err = s.mayActOnRow(ctx, acc, t, row)
	}
	if err != nil {
		httpserver.Problem(w, r, errs.Wrap(s.downloadError(ctx, acc, t, err), op))
		return
	}
	err = s.idem.Do(w, r, nil, IdempotentOp{Run: func(ctx context.Context, tx meta.Tx) (Outcome, error) {
		got, event, err := act(ctx, tx, svc, acc, t, row)
		if err != nil {
			return Outcome{}, s.downloadError(ctx, acc, t, err)
		}
		if event != "" {
			if err := s.downloadEvent(ctx, r, tx, t, event, map[string]any{"subject": got.Name, "version": got.Version, "row": got.ID, "count": 1}); err != nil {
				return Outcome{}, err
			}
		}
		return Outcome{Status: http.StatusOK, Body: s.downloadView(ctx, acc, t, got)}, nil
	}})
	if err != nil {
		httpserver.Problem(w, r, err)
	}
}

// RetryRemoteDownload implements POST …/downloads/{downloadId}/retry (R-CUR-012).
func (s *Server) RetryRemoteDownload(w http.ResponseWriter, r *http.Request, projectKey gen.ProjectKey, repoKey, downloadID string, _ gen.RetryRemoteDownloadParams) {
	s.rowAct(w, r, qname.Repo{Project: projectKey, Key: repoKey}, downloadID, "api.retryRemoteDownload",
		func(ctx context.Context, tx meta.Tx, svc *queue.Service, _ *edge.Access, t downloadTarget, row queue.Row) (queue.Row, string, error) {
			var ticket []byte
			if row.Origin == remotecuration.OriginClient {
				var err error
				if ticket, err = s.downloadCharge(r, ctx, t); err != nil {
					return queue.Row{}, "", err
				}
			}
			got, err := svc.Retry(ctx, tx, t.ID, row.ID, callerID(ctx), ticket)
			return got, audit.EventRemoteDownloadRetried, err
		})
}

// CancelRemoteDownload implements POST …/downloads/{downloadId}/cancel (R-CUR-012).
func (s *Server) CancelRemoteDownload(w http.ResponseWriter, r *http.Request, projectKey gen.ProjectKey, repoKey, downloadID string, _ gen.CancelRemoteDownloadParams) {
	s.rowAct(w, r, qname.Repo{Project: projectKey, Key: repoKey}, downloadID, "api.cancelRemoteDownload",
		func(ctx context.Context, tx meta.Tx, svc *queue.Service, _ *edge.Access, t downloadTarget, row queue.Row) (queue.Row, string, error) {
			if !row.State.Active() {
				return row, "", nil
			}
			got, err := svc.Cancel(ctx, tx, t.ID, row.ID)
			return got, audit.EventRemoteDownloadCancelled, err
		})
}

// downloadRetryRequest is the body of POST …/downloads/retry.
type downloadRetryRequest struct {
	Class string `json:"class,omitempty"`
	Bulk  string `json:"bulk,omitempty"`
}

// RetryRemoteDownloads implements POST …/downloads/retry (R-CUR-012).
func (s *Server) RetryRemoteDownloads(w http.ResponseWriter, r *http.Request, projectKey gen.ProjectKey, repoKey string, _ gen.RetryRemoteDownloadsParams) {
	key := qname.Repo{Project: projectKey, Key: repoKey}
	ctx, svc, acc, t, err := s.downloadSetup(r.Context(), key)
	if err == nil && s.idem == nil {
		err = unavailable("The download queue")
	}
	if err == nil && !downloadManager(ctx, acc, key) {
		err = errs.E(errs.Forbidden, errs.CodeForbidden, "Retrying the failed downloads of "+strconv.Quote(key.String())+" needs manage on it.")
	}
	var in downloadRetryRequest
	var body []byte
	if err == nil {
		body, err = readOptionalBody(w, r, s.bodyLimit())
	}
	if err == nil && body != nil {
		if err = requireJSON(r, MediaJSON); err == nil {
			err = StrictUnmarshal(body, &in)
		}
	}
	if err != nil {
		httpserver.Problem(w, r, errs.Wrap(err, "api.retryRemoteDownloads"))
		return
	}
	err = s.idem.Do(w, r, body, IdempotentOp{Run: func(ctx context.Context, tx meta.Tx) (Outcome, error) {
		n, err := svc.RetryMatching(ctx, tx, t.ID, in.Class, in.Bulk)
		if err != nil {
			return Outcome{}, s.downloadError(ctx, acc, t, err)
		}
		details := map[string]any{"count": n}
		if in.Bulk != "" {
			details["bulk"] = in.Bulk
		}
		if err := s.downloadEvent(ctx, r, tx, t, audit.EventRemoteDownloadRetried, details); err != nil {
			return Outcome{}, err
		}
		return Outcome{Status: http.StatusOK, Body: map[string]any{"retried": n}}, nil
	}})
	if err != nil {
		httpserver.Problem(w, r, err)
	}
}

// GetRemoteDownloadBulk implements GET …/download-bulks/{bulkId} (R-CUR-012).
func (s *Server) GetRemoteDownloadBulk(w http.ResponseWriter, r *http.Request, projectKey gen.ProjectKey, repoKey, bulkID string) {
	key := qname.Repo{Project: projectKey, Key: repoKey}
	ctx, svc, acc, t, err := s.downloadSetup(r.Context(), key)
	var b queue.Bulk
	var rows map[string]int64
	if err == nil {
		var found bool
		b, rows, found, err = svc.Reader().GetBulk(ctx, bulkID, t.ID)
		if err == nil && !found {
			err = errs.E(errs.NotFound, errs.CodeNotFound, "The bulk download does not exist in this repository.")
		}
	}
	if err != nil {
		httpserver.Problem(w, r, errs.Wrap(err, "api.getRemoteDownloadBulk"))
		return
	}
	writeJSON(w, r, http.StatusOK, s.bulkView(ctx, acc, t, b, rows))
}

// ListRemoteDownloadBulks implements GET …/download-bulks (R-CUR-012, Q-CUR-168): the repository's bulks newest first, so that a bulk
// whose expansion failed before it queued a row is seen.
func (s *Server) ListRemoteDownloadBulks(w http.ResponseWriter, r *http.Request, projectKey gen.ProjectKey, repoKey string, _ gen.ListRemoteDownloadBulksParams) {
	key := qname.Repo{Project: projectKey, Key: repoKey}
	ctx, svc, acc, t, err := s.downloadSetup(r.Context(), key)
	var lq ListQuery
	if err == nil {
		lq, err = ParseList(r, ListSpec{}, s.o.API.Config)
	}
	f := queue.BulkFilter{RepoID: t.ID, Limit: lq.Limit + 1}
	if err == nil && lq.After != nil {
		f.After, err = bulkCursor(lq.After)
	}
	var bulks []queue.Bulk
	var rows map[string]map[string]int64
	if err == nil {
		bulks, rows, err = svc.Reader().ListBulks(ctx, f)
	}
	if err != nil {
		httpserver.Problem(w, r, errs.Wrap(err, "api.listRemoteDownloadBulks"))
		return
	}
	var next *string
	if len(bulks) > lq.Limit {
		bulks = bulks[:lq.Limit]
		last := bulks[len(bulks)-1]
		c := lq.EncodeCursor([]string{last.CreatedAt.UTC().Format(time.RFC3339Nano), last.ID})
		next = &c
		w.Header().Set("Link", "<"+nextURL(r, c)+`>; rel="next"`)
	}
	items := make([]map[string]any, 0, len(bulks))
	for _, b := range bulks {
		items = append(items, s.bulkView(ctx, acc, t, b, rows[b.ID]))
	}
	writeJSON(w, r, http.StatusOK, map[string]any{"items": items, "nextCursor": next})
}

// bulkCursor reads the keyset position of the bulks' list: the last bulk's creation time and ID.
func bulkCursor(key []string) (*queue.Bulk, error) {
	if len(key) != 2 {
		return nil, invalidCursor()
	}
	at, err := time.Parse(time.RFC3339Nano, key[0])
	if err != nil {
		return nil, invalidCursor()
	}
	return &queue.Bulk{CreatedAt: at, ID: key[1]}, nil
}

// CancelRemoteDownloadBulk implements POST …/download-bulks/{bulkId}/cancel (R-CUR-012).
func (s *Server) CancelRemoteDownloadBulk(w http.ResponseWriter, r *http.Request, projectKey gen.ProjectKey, repoKey, bulkID string, _ gen.CancelRemoteDownloadBulkParams) {
	key := qname.Repo{Project: projectKey, Key: repoKey}
	ctx, svc, acc, t, err := s.downloadSetup(r.Context(), key)
	if err == nil && s.idem == nil {
		err = unavailable("The download queue")
	}
	var b queue.Bulk
	if err == nil {
		var found bool
		b, _, found, err = svc.Reader().GetBulk(ctx, bulkID, t.ID)
		if err == nil && !found {
			err = errs.E(errs.NotFound, errs.CodeNotFound, "The bulk download does not exist in this repository.")
		}
	}
	if err == nil && (b.StartedBy == "" || b.StartedBy != callerID(ctx)) && !downloadManager(ctx, acc, key) {
		err = errs.E(errs.Forbidden, errs.CodeForbidden, "Only the person who started the bulk, or who manages "+strconv.Quote(key.String())+", may cancel it.")
	}
	if err != nil {
		httpserver.Problem(w, r, errs.Wrap(err, "api.cancelRemoteDownloadBulk"))
		return
	}
	err = s.idem.Do(w, r, nil, IdempotentOp{Run: func(ctx context.Context, tx meta.Tx) (Outcome, error) {
		got, err := svc.CancelBulk(ctx, tx, t.ID, b.ID)
		if err != nil {
			return Outcome{}, s.downloadError(ctx, acc, t, err)
		}
		if err := s.downloadEvent(ctx, r, tx, t, audit.EventRemoteDownloadCancelled, map[string]any{"subject": got.Subject, "bulk": got.ID}); err != nil {
			return Outcome{}, err
		}
		return Outcome{Status: http.StatusOK, Body: s.bulkView(ctx, acc, t, got, nil)}, nil
	}})
	if err != nil {
		httpserver.Problem(w, r, err)
	}
}

// downloadCatalogueTarget is the catalogue's target of a download's repository: the person is the caller, whose budget a preview spends.
func (s *Server) downloadCatalogueTarget(ctx context.Context, t downloadTarget) (catalogue.Target, error) {
	svc, err := s.repositoryService()
	if err != nil {
		return catalogue.Target{}, err
	}
	st, err := svc.Get(ctx, t.Key)
	if err != nil {
		return catalogue.Target{}, err
	}
	rs, _ := st.Repository.Remote()
	return catalogue.Target{RepoID: t.ID, Repo: t.Key.String(), Format: t.Format, Upstream: rs.URL, Person: callerID(ctx)}, nil
}

// sectionOffered refuses a sub-section of an upstream whose catalogue lists no levels, and a level that is not spelled as one (R-CUR-010).
func (s *Server) sectionOffered(ctx context.Context, t downloadTarget, section string) error {
	eng := s.o.RemoteDownloads.Catalogue
	if eng == nil {
		return sectionNotOffered("")
	}
	if _, err := parseLevelParam(section); err != nil {
		return err
	}
	ct, err := s.downloadCatalogueTarget(ctx, t)
	if err != nil {
		return err
	}
	caps, err := eng.Caps(ctx, ct)
	if err != nil {
		return err
	}
	if len(caps.Levels) == 0 && caps.LevelsReason != "" {
		return sectionNotOffered(caps.LevelsReason)
	}
	return nil
}

// The codes of a preview's note (R-CUR-010, R-UI-214): what the counts leave open, which the screen says in its own words.
const (
	previewNoteSectionUncounted = "section_uncounted" // the upstream does not tell a sub-section's versions and sizes
	previewNoteSizesUnannounced = "sizes_unannounced" // the upstream announces no size; a size counts once the file is stored
	previewNoteMoreVersions     = "more_versions"     // the upstream lists more versions than one answer holds
	previewNoteSizeOnceStored   = "size_once_stored"  // without the catalogue: the version's size is known once it is stored
	previewNoteCountedOnExpand  = "counted_on_expand" // without the catalogue: the versions are counted when the bulk is expanded
)

// preview counts what a download ahead would queue (R-CUR-010): one version or a package by one catalogue call (Package) with a page of
// the bulk's maxVersions, a sub-section by its level's count where the upstream tells it. Without the catalogue a version is one version
// of unknown size and a package is not counted.
func (s *Server) preview(ctx context.Context, t downloadTarget, in downloadRequest, b downloadBounds) (map[string]any, error) {
	out := map[string]any{"bounds": b, "stored": 0, "notDelivered": 0, "sizeUnknown": 0, "notVersions": 0, "note": nil}
	repoID := strconv.FormatInt(t.ID, 10)
	eng := s.o.RemoteDownloads.Catalogue
	if in.Level == string(queue.LevelSection) {
		if err := s.sectionOffered(ctx, t, in.Section); err != nil {
			return nil, err
		}
		level, _ := parseLevelParam(in.Section)
		ct, err := s.downloadCatalogueTarget(ctx, t)
		if err != nil {
			return nil, err
		}
		page, err := eng.Children(ctx, ct, discover.LevelRequest{Level: level, Limit: 1})
		if err != nil {
			return nil, err
		}
		out["versions"], out["bytes"], out["packages"] = nil, nil, nil
		if page.Total >= 0 {
			out["packages"] = page.Total
		}
		out["note"] = previewNoteSectionUncounted
		return out, nil
	}
	if eng == nil {
		return previewWithoutCatalogue(ctx, s, repoID, in, out), nil
	}
	ct, err := s.downloadCatalogueTarget(ctx, t)
	if err != nil {
		return nil, err
	}
	var info discover.PackageInfo
	var picked []discover.VersionFacts
	notVersions := 0
	if in.Level == string(queue.LevelVersion) {
		info, picked, err = previewVersion(ctx, eng, ct, t.Format, in, b.MaxVersions)
	} else {
		info, picked, notVersions, err = previewPackage(ctx, eng, ct, t.Format, in.Name, b)
	}
	if err != nil {
		return nil, err
	}
	out["notVersions"] = notVersions
	var stored map[string][]string
	if s.o.RemoteDownloads.Stored != nil {
		if stored, err = s.o.RemoteDownloads.Stored.Versions(ctx, t.ID, []string{info.Name}); err != nil {
			return nil, err
		}
	}
	var bytes int64
	unknown, held, refused := 0, 0, 0
	for _, v := range picked {
		if v.Size > 0 {
			bytes += v.Size
		} else {
			unknown++
		}
		if slices.Contains(stored[info.Name], v.Version) {
			held++
		}
		if !s.downloadDecider().Decide(ctx, repoID, info.Name, v.Version).Delivered {
			refused++
		}
	}
	out["versions"], out["packages"], out["sizeUnknown"], out["stored"], out["notDelivered"] = len(picked), 1, unknown, held, refused
	switch {
	case unknown == len(picked):
		out["bytes"] = nil
		out["note"] = previewNoteSizesUnannounced
	default:
		out["bytes"] = bytes
	}
	if !info.Done && in.Level == string(queue.LevelPackage) && len(picked) < b.MaxVersions {
		out["note"] = previewNoteMoreVersions
	}
	return out, nil
}

// previewVersion is the version a preview of one version names, by one catalogue call of a page of limit filtered by the version.
func previewVersion(ctx context.Context, eng *catalogue.Engine, ct catalogue.Target, format string, in downloadRequest, limit int) (
	discover.PackageInfo, []discover.VersionFacts, error,
) {
	// The filter is a substring: the exact version is picked below.
	info, err := eng.Versions(ctx, ct, in.Name, discover.VersionsRequest{Limit: limit, Filter: in.Version})
	if err != nil {
		return info, nil, err
	}
	cv, ok := catalog.Default().CanonicalVersion(format, in.Version)
	var picked []discover.VersionFacts
	for _, v := range info.Versions {
		if v.Version == in.Version || (ok && v.Version == cv) {
			picked = append(picked, v)
		}
	}
	if len(picked) == 0 {
		return info, nil, errs.E(errs.NotFound, errs.CodeCatalogueNotFound, "The upstream does not list "+in.Name+" "+in.Version+".")
	}
	return info, picked, nil
}

// The pages a preview of a package with a range reads (Q-CUR-191): the versions of a range may lie past the newest maxVersions, so the
// preview pages on, at least previewRangePage versions a page and at most previewRangePages pages; without a range it reads the one
// page of the bulk's maxVersions (Q-CUR-65).
const (
	previewRangePage  = 1000
	previewRangePages = 10
)

// previewPackage counts the versions a package's bulk takes, as its planner takes them (queue.Picker, Q-CUR-191): the range, then the
// pre-releases, then the newest maxVersions. info is the last page read; Done says whether the preview read to the list's end.
// notVersions counts the strings the range left out because the format does not rank them (Q-CUR-192).
func previewPackage(ctx context.Context, eng *catalogue.Engine, ct catalogue.Target, format, name string, b downloadBounds) (
	info discover.PackageInfo, picked []discover.VersionFacts, notVersions int, err error,
) {
	pick := queue.Picker{Selection: queue.Selection{Range: b.versionRange(), IncludePrereleases: b.IncludePrereleases, Max: b.MaxVersions}}
	rq := discover.VersionsRequest{Limit: b.MaxVersions}
	pages := 1
	if pick.Range.Set() {
		pick.Order, _ = versionOrder(format) // downloadRange found it
		rq.Limit, pages = max(b.MaxVersions, previewRangePage), previewRangePages
	}
	for range pages {
		if info, err = eng.Versions(ctx, ct, name, rq); err != nil {
			return info, nil, 0, err
		}
		for _, v := range info.Versions {
			take, full := pick.Take(v.Version, slices.Contains(v.Flags, "prerelease"))
			if full {
				info.Done = true // the bound is reached: what follows is not the bulk's
				return info, picked, pick.NotVersions, nil
			}
			if take {
				picked = append(picked, v)
			}
		}
		if info.Done || len(info.Cursor) == 0 {
			break
		}
		rq.Cursor = info.Cursor
	}
	return info, picked, pick.NotVersions, nil
}

// previewWithoutCatalogue is the preview of a process without the catalogue: a version of unknown size, a package not counted.
func previewWithoutCatalogue(ctx context.Context, s *Server, repoID string, in downloadRequest, out map[string]any) map[string]any {
	if in.Level == string(queue.LevelVersion) {
		out["versions"], out["packages"], out["bytes"], out["sizeUnknown"] = 1, 1, nil, 1
		if !s.downloadDecider().Decide(ctx, repoID, in.Name, in.Version).Delivered {
			out["notDelivered"] = 1
		}
		out["note"] = previewNoteSizeOnceStored
		return out
	}
	out["versions"], out["packages"], out["bytes"] = nil, 1, nil
	out["note"] = previewNoteCountedOnExpand
	return out
}
