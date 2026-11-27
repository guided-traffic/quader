package v1

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/guided-traffic/quader/internal/api/v1/gen"
	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/blob/history"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/httpserver"
	"github.com/guided-traffic/quader/internal/meta"
)

// The history of the stored content in the API (storage.md R-STOR-111): the cells of a period per connection, its problems and its
// findings, and the acknowledgement of a finding. The records are read by internal/blob/history and the problems and cells derived
// there at read time (Q-STOR-525), once for the overview, the list and the strips. Nothing here deletes, drains or touches a key.

// parsePeriod is the period of a request's parameter, 30d when it names none; 400 for another.
func parsePeriod(name *string) (history.Period, error) {
	n := ""
	if name != nil {
		n = *name
	}
	p, ok := history.ParsePeriod(n)
	if !ok {
		return history.Period{}, malformed("/period", "enum", "The period is 24h, 7d, 30d, 90d or 1y.", nil)
	}
	return p, nil
}

func periodName[T ~string](p *T) *string {
	if p == nil {
		return nil
	}
	s := string(*p)
	return &s
}

// GetStorageHistory implements GET /api/v1/storage/history. The caller holds settings:read (declared).
func (s *Server) GetStorageHistory(w http.ResponseWriter, r *http.Request, p gen.GetStorageHistoryParams) {
	if s.o.Storage.Cells == nil {
		httpserver.Problem(w, r, unavailable("The history of the stored content"))
		return
	}
	period, err := parsePeriod(periodName(p.Period))
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	ctx := r.Context()
	from, to := period.Window(time.Now())
	records, err := s.o.Storage.Cells(ctx, period, to)
	if err != nil {
		httpserver.Problem(w, r, errs.Wrap(err, "api.getStorageHistory"))
		return
	}
	var ids []string
	if s.o.Storage.Connections != nil {
		views, err := s.o.Storage.Connections(ctx)
		if err != nil {
			httpserver.Problem(w, r, errs.Wrap(err, "api.getStorageHistory"))
			return
		}
		for _, v := range views {
			// A connection retired before the period has nothing in it; one the records name stays by its id (Q-STOR-532).
			if v.RetiredAt == nil || !v.RetiredAt.Before(from) {
				ids = append(ids, v.ID)
			}
		}
	}
	out := gen.StorageHistory{
		Period: gen.StorageHistoryPeriod(period.Name), From: from, To: to, BucketSeconds: int64(period.Cell / time.Second),
		Connections: []gen.StorageHistoryConnection{},
	}
	for _, c := range history.DeriveCells(period, to, records, ids) {
		out.Connections = append(out.Connections, storageHistoryConnection(c))
	}
	writeJSON(w, r, http.StatusOK, out)
}

func storageHistoryConnection(c history.ConnectionCells) gen.StorageHistoryConnection {
	out := gen.StorageHistoryConnection{
		Id: c.Connection, Buckets: make([]gen.StorageHistoryBucket, 0, len(c.Cells)),
		Findings: gen.StorageFindingsTotal{
			Count: c.Findings.N, Bytes: c.Findings.Bytes, Unlisted: c.Findings.Unlisted, ByOutcome: c.Findings.ByOutcome, BySource: c.Findings.BySource,
		},
	}
	for _, b := range c.Cells {
		out.Buckets = append(out.Buckets, gen.StorageHistoryBucket{
			Start: b.Start, Tone: gen.StorageHistoryBucketTone(b.Tone), UnreachableSeconds: b.UnreachableSeconds, Replicas: b.Replicas,
			StartChecksFailed: b.StartChecksFailed, DamagedCopies: b.DamagedCopies, DamagedBytes: b.DamagedBytes, UnavailableBlobs: b.UnavailableBlobs,
			ReadFallovers: b.ReadFallovers, ReadsUnavailable: b.ReadsUnavailable, WriteFallovers: b.WriteFallovers, UploadsRefused: b.UploadsRefused,
			CopiesFailed: b.CopiesFailed, ReadsRefused: b.ReadsRefused, UnknownKeyReads: b.UnknownKeyReads,
		})
	}
	return out
}

// periodProblems reads the records of period p ending at now and derives its problems; none without the service.
func (s *Server) periodProblems(r *http.Request, p history.Period, now time.Time) ([]history.Problem, error) {
	if s.o.Storage.Problems == nil {
		return nil, nil
	}
	from, to := p.Window(now)
	records, err := s.o.Storage.Problems(r.Context(), from, to)
	if err != nil {
		return nil, err
	}
	return history.DeriveProblems(records, to), nil
}

// problemKey is a problem's place in the list's order, the cursor's values: its start, its kind and its connection.
func problemKey(p history.Problem) []string {
	return []string{p.Started.UTC().Format(time.RFC3339Nano), p.Kind, p.Connection}
}

// ListStorageProblems implements GET /api/v1/storage/problems. The caller holds settings:read (declared). The problems are derived
// for the whole period and paged in memory: their number is bounded by the history's caps (Q-STOR-516, H-2437).
func (s *Server) ListStorageProblems(w http.ResponseWriter, r *http.Request, p gen.ListStorageProblemsParams) {
	if s.o.Storage.Problems == nil {
		httpserver.Problem(w, r, unavailable("The problems of the stored content"))
		return
	}
	lq, err := ParseList(r, ListSpec{Filters: []string{"period", "connection", "kind", "state"}}, s.o.API.Config)
	var period history.Period
	if err == nil {
		period, err = parsePeriod(periodName(p.Period))
	}
	var after *history.Problem
	if err == nil && lq.After != nil {
		after, err = problemCursor(lq.After)
	}
	switch {
	case err != nil:
	case p.Kind != nil && !p.Kind.Valid():
		err = malformed("/kind", "enum", "The kind is unreachable, start_check_failed, copies_damaged, uploads_refused or reads_unavailable.", nil)
	case p.State != nil && !p.State.Valid():
		err = malformed("/state", "enum", "The state is ongoing, ended or all.", nil)
	case p.Connection != nil && !damagedConnectionPattern.MatchString(*p.Connection):
		err = malformed("/connection", "pattern", "The connection is the id of a connection.", nil)
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	all, err := s.periodProblems(r, period, time.Now())
	if err != nil {
		httpserver.Problem(w, r, errs.Wrap(err, "api.listStorageProblems"))
		return
	}
	rows := make([]history.Problem, 0, lq.FetchLimit())
	for _, pr := range all {
		switch {
		case p.Connection != nil && pr.Connection != *p.Connection,
			p.Kind != nil && pr.Kind != string(*p.Kind),
			p.State != nil && *p.State == gen.ListStorageProblemsParamsStateOngoing && !pr.Lasting(),
			p.State != nil && *p.State == gen.ListStorageProblemsParamsStateEnded && pr.Lasting(),
			after != nil && !history.Newer(*after, pr):
			continue
		}
		rows = append(rows, pr)
		if len(rows) == lq.FetchLimit() {
			break
		}
	}
	WritePage(w, r, MapPage(NewPage(lq, rows, problemKey), storageProblemView))
}

// problemCursor is the problem a cursor's values name.
func problemCursor(after []string) (*history.Problem, error) {
	if len(after) != 3 {
		return nil, invalidCursor()
	}
	t, err := time.Parse(time.RFC3339Nano, after[0])
	if err != nil {
		return nil, invalidCursor()
	}
	return &history.Problem{Started: t, Kind: after[1], Connection: after[2]}, nil
}

func storageProblemView(p history.Problem) gen.StorageProblem {
	out := gen.StorageProblem{
		Kind: gen.StorageProblemKind(p.Kind), Connection: p.Connection, StartedAt: p.Started.UTC(), Seconds: p.Seconds,
		Tone: gen.StorageProblemTone(p.Tone), Replicas: p.Replicas, Occurrences: p.Occurrences, Count: p.Count, Bytes: p.Bytes,
		Resolved: p.Resolved, Open: p.Open, Acknowledged: p.Acknowledged, Unavailable: p.Unavailable,
		Codes: nonNilStrings(p.Codes), Sources: nonNilStrings(p.Sources), Steps: nonNilStrings(p.Steps),
	}
	if !p.Lasting() {
		end := p.Ended.UTC()
		out.EndedAt = &end
	}
	return out
}

func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// ListStorageFindings implements GET /api/v1/storage/findings. The caller holds settings:read (declared).
func (s *Server) ListStorageFindings(w http.ResponseWriter, r *http.Request, p gen.ListStorageFindingsParams) {
	if s.o.Storage.Findings == nil {
		httpserver.Problem(w, r, unavailable("The findings of the stored content"))
		return
	}
	lq, err := ParseList(r, ListSpec{Filters: []string{"period", "connection", "state"}}, s.o.API.Config)
	var period history.Period
	if err == nil {
		period, err = parsePeriod(periodName(p.Period))
	}
	from, _ := period.Window(time.Now())
	fq := history.FindingQuery{From: from, State: history.FindingAll, Limit: lq.FetchLimit()}
	if err == nil && lq.After != nil {
		fq.AfterDetected, fq.AfterID, err = findingCursor(lq.After)
	}
	switch {
	case err != nil:
	case p.State != nil && !p.State.Valid():
		err = malformed("/state", "enum", "The state is open, acknowledged, resolved or all.", nil)
	case p.Connection != nil && !damagedConnectionPattern.MatchString(*p.Connection):
		err = malformed("/connection", "pattern", "The connection is the id of a connection.", nil)
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	if p.State != nil {
		fq.State = string(*p.State)
	}
	if p.Connection != nil {
		fq.Connection = *p.Connection
	}
	rows, err := s.o.Storage.Findings(r.Context(), fq)
	if err != nil {
		httpserver.Problem(w, r, errs.Wrap(err, "api.listStorageFindings"))
		return
	}
	page := NewPage(lq, rows, func(f history.Finding) []string {
		return []string{strconv.FormatInt(f.Detected.UnixMicro(), 10), strconv.FormatInt(f.ID, 10)}
	})
	WritePage(w, r, MapPage(page, storageFindingView))
}

// findingCursor is the detection and the id a cursor's values name.
func findingCursor(after []string) (time.Time, int64, error) {
	if len(after) != 2 {
		return time.Time{}, 0, invalidCursor()
	}
	us, err1 := strconv.ParseInt(after[0], 10, 64)
	id, err2 := strconv.ParseInt(after[1], 10, 64)
	if err1 != nil || err2 != nil || id <= 0 {
		return time.Time{}, 0, invalidCursor()
	}
	return time.UnixMicro(us).UTC(), id, nil
}

func storageFindingView(f history.Finding) gen.StorageFinding {
	out := gen.StorageFinding{
		Id: f.ID, DetectedAt: f.Detected.UTC(), Connection: f.Connection, Sha256: f.Digest.Hex(), Status: gen.StorageFindingStatus(f.Status),
		Source: gen.StorageFindingSource(f.Source), Reason: f.Reason, StoredSize: f.StoredSize, Unavailable: f.Unavailable,
		Items: make([]gen.DamagedBlobItem, 0, len(f.Items)), ItemCount: f.ItemCount,
	}
	if !f.Resolved.IsZero() {
		at, res := f.Resolved.UTC(), gen.StorageFindingResolution(f.Resolution)
		out.ResolvedAt, out.Resolution = &at, &res
	}
	if !f.Acknowledged.IsZero() {
		ack := gen.StorageFindingAcknowledgement{At: f.Acknowledged.UTC(), Note: f.Note}
		if f.AcknowledgedBy != "" {
			ack.By = &struct {
				Id   string `json:"id"`
				Name string `json:"name"`
			}{Id: f.AcknowledgedBy, Name: f.ByName}
		}
		out.Acknowledged = &ack
	}
	for _, it := range f.Items {
		out.Items = append(out.Items, gen.DamagedBlobItem{Project: it.Project, Repository: it.Repository, Path: it.Path, Trashed: it.Trashed})
	}
	return out
}

// maxFindingNote is the longest note of an acknowledgement, in characters.
const maxFindingNote = 500

// AcknowledgeStorageFinding implements POST /api/v1/storage/findings/{id}/acknowledge. The caller holds settings:write (declared). The
// acknowledgement and its event storage.finding.acknowledged are written in one transaction (Q-OPS-472): an acknowledgement the audit log
// does not hold is not made.
func (s *Server) AcknowledgeStorageFinding(w http.ResponseWriter, r *http.Request, id int64) {
	if s.o.Storage.Acknowledge == nil || s.o.API.Store == nil {
		httpserver.Problem(w, r, unavailable("The acknowledgement of a finding"))
		return
	}
	if id <= 0 {
		httpserver.Problem(w, r, malformed("/id", "minimum", "The id is a finding's id.", nil))
		return
	}
	in, err := s.acknowledgeInput(w, r)
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	ctx := r.Context()
	caller := httpserver.PrincipalFrom(ctx)
	by := ""
	if caller.Kind == httpserver.KindUser || caller.Kind == httpserver.KindServiceAccount {
		by = caller.ID
	}
	var out history.Finding
	err = s.o.API.Store.InTx(ctx, func(tx meta.Tx) error {
		f, err := s.o.Storage.Acknowledge(ctx, tx, id, by, in)
		if err != nil {
			return err
		}
		out = f
		details, err := json.Marshal(map[string]any{"finding": f.ID, "connection": f.Connection, "sha256": f.Digest.Hex(), "note": in})
		if err != nil {
			return err
		}
		return s.recorder().RecordTx(ctx, tx, audit.Event{
			Type: audit.EventStorageFindingAcknowledged, Actor: ActorFrom(caller), Outcome: audit.Success, Source: auditSource(r),
			Subject: audit.Subject{Kind: "storage_finding", ID: strconv.FormatInt(f.ID, 10)}, Details: details,
		})
	})
	switch {
	case errors.Is(err, history.ErrNoFinding):
		httpserver.Problem(w, r, errs.E(errs.NotFound, errs.CodeNotFound, "No finding has this id."))
	case errors.Is(err, history.ErrFindingResolved):
		httpserver.Problem(w, r, errs.E(errs.Conflict, errs.CodeFindingResolved,
			"The finding is resolved: its copy was written again, found again or deleted. A resolved finding is not acknowledged."))
	case err != nil:
		httpserver.Problem(w, r, errs.Wrap(err, "api.acknowledgeStorageFinding"))
	default:
		writeJSON(w, r, http.StatusOK, storageFindingView(out))
	}
}

// acknowledgeInput is the note of an acknowledgement: the optional body's, empty without one.
func (s *Server) acknowledgeInput(w http.ResponseWriter, r *http.Request) (string, error) {
	body, err := readOptionalBody(w, r, int64(s.o.API.Config.MaxRequestBodyBytes))
	if err != nil || body == nil {
		return "", err
	}
	if err := requireJSON(r, MediaJSON); err != nil {
		return "", err
	}
	var in gen.StorageFindingAcknowledgeInput
	if err := StrictUnmarshal(body, &in); err != nil {
		return "", err
	}
	if in.Note == nil {
		return "", nil
	}
	if n := len([]rune(*in.Note)); n > maxFindingNote {
		return "", errs.E(errs.Invalid, errs.CodeValidation, "The note is at most 500 characters.",
			errs.WithFields(errs.FieldError{Pointer: "/note", Code: "maxLength", Detail: "at most 500 characters"}))
	}
	return *in.Note, nil
}
