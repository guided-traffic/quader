package blob_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/blob/blobtest"
	"github.com/guided-traffic/quader/internal/errs"
)

func TestR_STOR_026_TheParametersOfTheOrphanSweep(t *testing.T) {
	cfg := blobtest.Storage(t, t.TempDir())
	cfg.Upload.CommitDeadline = time.Hour
	field := func(err error) string {
		var e *errs.Error
		if !errors.As(err, &e) || e.Kind != errs.Invalid || len(e.Fields) == 0 {
			return ""
		}
		return e.Fields[0].Pointer
	}
	for _, c := range []struct {
		body, pointer string
	}{
		{`{"minAge": "1h"}`, "/minAge"}, // R-STOR-026: not greater than twice commit_deadline (1h)
		{`{"minAge": "2h"}`, "/minAge"},
		{`{"minAge": "a day"}`, "/minAge"},
		{`{"connection": "archive"}`, "/connection"},
		{`{"minAge": "2h1s", "connection": "default", "resume": false, "dryRun": true}`, ""},
		{`{}`, ""},
	} {
		a := blob.NewSweepJobArgs(cfg)
		if err := json.Unmarshal([]byte(c.body), a); err != nil {
			t.Fatal(err)
		}
		err := a.Validate()
		if got := field(err); got != c.pointer || (c.pointer == "" && err != nil) {
			t.Errorf("R-STOR-026 %s: %v (field %q), want field %q", c.body, err, got, c.pointer)
		}
	}
}

func TestR_STOR_026_TheSweepRefusesAMinAgeAtTheBound(t *testing.T) {
	cfg := blobtest.Storage(t, t.TempDir())
	c := blob.NewCollector(cfg, blob.SingleConnection("quader", blobtest.New()), nil)
	// a job that reached the table without the trigger's check: refused before the first call to the database or the store
	if _, err := c.Sweep(context.Background(), blob.SweepOptions{MinAge: 2 * cfg.Upload.CommitDeadline}); err == nil || !strings.Contains(err.Error(), "commit_deadline") {
		t.Fatalf("R-STOR-026 a sweep with minAge twice commit_deadline: %v", err)
	}
}

func TestR_STOR_026_TheResultsSerializeInTheShapeTheJobAPIShows(t *testing.T) {
	b, err := json.Marshal(blob.SweepResult{
		Connections: []blob.SweepConnectionResult{{Connection: "default", PrefixesDone: 256, PrefixesTotal: 256}},
		Skipped:     []blob.SweepSkip{{Connection: "secondary", Reason: "circuit_open"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{
		`"dryRun":false`, `"connection":"default"`, `"prefixesDone":256`, `"prefixesTotal":256`, `"objectsSeen":0`,
		`"orphansFound":0`, `"orphansDeleted":0`, `"bytesReclaimed":0`, `"adopted":0`, `"copiesMarked":0`, `"multipartAborted":0`,
		`"stagingDeleted":0`, `"foreignKeys":0`, `"skipped":[{"connection":"secondary","reason":"circuit_open"}]`, `"errors":0`,
	} {
		if !strings.Contains(string(b), k) {
			t.Errorf("R-STOR-026 missing %s in %s", k, b)
		}
	}
	c, _ := json.Marshal(blob.SweepCursor{Connection: "secondary", Prefix: "7f"})
	if string(c) != `{"connection":"secondary","prefix":"7f"}` {
		t.Fatalf("cursor %s", c)
	}
	g, _ := json.Marshal(blob.GCResult{CopiesReleased: 2})
	if !strings.Contains(string(g), `"copiesReleased":2`) {
		t.Fatalf("R-STOR-057 the collection result %s names no released copies", g)
	}
}
