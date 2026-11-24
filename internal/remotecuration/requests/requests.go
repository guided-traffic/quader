// Package requests is the requests for an allow of a remote repository (remote-curation.md R-CUR-015, Q-CUR-23): one entry per subject
// (a package, or a version of it) with everyone who asked and the refusals clients met, the examination a new entry starts, and the
// decisions: an allow writes the subject's rule (rules/store.PutTx) and starts the download, a decline keeps a reason and discards what
// the examination stored (retention), a withdrawal takes an asker out. Each act is one transaction with its audit event; the
// examination's row or bulk of the download queue commits with it.
//
// Who may do what is the API's (R-CUR-005); this package takes the caller's decision as given.
package requests

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats/catalog"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
	rc "github.com/guided-traffic/quader/internal/remotecuration"
	"github.com/guided-traffic/quader/internal/remotecuration/queue"
	"github.com/guided-traffic/quader/internal/remotecuration/retention"
	"github.com/guided-traffic/quader/internal/remotecuration/rules"
	"github.com/guided-traffic/quader/internal/remotecuration/rules/store"
	"github.com/guided-traffic/quader/internal/repository"
)

// The bounds of an entry's texts (the columns' CHECKs).
const (
	MaxNote   = 500
	MaxReason = 1000
)

// Settings are the bounds of proxy.delivery and the bulk bounds of proxy.downloads (R-CUR-020), read at every use.
type Settings struct {
	// OpenPerPerson is proxy.delivery.open_requests_per_person.
	OpenPerPerson int
	// ExamineVersions is proxy.delivery.examine_versions_per_request: the newest versions a package's request examines.
	ExamineVersions int
	// MaxVersionsPerBulk and MaxBytesPerBulk bound the bulk of an allow that downloads every version (proxy.downloads).
	MaxVersionsPerBulk int
	MaxBytesPerBulk    int64
}

// DefaultSettings are the configuration's defaults.
func DefaultSettings() Settings {
	return Settings{OpenPerPerson: 20, ExamineVersions: 1, MaxVersionsPerBulk: 10000, MaxBytesPerBulk: 100 << 30}
}

// Lookup checks that the upstream has a package (and a version) before an ask becomes an entry (R-CUR-015 "Asking": a typo cannot
// become a request): the upstream catalogue's lookup by exact name. found false answers 404 catalogue_not_found.
type Lookup interface {
	Exists(ctx context.Context, repoID int64, name, version string) (found bool, err error)
}

// Options configure [New].
type Options struct {
	Runner   meta.Runner
	Recorder audit.Recorder
	// Rules writes the subject's rule of an allow.
	Rules *store.Store
	// Queue takes the examination's and the allow's downloads.
	Queue *queue.Service
	// Retention discards what a declined entry's examination stored.
	Retention *retention.Service
	// Catalog folds names and versions; nil is catalog.Default.
	Catalog *catalog.Registry
	// Settings reads the bounds; nil is DefaultSettings.
	Settings func() Settings
	// Lookup is the catalogue's lookup; nil asks nothing (Q-CUR-145: until the catalogue is wired, the examination's download says
	// whether the upstream has the subject).
	Lookup Lookup
}

// Service is the requests' service.
type Service struct {
	o Options
}

var _ rc.Requests = (*Service)(nil)

// New returns the service.
func New(o Options) (*Service, error) {
	if o.Runner == nil || o.Recorder == nil || o.Rules == nil || o.Queue == nil || o.Retention == nil {
		return nil, errors.New("requests: the service needs a runner, a recorder, the rules' store, the queue and the retention")
	}
	o.Catalog = catalog.Or(o.Catalog)
	if o.Settings == nil {
		o.Settings = DefaultSettings
	}
	return &Service{o: o}, nil
}

// Entry is a request entry (remote_delivery_requests).
type Entry struct {
	ID                 string          `json:"id"`
	RepoID             int64           `json:"repo_id"`
	Name               string          `json:"name"`
	Version            *string         `json:"version"`
	State              rc.RequestState `json:"state"`
	Askers             int             `json:"askers"`
	Refusals           int64           `json:"refusals"`
	FirstAt            time.Time       `json:"first_at"`
	LastAt             time.Time       `json:"last_at"`
	DecidedBy          *string         `json:"decided_by"`
	DecidedAt          *time.Time      `json:"decided_at"`
	DeclineReason      *string         `json:"decline_reason"`
	ExaminationRefused *string         `json:"examination_refused"`
}

// VersionText is the entry's version, "" for the package with its future versions.
func (e Entry) VersionText() string {
	if e.Version == nil {
		return ""
	}
	return *e.Version
}

func decodeEntry(doc string) (Entry, error) {
	var e Entry
	if err := json.Unmarshal([]byte(doc), &e); err != nil {
		return Entry{}, errors.New("requests: an entry does not decode: " + err.Error())
	}
	return e, nil
}

func firstEntry(docs []string, err error) (Entry, bool, error) {
	if err != nil || len(docs) == 0 {
		return Entry{}, false, err
	}
	e, err := decodeEntry(docs[0])
	return e, err == nil, err
}

func queries(tx meta.Tx) (*q.Queries, error) {
	ptx, ok := postgres.Unwrap(tx)
	if !ok {
		return nil, errors.New("requests: the transaction is not one of internal/meta/postgres")
	}
	return q.New(ptx), nil
}

// Target is the remote repository an act addresses, from the replica's snapshot.
type Target struct {
	Repo repository.Repository
}

func (t Target) id() int64 { return t.Repo.NumericID() }

func (t Target) rulesTarget() store.Target {
	return store.Target{ID: t.Repo.NumericID(), Name: t.Repo.Name, Format: t.Repo.Format, Mode: t.Repo.Delivery().Mode()}
}

func (t Target) examineOnRequest() bool {
	st, ok := t.Repo.Remote()
	return ok && st.Delivery.ExamineOnRequest
}

// Caller is who acts.
type Caller struct {
	Actor  audit.Actor
	Source audit.Source
	// PrincipalID is the principal's ID; "" for none (a break-glass administrator, who cannot ask).
	PrincipalID string
}

func (c Caller) rules() store.Caller {
	return store.Caller{Actor: c.Actor, Source: c.Source, PrincipalID: c.PrincipalID}
}

// Charge takes the asker's charge of an examination (R-CUR-010: step 9a of the person's budget key at the remote) and answers the
// ticket the queue's rows carry; an error is the budget's refusal, and the examination does not start. Nil charges nobody.
type Charge func(ctx context.Context) (ticket []byte, err error)

// AskInput is an ask (POST delivery-requests).
type AskInput struct {
	Name, Version, Note string
}

// Asked is the answer of an ask: the entry, whether the ask opened it or joined it, and what started for its examination.
type Asked struct {
	Entry Entry
	// Created reports that the caller is a new asker of the entry (an ask of someone who asks in it already changes nothing).
	Created bool
	// Row is the examination's row of a version; Bulk the examination's bulk of a package; both zero when none started.
	Row  *queue.Row
	Bulk *queue.Bulk
}

// canonical folds a subject by the format's rules (R-CUR-006): 422 invalid_name or invalid_version.
func (s *Service) canonical(format, name, version string) (string, string, error) {
	n, ok := s.o.Catalog.NormalizeName(format, name)
	if !ok || name == "" || strings.ContainsAny(n, "*?[]{}") || len(n) > rules.MaxNameLength {
		return "", "", errs.E(errs.Unprocessable, errs.CodeInvalidName, "The name is no package name of the format.",
			errs.WithFields(errs.FieldError{Pointer: "/name", Code: errs.CodeInvalidName, Detail: "the name is no package name of the format"}))
	}
	if version == "" {
		return n, "", nil
	}
	v, ok := s.o.Catalog.CanonicalVersion(format, version)
	if !ok || len(v) > rules.MaxVersionLength {
		return "", "", errs.E(errs.Unprocessable, errs.CodeInvalidVersion, "The version is not one the format can read; a range is not a version.",
			errs.WithFields(errs.FieldError{Pointer: "/version", Code: errs.CodeInvalidVersion, Detail: "the version is not one the format can read"}))
	}
	return n, v, nil
}

// Subject is what an ask names, in the format's canonical forms; Version "" is the package with its future versions.
type Subject struct {
	Name, Version string
}

// Check reads an ask before it is written (R-CUR-015 "Asking"): the note's bound, the subject in the format's canonical forms (422
// invalid_name, invalid_version), a subject the rules deliver (409 already_delivered), and the catalogue's lookup when one is wired
// (404 catalogue_not_found). It holds no transaction, so the lookup's upstream call holds none either.
func (s *Service) Check(ctx context.Context, t Target, c Caller, in AskInput) (Subject, error) {
	if c.PrincipalID == "" {
		return Subject{}, errs.E(errs.Forbidden, errs.CodeForbidden, "Only a person or a service account can ask for an allow.")
	}
	if utf8.RuneCountInString(in.Note) > MaxNote {
		return Subject{}, errs.E(errs.Invalid, errs.CodeValidation, "The note has at most 500 characters.",
			errs.WithFields(errs.FieldError{Pointer: "/note", Code: "out_of_range", Detail: "at most 500 characters"}))
	}
	name, version, err := s.canonical(t.Repo.Format, in.Name, in.Version)
	if err != nil {
		return Subject{}, err
	}
	if t.Repo.Delivery().Decide(name, version).Delivered {
		return Subject{}, errs.E(errs.Conflict, errs.CodeAlreadyDelivered,
			strings.TrimSpace(name+" "+version)+" is delivered by "+t.Repo.Name.String()+"; there is nothing to ask for.")
	}
	if s.o.Lookup != nil {
		found, err := s.o.Lookup.Exists(ctx, t.id(), name, version)
		if err != nil {
			return Subject{}, err
		}
		if !found {
			return Subject{}, errs.E(errs.NotFound, errs.CodeCatalogueNotFound,
				"The upstream of "+t.Repo.Name.String()+" has no "+strings.TrimSpace(name+" "+version)+".")
		}
	}
	return Subject{Name: name, Version: version}, nil
}

// Ask is [Service.Check] and [Service.AskTx] in a transaction of its own.
func (s *Service) Ask(ctx context.Context, t Target, c Caller, in AskInput, charge Charge) (Asked, error) {
	sub, err := s.Check(ctx, t, c, in)
	if err != nil {
		return Asked{}, err
	}
	var out Asked
	err = s.o.Runner.InTx(ctx, func(tx meta.Tx) error {
		var err error
		out, err = s.AskTx(ctx, tx, t, c, sub, in.Note, charge)
		return err
	})
	return out, err
}

// AskTx records a person's ask for an allow of a checked subject (R-CUR-015): refused when the person holds
// proxy.delivery.open_requests_per_person open entries in the repository (429 too_many_requests); one open entry per subject, the
// person added to it. The first person of an entry starts its examination when the repository's remote.delivery.examineOnRequest is
// on: a row of origin request of the version asked, or a bulk of the newest versions of a package (Q-CUR-141), charged to the asker by
// charge. A refusal by the asker's budget, or by the bound of waiting rows, starts nothing and the entry says why (examination_refused).
func (s *Service) AskTx(ctx context.Context, tx meta.Tx, t Target, c Caller, sub Subject, note string, charge Charge) (Asked, error) {
	qs, err := queries(tx)
	if err != nil {
		return Asked{}, err
	}
	name, version := sub.Name, sub.Version
	e, err := s.openEntry(ctx, qs, t.id(), name, version)
	if err != nil {
		return Asked{}, err
	}
	asks, err := qs.DeliveryRequestAsker(ctx, q.DeliveryRequestAskerParams{RequestID: e.ID, Principal: c.PrincipalID})
	if err != nil {
		return Asked{}, err
	}
	if len(asks) == 1 && !asks[0] {
		return Asked{Entry: e}, nil // the person asks in it already
	}
	open, err := qs.DeliveryRequestOpenOfPerson(ctx, q.DeliveryRequestOpenOfPersonParams{Principal: c.PrincipalID, RepoID: t.id()})
	if err != nil {
		return Asked{}, err
	}
	if bound := max(s.o.Settings().OpenPerPerson, 1); open >= int64(bound) {
		return Asked{}, errs.E(errs.RateLimited, errs.CodeTooManyRequests,
			"You hold "+strconv.Itoa(bound)+" open requests in "+t.Repo.Name.String()+", the most you may (proxy.delivery.open_requests_per_person); withdraw one first.",
			errs.WithRetryAfter(time.Hour))
	}
	first := e.Askers == 0
	if err := qs.DeliveryRequestAskerPut(ctx, q.DeliveryRequestAskerPutParams{RequestID: e.ID, Principal: c.PrincipalID, Note: note}); err != nil {
		return Asked{}, err
	}
	if e, err = s.countAskers(ctx, qs, e.ID, true); err != nil {
		return Asked{}, err
	}
	out := Asked{Entry: e, Created: true}
	if err := s.event(ctx, tx, t, c, audit.EventRemoteRequestCreated, e, ""); err != nil {
		return Asked{}, err
	}
	if !first || !t.examineOnRequest() {
		return out, nil
	}
	if err := s.examine(ctx, tx, qs, t, c, &out, charge); err != nil {
		return Asked{}, err
	}
	return out, nil
}

// openEntry is the subject's open entry, opened when there is none.
func (s *Service) openEntry(ctx context.Context, qs *q.Queries, repoID int64, name, version string) (Entry, error) {
	for range 2 {
		e, found, err := firstEntry(qs.DeliveryRequestOpenLock(ctx, q.DeliveryRequestOpenLockParams{RepoID: repoID, Name: name, Version: version}))
		if err != nil || found {
			return e, err
		}
		e, found, err = firstEntry(qs.DeliveryRequestInsert(ctx, q.DeliveryRequestInsertParams{
			ID: uuid.Must(uuid.NewV7()).String(), RepoID: repoID, Name: name, Version: version,
		}))
		if err != nil || found {
			return e, err
		}
	}
	return Entry{}, errs.E(errs.Unavailable, errs.CodeUnavailable, "The request entry changed while it was written; ask again.", errs.WithRetryAfter(time.Second))
}

func (s *Service) countAskers(ctx context.Context, qs *q.Queries, id string, touch bool) (Entry, error) {
	doc, err := qs.DeliveryRequestCountAskers(ctx, q.DeliveryRequestCountAskersParams{ID: id, Touch: touch})
	if err != nil {
		return Entry{}, err
	}
	return decodeEntry(doc)
}

// examine starts the examination of a new entry (R-CUR-015 "Examination at once", Q-CUR-23).
func (s *Service) examine(ctx context.Context, tx meta.Tx, qs *q.Queries, t Target, c Caller, out *Asked, charge Charge) error {
	e := out.Entry
	refuse := func(reason string) error {
		out.Entry.ExaminationRefused = &reason
		return qs.DeliveryRequestSetExaminationRefused(ctx, q.DeliveryRequestSetExaminationRefusedParams{ID: e.ID, Reason: reason})
	}
	var ticket []byte
	if charge != nil {
		tk, err := charge(ctx)
		if err != nil {
			if ek := errs.KindOf(err); ek == errs.RateLimited || ek == errs.Forbidden {
				detail := ""
				if pe, ok := errs.As(err); ok {
					detail = pe.Detail
				}
				return refuse(cut(detail, MaxReason))
			}
			return err
		}
		ticket = tk
	}
	if v := e.VersionText(); v != "" {
		row, err := s.o.Queue.Add(ctx, tx, queue.NewRow{
			RepoID: t.id(), Name: e.Name, Version: v, Origin: rc.OriginRequest, RequestID: e.ID, StartedBy: c.PrincipalID, Ticket: ticket,
		})
		var qe *queue.QueuedError
		var fe *queue.FullError
		switch {
		case errors.As(err, &qe):
			return nil // a download of the version waits or runs already: it is fetched once
		case errors.As(err, &fe):
			return refuse("The download queue of " + t.Repo.Name.String() + " holds " + strconv.Itoa(fe.Waiting) + " waiting downloads, as many as it may.")
		case err != nil:
			return err
		}
		out.Row = &row
		return nil
	}
	st := s.o.Settings()
	b, err := s.o.Queue.StartBulk(ctx, tx, queue.BulkSpec{
		RepoID: t.id(), Level: queue.LevelPackage, Subject: e.Name, MaxVersions: max(st.ExamineVersions, 1), MaxBytes: max(st.MaxBytesPerBulk, 1),
		IncludePrereleases: false, StartedBy: c.PrincipalID, Origin: rc.OriginRequest, RequestID: e.ID, Ticket: ticket,
	})
	if err != nil {
		return err
	}
	out.Bulk = &b
	return nil
}

func cut(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		if s == "" {
			return "The examination was refused."
		}
		return s
	}
	r := []rune(s)
	return string(r[:n])
}

// locked reads an entry of the repository for a change; 404 for none.
func locked(ctx context.Context, qs *q.Queries, repoID int64, id string) (Entry, error) {
	if _, err := uuid.Parse(id); err != nil {
		return Entry{}, notFound()
	}
	e, found, err := firstEntry(qs.DeliveryRequestLock(ctx, q.DeliveryRequestLockParams{RepoID: repoID, ID: id}))
	if err != nil {
		return Entry{}, err
	}
	if !found {
		return Entry{}, notFound()
	}
	return e, nil
}

func notFound() error {
	return errs.E(errs.NotFound, errs.CodeNotFound, "The repository holds no request of this ID.")
}

func notOpen(e Entry) error {
	return errs.E(errs.Conflict, errs.CodeConflict, "The request is "+string(e.State)+"; only an open request is decided.")
}

// AllowInput is an allow (POST delivery-requests/{id}/allow).
type AllowInput struct {
	// AsPackage widens a version's entry to its package (as: package).
	AsPackage bool
	// DownloadEveryVersion downloads every version of the package with a bulk (R-CUR-010).
	DownloadEveryVersion bool
	Note                 string
}

// Allowed is the answer of an allow: the entry, the rule written, and what it started.
type Allowed struct {
	Entry Entry
	Rule  store.Written
	// Rows are the rows started or started again; Bulk the bulk of a package's newest version or of every version.
	Rows []queue.Row
	Bulk *queue.Bulk
}

// Allow is [Service.AllowTx] in a transaction of its own.
func (s *Service) Allow(ctx context.Context, t Target, c Caller, id string, in AllowInput) (Allowed, error) {
	var out Allowed
	err := s.o.Runner.InTx(ctx, func(tx meta.Tx) error {
		var err error
		out, err = s.AllowTx(ctx, tx, t, c, id, in)
		return err
	})
	return out, err
}

// AllowTx allows an open entry (R-CUR-015 "Allow"): the subject's rule (allow, the entry's ID) through the rules' store, the entry
// allowed, and the download at once: nothing more where the examination stored the version (it is delivered from the moment of the
// rule), else a row of origin allow of the version asked, or of a package a bulk of its newest version (Q-CUR-141); with
// downloadEveryVersion a bulk of every version. A row of the examination that failed or was cancelled is started again.
func (s *Service) AllowTx(ctx context.Context, tx meta.Tx, t Target, c Caller, id string, in AllowInput) (Allowed, error) {
	var out Allowed
	if utf8.RuneCountInString(in.Note) > rules.MaxNoteLength {
		return out, errs.E(errs.Invalid, errs.CodeValidation, "The note has at most 1,000 characters.",
			errs.WithFields(errs.FieldError{Pointer: "/note", Code: "out_of_range", Detail: "at most 1000 characters"}))
	}
	qs, err := queries(tx)
	if err != nil {
		return out, err
	}
	e, err := locked(ctx, qs, t.id(), id)
	if err != nil {
		return out, err
	}
	if e.State != rc.RequestOpen {
		return out, notOpen(e)
	}
	version := e.VersionText()
	if in.AsPackage {
		version = ""
	}
	// What the examination stored is read before the rule's write, which removes the rows it comes to deliver (R-CUR-016).
	exams, err := qs.RemoteExaminationsOf(ctx, q.RemoteExaminationsOfParams{RepoID: t.id(), Name: e.Name, Version: e.VersionText()})
	if err != nil {
		return out, err
	}
	kind := rc.KindVersion
	if version == "" {
		kind = rc.KindPackage
	}
	out.Rule, err = s.o.Rules.PutTx(ctx, tx, t.rulesTarget(), c.rules(), store.Input{
		Kind: kind, Name: e.Name, Version: version, Effect: rc.Allow, Note: in.Note, RequestID: e.ID,
	})
	if err != nil {
		return out, err
	}
	if err := qs.DeliveryRequestSetState(ctx, q.DeliveryRequestSetStateParams{ID: e.ID, State: string(rc.RequestAllowed), DecidedBy: c.PrincipalID}); err != nil {
		return out, err
	}
	e.State = rc.RequestAllowed
	if err := s.event(ctx, tx, t, c, audit.EventRemoteRequestAllowed, e, ""); err != nil {
		return out, err
	}
	if err := s.allowDownloads(ctx, tx, qs, t, c, e, len(exams) > 0, in, &out); err != nil {
		return out, err
	}
	out.Entry, err = getTx(ctx, qs, t.id(), e.ID)
	return out, err
}

// getTx reads an entry in the act's transaction.
func getTx(ctx context.Context, qs *q.Queries, repoID int64, id string) (Entry, error) {
	e, found, err := firstEntry(qs.DeliveryRequestGet(ctx, q.DeliveryRequestGetParams{RepoID: repoID, ID: id}))
	if err == nil && !found {
		err = notFound()
	}
	return e, err
}

// allowDownloads starts what an allow downloads (R-CUR-015 "An allow starts the download" holds in every case).
func (s *Service) allowDownloads(ctx context.Context, tx meta.Tx, qs *q.Queries, t Target, c Caller, e Entry, examined bool, in AllowInput, out *Allowed) error {
	docs, err := qs.DeliveryRequestRows(ctx, q.DeliveryRequestRowsParams{RepoID: t.id(), Ids: []string{e.ID}})
	if err != nil {
		return err
	}
	active := false
	for _, d := range docs {
		var r queue.Row
		if err := json.Unmarshal([]byte(d), &r); err != nil {
			return err
		}
		switch r.State {
		case rc.StateFailed, rc.StateCancelled:
			got, err := s.o.Queue.Retry(ctx, tx, t.id(), r.ID, c.PrincipalID, nil)
			var qe *queue.QueuedError
			if errors.As(err, &qe) || errors.Is(err, queue.ErrNotRetryable) {
				continue
			}
			if err != nil {
				return err
			}
			out.Rows = append(out.Rows, got)
			active = true
		case rc.StateWaiting, rc.StateRunning:
			active = true
		}
	}
	st := s.o.Settings()
	if in.DownloadEveryVersion {
		b, err := s.o.Queue.StartBulk(ctx, tx, queue.BulkSpec{
			RepoID: t.id(), Level: queue.LevelPackage, Subject: e.Name, MaxVersions: max(st.MaxVersionsPerBulk, 1),
			MaxBytes: max(st.MaxBytesPerBulk, 1), IncludePrereleases: true, StartedBy: c.PrincipalID,
		})
		if err != nil {
			return err
		}
		out.Bulk = &b
		return nil
	}
	if examined || active {
		return nil // the examination stored the version, or still fetches it: it is delivered once stored
	}
	if v := e.VersionText(); v != "" {
		row, err := s.o.Queue.Add(ctx, tx, queue.NewRow{RepoID: t.id(), Name: e.Name, Version: v, Origin: rc.OriginAllow, RequestID: e.ID, StartedBy: c.PrincipalID})
		var qe *queue.QueuedError
		var fe *queue.FullError
		switch {
		case errors.As(err, &qe), errors.As(err, &fe):
			return nil // fetched by the waiting row, or by a client's request once the queue drains
		case err != nil:
			return err
		}
		out.Rows = append(out.Rows, row)
		return nil
	}
	b, err := s.o.Queue.StartBulk(ctx, tx, queue.BulkSpec{
		RepoID: t.id(), Level: queue.LevelPackage, Subject: e.Name, MaxVersions: 1, MaxBytes: max(st.MaxBytesPerBulk, 1),
		IncludePrereleases: false, StartedBy: c.PrincipalID, Origin: rc.OriginAllow, RequestID: e.ID,
	})
	if err != nil {
		return err
	}
	out.Bulk = &b
	return nil
}

// CheckReason reads a decline's reason: 1 to 1,000 characters.
func CheckReason(reason string) error {
	if strings.TrimSpace(reason) == "" || utf8.RuneCountInString(reason) > MaxReason {
		return errs.E(errs.Invalid, errs.CodeValidation, "A decline gives a reason of 1 to 1,000 characters.",
			errs.WithFields(errs.FieldError{Pointer: "/reason", Code: "out_of_range", Detail: "1 to 1000 characters"}))
	}
	return nil
}

// Decline is [Service.DeclineTx] in a transaction of its own.
func (s *Service) Decline(ctx context.Context, t Target, c Caller, id, reason string, discard bool) (Entry, retention.Discarded, error) {
	var e Entry
	var gone retention.Discarded
	err := s.o.Runner.InTx(ctx, func(tx meta.Tx) error {
		var err error
		e, gone, err = s.DeclineTx(ctx, tx, t, c, id, reason, discard)
		return err
	})
	return e, gone, err
}

// DeclineTx declines an open entry with a reason (R-CUR-015 "Decline"): the entry keeps the reason; its examination's waiting rows and
// expanding bulks are cancelled; with discard, what its examination stored is discarded (retention, cause declined, Q-CUR-143).
func (s *Service) DeclineTx(ctx context.Context, tx meta.Tx, t Target, c Caller, id, reason string, discard bool) (Entry, retention.Discarded, error) {
	var gone retention.Discarded
	if err := CheckReason(reason); err != nil {
		return Entry{}, gone, err
	}
	qs, err := queries(tx)
	if err != nil {
		return Entry{}, gone, err
	}
	e, err := locked(ctx, qs, t.id(), id)
	if err != nil {
		return Entry{}, gone, err
	}
	if e.State != rc.RequestOpen {
		return Entry{}, gone, notOpen(e)
	}
	if err := qs.DeliveryRequestSetState(ctx, q.DeliveryRequestSetStateParams{
		ID: e.ID, State: string(rc.RequestDeclined), DecidedBy: c.PrincipalID, DeclineReason: reason,
	}); err != nil {
		return Entry{}, gone, err
	}
	e.State = rc.RequestDeclined
	if err := s.event(ctx, tx, t, c, audit.EventRemoteRequestDeclined, e, reason); err != nil {
		return Entry{}, gone, err
	}
	if err := s.cancelExamination(ctx, tx, qs, t, e); err != nil {
		return Entry{}, gone, err
	}
	if discard {
		gone, err = s.o.Retention.DiscardTx(ctx, tx, t.Repo, retention.Caller{Actor: c.Actor, Source: c.Source},
			retention.Subject{Name: e.Name, Version: e.VersionText(), RequestID: e.ID}, retention.CauseDeclined)
		if errs.KindOf(err) == errs.Conflict {
			gone, err = retention.Discarded{}, nil // the version is delivered (by another rule): it is not discarded
		}
		if err != nil {
			return Entry{}, gone, err
		}
	}
	e, err = getTx(ctx, qs, t.id(), e.ID)
	return e, gone, err
}

// cancelExamination cancels the waiting rows and the expanding bulks of a declined entry.
func (s *Service) cancelExamination(ctx context.Context, tx meta.Tx, qs *q.Queries, t Target, e Entry) error {
	docs, err := qs.DeliveryRequestRows(ctx, q.DeliveryRequestRowsParams{RepoID: t.id(), Ids: []string{e.ID}})
	if err != nil {
		return err
	}
	for _, d := range docs {
		var r queue.Row
		if err := json.Unmarshal([]byte(d), &r); err != nil {
			return err
		}
		if r.State == rc.StateWaiting {
			if _, err := s.o.Queue.Cancel(ctx, tx, t.id(), r.ID); err != nil {
				return err
			}
		}
	}
	bulks, err := qs.DeliveryRequestBulks(ctx, q.DeliveryRequestBulksParams{RepoID: t.id(), Ids: []string{e.ID}})
	if err != nil {
		return err
	}
	for _, b := range bulks {
		if b.State == string(queue.BulkExpanding) {
			if _, err := s.o.Queue.CancelBulk(ctx, tx, t.id(), b.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

// Withdraw is [Service.WithdrawTx] in a transaction of its own.
func (s *Service) Withdraw(ctx context.Context, t Target, c Caller, id string) (Entry, error) {
	var e Entry
	err := s.o.Runner.InTx(ctx, func(tx meta.Tx) error {
		var err error
		e, err = s.WithdrawTx(ctx, tx, t, c, id)
		return err
	})
	return e, err
}

// WithdrawTx takes the caller out of an entry (R-CUR-015 "Withdraw"): 404 when the caller does not ask in it. An open entry that
// keeps no asker and counts no refusal of a client ends withdrawn; one that clients were refused stays open as their entry.
func (s *Service) WithdrawTx(ctx context.Context, tx meta.Tx, t Target, c Caller, id string) (Entry, error) {
	qs, err := queries(tx)
	if err != nil {
		return Entry{}, err
	}
	e, err := locked(ctx, qs, t.id(), id)
	if err != nil {
		return Entry{}, err
	}
	n, err := qs.DeliveryRequestAskerWithdraw(ctx, q.DeliveryRequestAskerWithdrawParams{RequestID: e.ID, Principal: c.PrincipalID})
	if err != nil {
		return Entry{}, err
	}
	if n == 0 {
		return Entry{}, errs.E(errs.NotFound, errs.CodeNotFound, "You do not ask in this request.")
	}
	if e, err = s.countAskers(ctx, qs, e.ID, false); err != nil {
		return Entry{}, err
	}
	if e.State == rc.RequestOpen && e.Askers == 0 && e.Refusals == 0 {
		if err := qs.DeliveryRequestSetState(ctx, q.DeliveryRequestSetStateParams{ID: e.ID, State: string(rc.RequestWithdrawn)}); err != nil {
			return Entry{}, err
		}
		e.State = rc.RequestWithdrawn
	}
	if err := s.event(ctx, tx, t, c, audit.EventRemoteRequestWithdrawn, e, ""); err != nil {
		return Entry{}, err
	}
	return e, nil
}

// event records one of the requests' events (R-CUR-019) in the act's transaction.
func (s *Service) event(ctx context.Context, tx meta.Tx, t Target, c Caller, typ string, e Entry, reason string) error {
	d := map[string]any{"requestId": e.ID, "name": e.Name, "version": nil}
	if v := e.VersionText(); v != "" {
		d["version"] = v
	}
	if reason != "" {
		d["reason"] = reason
	}
	raw, err := json.Marshal(d)
	if err != nil {
		return err
	}
	id := strconv.FormatInt(t.id(), 10)
	return s.o.Recorder.RecordTx(ctx, tx, audit.Event{
		Type: typ, Actor: c.Actor, Outcome: audit.Success, Source: c.Source,
		Subject:    audit.Subject{Kind: "repository", ID: id, Name: t.Repo.Name.String()},
		Repository: t.Repo.Name.String(), Project: t.Repo.Name.Project, Details: raw,
	})
}
