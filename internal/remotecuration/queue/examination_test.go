package queue

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/guided-traffic/quader/internal/remotecuration"
)

// What the requests for an allow and the examination add to the queue (remote-curation.md R-CUR-015, R-CUR-016, Q-CUR-140, Q-CUR-141),
// over the store in memory. Unit tier, milliseconds, no data.

// TestR_CUR_015_ARequestsBulkQueuesTheNewestVersionsAsTheRequestsRows: the bulk a request starts for the newest versions of a package
// queues its rows with the origin request, the request's ID and the asker's charge, which a person's bulk does not carry, and the
// prereleases left out; an allow's bulk takes the origin allow and no charge.
func TestR_CUR_015_ARequestsBulkQueuesTheNewestVersionsAsTheRequestsRows(t *testing.T) {
	vs := []PlannedVersion{{"3.0.0", 10}, {"2.0.0", 10}, {"1.0.0", 10}}
	ticket := json.RawMessage(`{"key":"user:alice"}`)
	for _, tc := range []struct {
		origin remotecuration.Origin
		ticket json.RawMessage
	}{
		{remotecuration.OriginRequest, ticket},
		{remotecuration.OriginAllow, nil},
	} {
		t.Run(string(tc.origin), func(t *testing.T) {
			f := newFixture(t, Settings{PerRepository: 4, MaxWaiting: 100, MaxAttempts: 5})
			f.svc.o.Planner = fakePlanner{versions: vs}
			b, err := f.svc.StartBulk(context.Background(), nil, BulkSpec{
				RepoID: 1, Level: LevelPackage, Subject: "@acme/b", MaxVersions: 1, MaxBytes: 1 << 30, StartedBy: "alice",
				Origin: tc.origin, RequestID: "req-1", Ticket: tc.ticket,
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.svc.Dispatch(context.Background(), func(error) remotecuration.Failure { return remotecuration.Failure{} }); err != nil {
				t.Fatal(err)
			}
			if len(f.store.rows) != 1 {
				t.Fatalf("%d rows, want the newest version alone", len(f.store.rows))
			}
			r := f.store.rows[0]
			if r.Origin != tc.origin || r.RequestID != "req-1" || r.BulkID != b.ID || r.Version != "3.0.0" || r.StartedBy != "alice" ||
				string(r.Ticket) != string(tc.ticket) {
				t.Errorf("row %+v", r)
			}
		})
	}
	t.Run("a person's bulk keeps the origin bulk and no request", func(t *testing.T) {
		b := Bulk{Origin: ""}
		if b.RowOrigin() != remotecuration.OriginBulk {
			t.Errorf("%s", b.RowOrigin())
		}
	})
}

// TestR_CUR_016_TheEndOfAnAttemptWritesItsExamination: the examination a run reports is written with the end of its attempt, with the
// request of the row; a run that reports none writes none.
func TestR_CUR_016_TheEndOfAnAttemptWritesItsExamination(t *testing.T) {
	f := newFixture(t, Settings{PerRepository: 4, MaxWaiting: 100, MaxAttempts: 5})
	r, err := f.svc.Add(context.Background(), nil, NewRow{RepoID: 1, Name: "@acme/a", Version: "1.0.0", Origin: remotecuration.OriginRequest, RequestID: "req-1"})
	if err != nil {
		t.Fatal(err)
	}
	other := f.add("@acme/c", remotecuration.OriginPerson)
	f.finish(r, Result{
		Outcome: remotecuration.OutcomeCompleted, Files: 1, Stored: 1,
		Examination: &Examination{Name: "@acme/a", Version: "1.0.0", Files: []string{"@acme/a/-/a-1.0.0.tgz"}},
	})
	f.finish(other, Result{Outcome: remotecuration.OutcomeCompleted, Files: 1, Stored: 1})
	if len(f.store.examined) != 1 {
		t.Fatalf("%d examinations written, want one", len(f.store.examined))
	}
	if x := f.store.examined[0]; x.RequestID != "req-1" || x.Name != "@acme/a" || x.Version != "1.0.0" || len(x.Files) != 1 {
		t.Errorf("the examination %+v", x)
	}
}
