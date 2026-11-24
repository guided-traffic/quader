//go:build integration

package backup

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/guided-traffic/quader/internal/items"
	"github.com/guided-traffic/quader/internal/journal"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/qname"
	"github.com/guided-traffic/quader/internal/remotecuration/retention"
	"github.com/guided-traffic/quader/internal/repository"
)

// R-CUR-016, R-CUR-021, R-HA-054: a discard of what a remote fetched for examination, written after t through the retention's service
// the API uses, shipped with the journal's shipper and replayed into the copy made at t. Data: two examination rows, no artifact.

// noRemover stands for the item store: the twin holds no item, the removal's eviction is the retention task's (area npm).
type noRemover struct{ paths []string }

func (r *noRemover) RemoveIn(_ context.Context, _ meta.Tx, spec items.RemoveSpec) error {
	r.paths = append(r.paths, spec.Paths...)
	return nil
}

// TestR_CUR_016_ADiscardAfterARestorePointIsReplayed: after t a person discards a version that was examined at t. The replay removes
// nothing itself: it makes the version's examination row in the restored copy due and queues a run of the task
// remote-examination-cleanup, whose run removes what is stored of the version while the rules still do not deliver it (its eviction is
// TestR_CUR_016_WhatWasExaminedIsRemovedAfterItsDaysOrADiscard of the area npm); the other version's row keeps its time, and a second
// replay finds the entry in effect. About 1 s.
func TestR_CUR_016_ADiscardAfterARestorePointIsReplayed(t *testing.T) {
	w := newNarrowWorld(t)
	ctx := context.Background()
	w.create("remote", "npm-remote", "npm", "")
	id := w.src.repoID("npm-remote")
	w.src.exec(fmt.Sprintf(`INSERT INTO remote_examinations (repo_id, name, version, files) VALUES
		(%d, 'a', '1.0.0', '{a/-/a-1.0.0.tgz}'), (%d, 'a', '2.0.0', '{a/-/a-2.0.0.tgz}')`, id, id))
	w.snapshot()

	doc := []byte(`{"remote":{"url":"https://upstream.example.test/","delivery":{"mode":"onlyAllowed"}}}`)
	repo, err := repository.Build(id, 1, qname.Repo{Project: "web", Key: "npm-remote"}, repository.Remote, "npm", doc, nil)
	if err != nil {
		t.Fatal(err)
	}
	rm := &noRemover{}
	svc, err := retention.New(retention.Options{
		Runner: w.src.db, Recorder: w.src.rec, Remover: rm, Repos: noRepos{},
		Classify: func(_ repository.Repository, path string) (string, string) {
			if path == "a/-/a-1.0.0.tgz" {
				return "a", "1.0.0"
			}
			return "a", "2.0.0"
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	gone, err := svc.Discard(ctx, repo, retention.Caller{Actor: repoCaller.Actor}, retention.Subject{Name: "a", Version: "1.0.0"}, retention.CausePerson)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(gone.Versions, []string{"1.0.0"}) || gone.Files != 1 || !slices.Equal(rm.paths, []string{"a/-/a-1.0.0.tgz"}) {
		t.Fatalf("the discard: %+v, removed %v", gone, rm.paths)
	}
	js := w.ship(1)
	w.src.close()

	rst := w.tw.open(w.tw.restored)
	due := func(version string) bool {
		return rst.number(fmt.Sprintf(`SELECT count(*) FROM remote_examinations WHERE repo_id = %d AND name = 'a' AND version = '%s'
			AND fetched_at <= to_timestamp(0)`, id, version)) == 1
	}
	if due("1.0.0") || due("2.0.0") {
		t.Fatal("a row of the copy at t is due already")
	}
	sum, _ := rst.replay(js, w.tw.at, rst.changes)
	if o := outcomes(sum)["remote.examination.discarded web/npm-remote a@1.0.0"]; !slices.Equal(o, []journal.Outcome{journal.Applied}) {
		t.Fatalf("the replay: %v (%v), want applied", o, outcomes(sum))
	}
	if !due("1.0.0") || due("2.0.0") {
		t.Errorf("1.0.0 due %v, 2.0.0 due %v; want only the discarded version due", due("1.0.0"), due("2.0.0"))
	}
	if n := rst.number(`SELECT count(*) FROM job WHERE kind = 'remote-examination-cleanup'`); n != 1 {
		t.Errorf("%d runs of remote-examination-cleanup queued, want one", n)
	}
	t.Run("R-HA-054 a second replay finds the entry in effect", func(t *testing.T) {
		sum, _ := rst.replay(js, w.tw.at, rst.changes)
		if o := outcomes(sum)["remote.examination.discarded web/npm-remote a@1.0.0"]; !slices.Equal(o, []journal.Outcome{journal.AlreadyInEffect}) {
			t.Errorf("the second replay: %v, want already in effect", o)
		}
	})
}

// noRepos is the retention's snapshot reader for a discard, which reads none.
type noRepos struct{}

func (noRepos) ByID(context.Context, int64) (repository.Repository, error) {
	return repository.Repository{}, fmt.Errorf("no snapshot in this test")
}
