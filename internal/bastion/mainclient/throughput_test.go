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

// catalogueThroughput is a fakeThroughput that also paces the catalogue's reads and counts them.
type catalogueThroughput struct {
	fakeThroughput
	catalogue atomic.Int32
}

func (f *catalogueThroughput) CatalogueReader(_ context.Context, body io.ReadCloser) mainclient.PacedBody {
	f.catalogue.Add(1)
	return &countedBody{ReadCloser: body, f: &f.fakeThroughput}
}

// TestR_BAS_109_TheCatalogueReadsArePacedInTheirClassAndTakeNoSlot: the public set, the change list and the batch of documents are
// read through the throughput's catalogue class, never admitted as a miss, and take no slot of the budget (R-BAS-097,
// R-BAS-109); the batch hands each frame over in the request's order.
func TestR_BAS_109_TheCatalogueReadsArePacedInTheirClassAndTakeNoSlot(t *testing.T) {
	ft := &catalogueThroughput{}
	r := newRig(t, func(o *mainclient.Options) {
		o.Throughput = ft
		o.Budget = mainclient.NewBudget(mainclient.BudgetOptions{MaxConcurrent: 1, Replicas: 1, AnonymousShare: 0.5, QueueTimeout: 50 * time.Millisecond})
	})
	r.main.SetConfiguration(func(c *iface.Configuration) { c.Switches.GUI = true })
	r.main.PutDocument("state", []byte(`{"a":1}`))
	r.main.PutDocument("signing/chain.pem", []byte("chain"))
	ctx := context.Background()
	if _, err := r.c.CataloguePublic(ctx); err != nil {
		t.Fatal(err)
	}
	list, err := r.c.CatalogueChanges(ctx, 0, 0)
	if err != nil || len(list.Entries) != 2 {
		t.Fatalf("the change list: %+v %v", list, err)
	}
	var got []string
	err = r.c.CatalogueDocuments(ctx, []string{"state", "signing/trusted-root.json", "signing/chain.pem"}, func(f iface.CatalogueFrame, gz []byte) error {
		got = append(got, f.Path)
		if f.Missing != (len(gz) == 0) {
			t.Errorf("%s: missing %v with %d bytes", f.Path, f.Missing, len(gz))
		}
		return nil
	})
	if err != nil || len(got) != 3 || got[1] != "signing/trusted-root.json" {
		t.Fatalf("the batch: %v %v", got, err)
	}
	if ft.catalogue.Load() != 3 || ft.admitted.Load() != 0 || ft.read.Load() == 0 {
		t.Fatalf("paced %d catalogue reads, admitted %d, read %d bytes", ft.catalogue.Load(), ft.admitted.Load(), ft.read.Load())
	}
	if n := maintest.Value(r.m.MainRequests.WithLabelValues("catalogue", "2xx")); n != 3 {
		t.Fatalf("catalogue 2xx = %v, want 3", n)
	}
	r.main.SetConfiguration(nil)
	if _, err := r.c.CatalogueChanges(ctx, 0, 0); !errors.Is(err, mainclient.ErrBadAnswer) {
		t.Fatalf("with gui off at the main installation: %v", err)
	}
}
