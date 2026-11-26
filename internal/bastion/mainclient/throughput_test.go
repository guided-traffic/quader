package mainclient_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastion/mainclient"
	"github.com/guided-traffic/quader/internal/bastion/maintest"
)

// fakeThroughput records how the client asks the throughput of R-BAS-097; refuse, when set, refuses every admission.
type fakeThroughput struct {
	refuse                     error
	admitted, running, cancels atomic.Int32
	read                       atomic.Int64
}

func (f *fakeThroughput) Admit(context.Context, mainclient.Class, time.Time) (mainclient.Admission, error) {
	if f.refuse != nil {
		return nil, f.refuse
	}
	f.admitted.Add(1)
	return fakeAdmission{f}, nil
}

func (f *fakeThroughput) Running(context.Context, mainclient.Class) mainclient.Admission {
	f.running.Add(1)
	return fakeAdmission{f}
}

type fakeAdmission struct{ f *fakeThroughput }

func (a fakeAdmission) Pace(body io.ReadCloser) mainclient.PacedBody {
	return &countedBody{ReadCloser: body, f: a.f}
}
func (a fakeAdmission) Cancel() { a.f.cancels.Add(1) }

type countedBody struct {
	io.ReadCloser
	f *fakeThroughput
}

func (b *countedBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	b.f.read.Add(int64(n))
	return n, err
}
func (*countedBody) Waiting() bool         { return false }
func (*countedBody) Waited() time.Duration { return 0 }

// TestR_BAS_097_AForwardIsAdmittedAfterItsSlotAndItsBodyIsPaced: the client admits every forward through the throughput after
// its slot and reads the answer's body through the pacing; the continuation of a fill is paced without an admission; a miss the
// throughput refuses is not sent, gives its slot back and counts as busy.
func TestR_BAS_097_AForwardIsAdmittedAfterItsSlotAndItsBodyIsPaced(t *testing.T) {
	ft := &fakeThroughput{}
	r := newRig(t, func(o *mainclient.Options) {
		o.Throughput = ft
		o.Budget = mainclient.NewBudget(mainclient.BudgetOptions{MaxConcurrent: 1, Replicas: 1, AnonymousShare: 0.5, QueueTimeout: 50 * time.Millisecond})
	})
	ctx := context.Background()
	resp, err := r.c.Do(ctx, forward(http.MethodGet, "/api/npm/web/npm-all/x"))
	if err != nil {
		t.Fatal(err)
	}
	if resp.Pacing == nil {
		t.Fatal("the answer carries no pacing")
	}
	body := closeBody(t, resp)
	if ft.admitted.Load() != 1 || ft.read.Load() != int64(len(body)) {
		t.Fatalf("admitted %d, paced %d of %d bytes", ft.admitted.Load(), ft.read.Load(), len(body))
	}
	cont := forward(http.MethodGet, "/api/npm/web/npm-all/x")
	cont.RangeFrom = 1
	if resp, err = r.c.Do(ctx, cont); err != nil {
		t.Fatal(err)
	}
	closeBody(t, resp)
	if ft.running.Load() != 1 || ft.admitted.Load() != 1 {
		t.Fatalf("the continuation: running %d, admitted %d, want paced without an admission", ft.running.Load(), ft.admitted.Load())
	}

	ft.refuse = &mainclient.ThroughputError{RetryAfter: time.Second}
	before := len(r.main.Forwards())
	_, err = r.c.Do(ctx, forward(http.MethodGet, "/api/npm/web/npm-all/y"))
	var te *mainclient.ThroughputError
	if !errors.As(err, &te) || !errors.Is(err, mainclient.ErrBusy) {
		t.Fatalf("a refused admission: %v", err)
	}
	if len(r.main.Forwards()) != before {
		t.Fatal("a miss the throughput refused reached the main installation")
	}
	if n := maintest.Value(r.m.MainRequests.WithLabelValues("forward", "busy")); n != 1 {
		t.Fatalf("forward busy = %v, want 1", n)
	}
	// The only slot was given back.
	if _, err := r.c.Check(ctx, mainclient.Authenticated, iface.CheckRequest{Repositories: []string{"a"}}); err != nil {
		t.Fatalf("a check after the refused admission: %v", err)
	}
}
