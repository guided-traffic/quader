package fetch

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/mirror/discover"
)

// TestR_SIG_026_TheRequestsOfARemoteAreBoundedPerHour: signatures.remote.requests_per_hour is a budget per remote and replica that
// refills at its rate; a spent budget says how long until the next request may go, and another remote has its own. Unit, microseconds,
// a clock of the test.
func TestR_SIG_026_TheRequestsOfARemoteAreBoundedPerHour(t *testing.T) {
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	b := newBudgets(3, func() time.Time { return now })
	for i := range 3 {
		if err := b.take(1); err != nil {
			t.Fatalf("request %d: %v", i+1, err)
		}
	}
	var spent errBudget
	if err := b.take(1); !errors.As(err, &spent) || spent.after < 19*time.Minute || spent.after > 21*time.Minute {
		t.Fatalf("the fourth request of the hour: %v, want a wait of about 20 minutes", err)
	}
	if err := b.take(2); err != nil {
		t.Errorf("another remote: %v", err)
	}
	now = now.Add(20 * time.Minute)
	if err := b.take(1); err != nil {
		t.Errorf("after a third of the hour: %v", err)
	}
}

// countingUpstream counts the requests that reach it.
type countingUpstream struct {
	discover.Upstream
	n int
}

func (c *countingUpstream) Get(context.Context, *url.URL, string, int64) (*discover.Response, error) {
	c.n++
	return &discover.Response{Status: 200}, nil
}

func (c *countingUpstream) Document(context.Context, string, string) ([]byte, bool, error) {
	c.n++
	return nil, false, nil
}

func (c *countingUpstream) Artifact(context.Context, string, int64) ([]byte, bool, error) {
	c.n++
	return nil, false, nil
}

// TestR_SIG_026_EveryRequestOfARunTakesFromTheBudget: the upstream of a run takes one of the budget before each request, sends none
// once it is spent, tells the observer each one, and a HEAD of an upstream without it is a bounded GET. Unit, microseconds.
func TestR_SIG_026_EveryRequestOfARunTakesFromTheBudget(t *testing.T) {
	up := &countingUpstream{}
	left := 3
	var seen []string
	l := &limited{
		Upstream: up,
		take: func() error {
			if left == 0 {
				return errBudget{after: time.Minute}
			}
			left--
			return nil
		},
		observe: observer(func(_, result string) { seen = append(seen, result) }, "oci"),
	}
	u, _ := url.Parse("https://registry.example.com/v2/a/referrers/sha256:x")
	ctx := context.Background()
	if _, err := l.Get(ctx, u, "", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Head(ctx, u, ""); err != nil {
		t.Fatal(err)
	}
	if _, _, err := l.Document(ctx, "a/_tags/x", ""); err != nil {
		t.Fatal(err)
	}
	if _, _, err := l.Artifact(ctx, "a/_blobs/x", 1); !errors.As(err, new(errBudget)) {
		t.Fatalf("a request beyond the budget: %v", err)
	}
	if up.n != 3 || l.sent != 3 || len(seen) != 3 {
		t.Errorf("%d requests reached the upstream, %d counted, %d observed; want 3", up.n, l.sent, len(seen))
	}
}

// TestR_SIG_026_AQueueWithoutJobsQueuesNothing: a replica that has not wired the fetch queues nothing and fails no commit; the job's
// arguments carry the repository and the target. Unit, microseconds.
func TestR_SIG_026_AQueueWithoutJobsQueuesNothing(t *testing.T) {
	var q *Queue
	if err := q.Enqueue(context.Background(), nil, 1, "oci", "a@sha256:x", [32]byte{}, false); err != nil {
		t.Errorf("a nil queue: %v", err)
	}
	if err := NewQueue(Options{}).Enqueue(context.Background(), nil, 1, "oci", "a@sha256:x", [32]byte{}, false); err != nil {
		t.Errorf("a queue without jobs: %v", err)
	}
	b, err := json.Marshal(Args{RepoID: 7, Target: "keys"})
	if err != nil || string(b) != `{"repoId":7,"target":"keys"}` {
		t.Errorf("args %s %v", b, err)
	}
	if Key(7, "keys") != "signature-fetch:7:keys" {
		t.Errorf("key %q", Key(7, "keys"))
	}
}
