package reconcile

import (
	"bytes"
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"

	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/blob/blobtest"
)

func TestR_STOR_056_TheJournalPassCopiesWhatOneConnectionLacks(t *testing.T) {
	a, b := blobtest.New(), blobtest.New()
	ca, cb := conn("primary", a), conn("secondary", b)
	entry := func(i byte) []byte { return bytes.Repeat([]byte{'{', i, '}'}, 40) }
	dir := "_journal/revocations"
	// a holds 1 and 3, b holds 2 and 3; an object above the bound of an entry is no entry and stays where it is
	a.Set("quader/"+dir+"/2026/11/22/1.json", entry(1), time.Now())
	a.Set("quader/"+dir+"/2026/11/22/3.json", entry(3), time.Now())
	b.Set("quader/"+dir+"/2026/11/22/2.json", entry(2), time.Now())
	b.Set("quader/"+dir+"/2026/11/22/3.json", entry(3), time.Now())
	b.Set("quader/"+dir+"/2026/11/22/big.json", make([]byte, maxJournalObject+1), time.Now())
	a.Set("quader/sha256/aa/bb/not-the-journal", []byte("x"), time.Now())
	n, err := copyJournal(context.Background(), []*blob.Connection{ca, cb}, dir, quiet)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("copied %d, want 2 (1 to secondary, 2 to primary)", n)
	}
	for _, k := range []string{"1", "2", "3"} {
		ga, _ := a.Data("quader/" + dir + "/2026/11/22/" + k + ".json")
		gb, _ := b.Data("quader/" + dir + "/2026/11/22/" + k + ".json")
		if len(ga) == 0 || !bytes.Equal(ga, gb) {
			t.Fatalf("entry %s: %d and %d bytes", k, len(ga), len(gb))
		}
	}
	if _, ok := a.Data("quader/" + dir + "/2026/11/22/big.json"); ok {
		t.Fatal("an object above the bound of an entry was copied")
	}
	if _, ok := b.Data("quader/sha256/aa/bb/not-the-journal"); ok {
		t.Fatal("an object outside the journal was copied")
	}
	if a.Count("put") != 1 || b.Count("put") != 1 {
		t.Fatalf("puts %d and %d, want 1 each", a.Count("put"), b.Count("put"))
	}
}

func TestR_STOR_056_TheGaugesOfTheQueueAndOfAFillingAndADetachedConnection(t *testing.T) {
	g := NewGauges()
	got := map[string]float64{}
	for _, m := range g.Metrics([]GaugeRow{{Connection: "default", Queued: 0}, {Connection: "secondary", Queued: 7, OldestSeconds: 61.5, Filling: true}, {Connection: "archive", Detached: true}}) {
		var pb dto.Metric
		if err := m.Write(&pb); err != nil {
			t.Fatal(err)
		}
		got[descName(m.Desc())+"/"+pb.GetLabel()[0].GetValue()] = pb.GetGauge().GetValue()
	}
	want := map[string]float64{
		MetricQueue + "/default": 0, MetricQueueOldest + "/default": 0, MetricFilling + "/default": 0,
		MetricQueue + "/secondary": 7, MetricQueueOldest + "/secondary": 61.5, MetricFilling + "/secondary": 1,
		MetricDetached + "/default": 0, MetricDetached + "/secondary": 0, MetricDetached + "/archive": 1, MetricFilling + "/archive": 0,
	}
	for k, v := range want {
		if gv, ok := got[k]; !ok || gv != v {
			t.Errorf("%s = %v (%v), want %v", k, gv, ok, v)
		}
	}
}

func descName(d *prometheus.Desc) string {
	_, rest, _ := strings.Cut(d.String(), `fqName: "`)
	name, _, _ := strings.Cut(rest, `"`)
	return name
}

func TestR_STOR_056_TheSignalFoldsTheCommitsIntoOneEnqueue(t *testing.T) {
	var n atomic.Int32
	done := make(chan struct{}, 4)
	s := NewSignal(func(context.Context) error { n.Add(1); done <- struct{}{}; return nil }, quiet)
	for range 3 {
		s.Notify() // before Start: one pending request
	}
	if err := s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("no enqueue")
	}
	s.Notify() // within the spacing: waits for it, then the one enqueue it stands for
	if err := s.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := n.Load(); got != 1 {
		t.Fatalf("%d enqueues, want 1 before the stop", got)
	}
}
