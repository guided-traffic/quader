package requests

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
	rc "github.com/guided-traffic/quader/internal/remotecuration"
	"github.com/guided-traffic/quader/internal/remotecuration/queue"
)

// The reads of the requests (R-CUR-015 "The list", "Who sees them"): one entry, a page of a repository's entries with their askers,
// their examination and the versions it stored, and the open entries of a package's subjects for the catalogue's rows (R-CUR-009).

// Asker is a person who asks in an entry.
type Asker struct {
	ID, Name, Note string
	AskedAt        time.Time
}

// Examined is a version stored for an entry's examination (remote_examinations): its verdicts are the stored version's, read where a
// stored version's findings, malware verdict and signatures are (R-API-081, Q-CUR-146).
type Examined struct {
	Version   string
	FetchedAt time.Time
	Files     int
}

// View is an entry as the list answers it.
type View struct {
	Entry
	Askers []Asker
	// Examination is the entry's newest row of the download queue; nil for none (yet).
	Examination *queue.Row
	// Examined are the versions its examination stored and the rules do not deliver yet.
	Examined []Examined
}

// Filter selects a page of the list. Mine is a principal's ID: only the entries the principal asks in; "" for all.
type Filter struct {
	State, Origin, Mine string
	// After is the last entry of the previous page; nil for the first.
	After *Entry
	Limit int
}

// Get reads one entry of a repository; 404 for none.
func (s *Service) Get(ctx context.Context, repoID int64, id string) (Entry, error) {
	var e Entry
	err := s.o.Runner.InTx(ctx, func(tx meta.Tx) error {
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		var found bool
		e, found, err = firstEntry(qs.DeliveryRequestGet(ctx, q.DeliveryRequestGetParams{RepoID: repoID, ID: id}))
		if err == nil && !found {
			err = notFound()
		}
		return err
	})
	return e, err
}

// ViewTx reads one entry of a repository as the list shows it, in the caller's transaction; 404 for none.
func (s *Service) ViewTx(ctx context.Context, tx meta.Tx, repoID int64, id string) (View, error) {
	qs, err := queries(tx)
	if err != nil {
		return View{}, err
	}
	e, err := getTx(ctx, qs, repoID, id)
	if err != nil {
		return View{}, err
	}
	out := []View{{Entry: e}}
	if err := s.fill(ctx, qs, repoID, []string{e.ID}, map[string]int{e.ID: 0}, out); err != nil {
		return View{}, err
	}
	return out[0], nil
}

// List reads one page of a repository's entries with what each shows.
func (s *Service) List(ctx context.Context, repoID int64, f Filter) ([]View, error) {
	var out []View
	err := s.o.Runner.InTx(ctx, func(tx meta.Tx) error {
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		p := q.DeliveryRequestsListParams{
			RepoID: repoID, State: f.State, Origin: f.Origin, Mine: f.Mine, AfterOpen: -1,
			MaxRows: int32(min(max(f.Limit, 1), 10001)), //nolint:gosec // bounded
		}
		if f.After != nil {
			p.AfterOpen, p.AfterUs, p.AfterID = openRank(f.After.State), f.After.LastAt.UnixMicro(), f.After.ID
		}
		docs, err := qs.DeliveryRequestsList(ctx, p)
		if err != nil {
			return err
		}
		out = make([]View, 0, len(docs))
		ids := make([]string, 0, len(docs))
		at := map[string]int{}
		for _, d := range docs {
			e, err := decodeEntry(d)
			if err != nil {
				return err
			}
			at[e.ID] = len(out)
			ids = append(ids, e.ID)
			out = append(out, View{Entry: e})
		}
		return s.fill(ctx, qs, repoID, ids, at, out)
	})
	return out, err
}

// fill adds the askers, the examination's row and the examined versions to the views.
func (s *Service) fill(ctx context.Context, qs *q.Queries, repoID int64, ids []string, at map[string]int, out []View) error {
	if len(ids) == 0 {
		return nil
	}
	askers, err := qs.DeliveryRequestAskersOf(ctx, ids)
	if err != nil {
		return err
	}
	for _, a := range askers {
		v := &out[at[a.RequestID]]
		v.Askers = append(v.Askers, Asker{ID: a.Principal, Name: a.Name, Note: a.Note, AskedAt: a.AskedAt.Time.UTC()})
	}
	docs, err := qs.DeliveryRequestRows(ctx, q.DeliveryRequestRowsParams{RepoID: repoID, Ids: ids})
	if err != nil {
		return err
	}
	for _, d := range docs {
		var r queue.Row
		if err := json.Unmarshal([]byte(d), &r); err != nil {
			return err
		}
		if string(r.Ticket) == "null" {
			r.Ticket = nil
		}
		i, ok := at[r.RequestID]
		if ok && out[i].Examination == nil {
			out[i].Examination = &r // newest first
		}
	}
	for i := range out {
		v := &out[i]
		rows, err := qs.RemoteExaminationsOf(ctx, q.RemoteExaminationsOfParams{RepoID: repoID, Name: v.Name, Version: v.VersionText()})
		if err != nil {
			return err
		}
		for _, x := range rows {
			if x.RequestID != "" && x.RequestID != v.ID {
				continue
			}
			v.Examined = append(v.Examined, Examined{Version: x.Version, FetchedAt: x.FetchedAt.Time.UTC(), Files: len(x.Files)})
		}
	}
	return nil
}

func openRank(st rc.RequestState) int32 {
	if st == rc.RequestOpen {
		return 0
	}
	return 1
}

// Open implements [remotecuration.Requests]: the open entries of a package's subjects, keyed by the version ("" for the package).
func (s *Service) Open(ctx context.Context, repoID, name string, versions []string) (map[string]rc.OpenRequest, error) {
	id, err := strconv.ParseInt(repoID, 10, 64)
	if err != nil {
		return map[string]rc.OpenRequest{}, nil //nolint:nilerr // an id of another form names no repository
	}
	if versions == nil {
		versions = []string{}
	}
	out := map[string]rc.OpenRequest{}
	err = s.o.Runner.InTx(ctx, func(tx meta.Tx) error {
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		rows, err := qs.DeliveryRequestsOpenOf(ctx, q.DeliveryRequestsOpenOfParams{RepoID: id, Name: name, Versions: versions})
		for _, r := range rows {
			out[r.Version] = rc.OpenRequest{ID: r.ID, Version: r.Version, Askers: int(r.Askers), Refusals: r.Refusals}
		}
		return err
	})
	return out, err
}
