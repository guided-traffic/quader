// Package retention removes what a remote repository fetched for examination and does not deliver (remote-curation.md R-CUR-016,
// Q-CUR-24): the discard by a person or by a decline, and the task remote-examination-cleanup, which removes the stored files of every
// version whose examination row is older than the repository's remote.delivery.examinedRetentionDays and that the rules still do not
// deliver. Every removal is an eviction of the remote's cache through the item store, in one transaction with the removal of the
// versions' examination rows and the event remote.examination.discarded, which the revocation journal carries, so that a restore removes
// it again (journal_replay.go).
//
// What is removed of a version is format-independent (Q-CUR-140): the files the queue's runs recorded in its examination row and the
// items the generic package rows link to the version, each kept only when the format classifies its path as a file of that package and
// version. A file the version shares with others by its digest (an image's manifest and layers) classifies to no version and stays.
package retention

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/items"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
	"github.com/guided-traffic/quader/internal/repository"
)

// The causes of a discard, the member cause of remote.examination.discarded (R-CUR-019).
const (
	CausePerson    = "person"
	CauseRetention = "retention"
	CauseDeclined  = "declined"
)

// CleanupActor is the actor of the retention task's discards (R-CUR-016).
var CleanupActor = audit.Actor{Kind: "system", Name: "examination-cleanup"}

// Remover removes files of a repository in the caller's transaction; *items.Store implements it.
type Remover interface {
	RemoveIn(ctx context.Context, tx meta.Tx, spec items.RemoveSpec) error
}

// Classifier names the package and the version a path of a repository belongs to (the format's Classify); "" for none.
type Classifier func(repo repository.Repository, path string) (pkg, version string)

// Repositories reads a repository from the replica's snapshot by its ID.
type Repositories interface {
	ByID(ctx context.Context, id int64) (repository.Repository, error)
}

// Options configure [New].
type Options struct {
	Runner   meta.Runner
	Recorder audit.Recorder
	Remover  Remover
	Classify Classifier
	Repos    Repositories
	// Now replaces the clock, for tests.
	Now func() time.Time
}

// Service discards and runs the retention.
type Service struct {
	o Options
}

// New returns the service.
func New(o Options) (*Service, error) {
	if o.Runner == nil || o.Recorder == nil || o.Remover == nil || o.Classify == nil || o.Repos == nil {
		return nil, errors.New("retention: the service needs a runner, a recorder, a remover, a classifier and the repositories")
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	return &Service{o: o}, nil
}

// Caller is who discards, as the event names it.
type Caller struct {
	Actor  audit.Actor
	Source audit.Source
}

// Discarded is what a discard removed: the versions and the count of their files.
type Discarded struct {
	Versions []string
	Files    int
}

// Subject is what a discard names: a package and one of its versions, or every version of the package when Version is "". RequestID
// limits a package's discard to the versions examined for that request (a decline's, Q-CUR-143); "" for every version the rules do not
// deliver.
type Subject struct {
	Name, Version string
	RequestID     string
}

func queries(tx meta.Tx) (*q.Queries, error) {
	ptx, ok := postgres.Unwrap(tx)
	if !ok {
		return nil, errors.New("retention: the transaction is not one of internal/meta/postgres")
	}
	return q.New(ptx), nil
}

// Discard removes what the remote repo stores of the subject and its rules do not deliver, in a transaction of its own (R-CUR-016
// "Discard"). A version the rules deliver answers 409 delivered.
func (s *Service) Discard(ctx context.Context, repo repository.Repository, c Caller, sub Subject, cause string) (Discarded, error) {
	var out Discarded
	err := s.o.Runner.InTx(ctx, func(tx meta.Tx) error {
		var err error
		out, err = s.DiscardTx(ctx, tx, repo, c, sub, cause)
		return err
	})
	return out, err
}

// DiscardTx is Discard in the caller's transaction (a decline's, R-CUR-015).
func (s *Service) DiscardTx(ctx context.Context, tx meta.Tx, repo repository.Repository, c Caller, sub Subject, cause string) (Discarded, error) {
	qs, err := queries(tx)
	if err != nil {
		return Discarded{}, err
	}
	set := repo.Delivery()
	name, version := set.Fold(sub.Name, sub.Version)
	if version != "" && set.Decide(name, version).Delivered {
		return Discarded{}, errs.E(errs.Conflict, errs.CodeDelivered,
			name+" "+version+" is delivered by "+repo.Name.String()+"; only what its rules do not deliver is discarded.")
	}
	plan, err := s.plan(ctx, qs, repo, name, version, sub.RequestID)
	if err != nil {
		return Discarded{}, err
	}
	out, err := s.remove(ctx, tx, qs, repo, c, name, plan)
	if err != nil {
		return Discarded{}, err
	}
	return out, s.event(ctx, tx, repo, c, name, version, out.Files, cause)
}

// versionFiles are the files of each version a removal takes, by the version in its canonical form.
type versionFiles map[string][]string

// plan finds the versions of the subject that the rules do not deliver and their files: the examination rows' recorded files and the
// items the package rows link to each version, each kept when the format classifies it as a file of that package and version.
func (s *Service) plan(ctx context.Context, qs *q.Queries, repo repository.Repository, name, version, requestID string) (versionFiles, error) {
	set := repo.Delivery()
	plan := versionFiles{}
	seen := map[string]map[string]bool{}
	add := func(v, path string) {
		_, fv := set.Fold(name, v)
		if fv == "" || set.Decide(name, fv).Delivered || (version != "" && fv != version) {
			return
		}
		if _, ok := plan[fv]; !ok {
			plan[fv], seen[fv] = nil, map[string]bool{}
		}
		if path == "" || seen[fv][path] {
			return
		}
		pkg, pv := s.o.Classify(repo, path)
		if pkg == "" || pv == "" {
			return
		}
		cn, cv := set.Fold(pkg, pv)
		if cn != name || cv != fv {
			return
		}
		seen[fv][path] = true
		plan[fv] = append(plan[fv], path)
	}
	exams, err := qs.RemoteExaminationsOf(ctx, q.RemoteExaminationsOfParams{RepoID: repo.NumericID(), Name: name, Version: version})
	if err != nil {
		return nil, err
	}
	for _, x := range exams {
		if requestID != "" && x.RequestID != requestID {
			continue
		}
		add(x.Version, "")
		for _, f := range x.Files {
			add(x.Version, f)
		}
	}
	stored, err := qs.RemoteStoredVersionFiles(ctx, q.RemoteStoredVersionFilesParams{RepoID: repo.NumericID(), Name: name})
	if err != nil {
		return nil, err
	}
	for _, row := range stored {
		_, fv := set.Fold(name, row.Version)
		if requestID != "" {
			if _, examined := plan[fv]; !examined {
				continue // a decline discards only what its request's examination stored
			}
		}
		add(row.Version, row.Path)
	}
	return plan, nil
}

// remove evicts the files of the plan and removes the versions' examination rows.
func (s *Service) remove(ctx context.Context, tx meta.Tx, qs *q.Queries, repo repository.Repository, c Caller, name string, plan versionFiles) (Discarded, error) {
	var out Discarded
	var paths []string
	for v, files := range plan {
		out.Versions = append(out.Versions, v)
		paths = append(paths, files...)
	}
	if len(out.Versions) == 0 {
		return out, nil
	}
	if len(paths) > 0 {
		if err := s.o.Remover.RemoveIn(ctx, tx, items.RemoveSpec{Repo: items.RepoOf(repo), Paths: paths, Actor: c.Actor, Source: c.Source}); err != nil {
			return Discarded{}, err
		}
	}
	out.Files = len(paths)
	_, err := qs.RemoteExaminationDelete(ctx, q.RemoteExaminationDeleteParams{RepoID: repo.NumericID(), Name: name, Versions: out.Versions})
	return out, err
}

// discardEntry is the journal entry of remote.examination.discarded (repositories-and-proxy.md, the table of journaled events): the
// repository and the subject; version is absent for a package.
type discardEntry struct {
	RepositoryID string `json:"repositoryId"`
	Key          string `json:"key"`
	Name         string `json:"name"`
	Version      string `json:"version,omitempty"`
}

// event records remote.examination.discarded with its journal entry (R-CUR-016, R-CUR-019).
func (s *Service) event(ctx context.Context, tx meta.Tx, repo repository.Repository, c Caller, name, version string, files int, cause string) error {
	id := strconv.FormatInt(repo.NumericID(), 10)
	entry, err := json.Marshal(discardEntry{RepositoryID: id, Key: repo.Name.String(), Name: name, Version: version})
	if err != nil {
		return err
	}
	var v any
	if version != "" {
		v = version
	}
	details, err := json.Marshal(map[string]any{"name": name, "version": v, "files": files, "cause": cause})
	if err != nil {
		return err
	}
	return s.o.Recorder.RecordTx(ctx, tx, audit.Event{
		Type: audit.EventRemoteExaminationDiscarded, Actor: c.Actor, Outcome: audit.Success, Source: c.Source,
		Subject:    audit.Subject{Kind: "repository", ID: id, Name: repo.Name.String()},
		Repository: repo.Name.String(), Project: repo.Name.Project, Details: details, Journal: entry,
	})
}

// CleanupResult is what one run of the task remote-examination-cleanup did, the result its job records.
type CleanupResult struct {
	// Discarded are the versions whose stored files were removed after their days without a decision.
	Discarded int `json:"discarded"`
	// Files are the files removed.
	Files int `json:"files"`
	// Delivered are the examination rows dropped because the rules deliver their versions now (Q-CUR-142).
	Delivered int `json:"delivered"`
	// Requests are the request entries removed after proxy.delivery.request_retention without an asker (R-CUR-015).
	Requests int64 `json:"requests"`
}

// cleanupBatch is the batch of the retention task (R-CUR-016: batches of 1,000).
const cleanupBatch = 1000

// minRetentionDays is the smallest remote.delivery.examinedRetentionDays: no row younger than it is read.
const minRetentionDays = 1

// Cleanup is one run of the task remote-examination-cleanup (R-CUR-016 "Removal after a time without a decision"): it reads the
// examination rows older than a day in batches of 1,000, the oldest first, keyed so that a row that stays (younger than its
// repository's days) is passed and not read again in the run; a row whose version the rules deliver now goes without an eviction
// (Q-CUR-142); a row older than its repository's days whose version they still do not deliver is discarded with the actor
// system:examination-cleanup, each version in a transaction of its own, so that a run that stops keeps what it did. Then it removes the
// request entries without an asker after requestRetention.
func (s *Service) Cleanup(ctx context.Context, requestRetention time.Duration) (CleanupResult, error) {
	var res CleanupResult
	now := s.o.Now()
	before := now.Add(-minRetentionDays * 24 * time.Hour)
	var after struct {
		at            time.Time
		repo          int64
		name, version string
	}
	after.at = time.Unix(0, 0).Add(-time.Microsecond)
	for {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		var rows []q.RemoteExaminationsOlderRow
		err := s.o.Runner.InTx(ctx, func(tx meta.Tx) error {
			qs, err := queries(tx)
			if err != nil {
				return err
			}
			rows, err = qs.RemoteExaminationsOlder(ctx, q.RemoteExaminationsOlderParams{
				BeforeUs: before.UnixMicro(), AfterUs: after.at.UnixMicro(), AfterRepo: after.repo, AfterName: after.name,
				AfterVersion: after.version, MaxRows: cleanupBatch,
			})
			return err
		})
		if err != nil {
			return res, err
		}
		for _, row := range rows {
			if err := s.cleanupRow(ctx, row, now, &res); err != nil {
				return res, err
			}
		}
		if len(rows) < cleanupBatch {
			break
		}
		last := rows[len(rows)-1]
		after.at, after.repo, after.name, after.version = last.FetchedAt.Time, last.RepoID, last.Name, last.Version
	}
	n, err := s.pruneRequests(ctx, now.Add(-requestRetention))
	res.Requests = n
	return res, err
}

// cleanupRow decides one examination row of the retention task.
func (s *Service) cleanupRow(ctx context.Context, row q.RemoteExaminationsOlderRow, now time.Time, res *CleanupResult) error {
	repo, err := s.o.Repos.ByID(ctx, row.RepoID)
	if errs.KindOf(err) == errs.NotFound {
		return nil // the repository goes with its rows (the key cascades)
	}
	if err != nil {
		return err
	}
	st, ok := repo.Remote()
	if !ok {
		return nil
	}
	if repo.Delivery().Decide(row.Name, row.Version).Delivered {
		return s.o.Runner.InTx(ctx, func(tx meta.Tx) error {
			qs, err := queries(tx)
			if err != nil {
				return err
			}
			n, err := qs.RemoteExaminationDelete(ctx, q.RemoteExaminationDeleteParams{RepoID: row.RepoID, Name: row.Name, Versions: []string{row.Version}})
			res.Delivered += int(n)
			return err
		})
	}
	days := st.Delivery.ExaminedRetentionDays
	if days < minRetentionDays {
		days = repository.DefaultExaminedRetentionDays
	}
	if !row.FetchedAt.Time.Before(now.Add(-time.Duration(days) * 24 * time.Hour)) {
		return nil
	}
	return s.o.Runner.InTx(ctx, func(tx meta.Tx) error {
		out, err := s.DiscardTx(ctx, tx, repo, Caller{Actor: CleanupActor}, Subject{Name: row.Name, Version: row.Version}, CauseRetention)
		if errs.KindOf(err) == errs.Conflict {
			return nil // delivered meanwhile
		}
		if err != nil {
			return err
		}
		res.Discarded += len(out.Versions)
		res.Files += out.Files
		return nil
	})
}

// pruneRequests removes the request entries nobody asks in any more, in batches.
func (s *Service) pruneRequests(ctx context.Context, before time.Time) (int64, error) {
	var total int64
	for range 100 {
		var n int64
		err := s.o.Runner.InTx(ctx, func(tx meta.Tx) error {
			qs, err := queries(tx)
			if err != nil {
				return err
			}
			n, err = qs.DeliveryRequestsPrune(ctx, q.DeliveryRequestsPruneParams{BeforeUs: before.UnixMicro(), MaxRows: cleanupBatch})
			return err
		})
		if err != nil {
			return total, err
		}
		total += n
		if n < cleanupBatch {
			break
		}
	}
	return total, nil
}
