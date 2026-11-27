package v1

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/api/v1/gen"
	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/blob/history"
	"github.com/guided-traffic/quader/internal/blob/summary"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/meta"
)

// historyAPIConfig is the test's API configuration with a body limit, which the acknowledgement's optional body is read against.
func historyAPIConfig() config.API {
	c := apiConfigForTest()
	c.MaxRequestBodyBytes = 1 << 20
	return c
}

// Tests of the history of the stored content in the API (storage.md R-STOR-111) over in-memory records: the cells, the problems and
// the findings in the document's shape, their paging, the acknowledgement with its audit event, and the overview's period. The
// derivation is internal/blob/history's unit test; the statements are the integration test's.

// historyFake holds the records the services answer and what they were asked.
type historyFake struct {
	problems   history.ProblemRecords
	cells      history.CellRecords
	findings   []history.Finding
	queries    []history.FindingQuery
	periods    []history.Period
	from       []time.Time
	acked      map[int64]string
	resolvedID int64
}

func (f *historyFake) services(views []summary.View) StorageServices {
	return StorageServices{
		Connections: func(context.Context) ([]summary.View, error) { return views, nil },
		Problems: func(_ context.Context, from, _ time.Time) (history.ProblemRecords, error) {
			f.from = append(f.from, from)
			return f.problems, nil
		},
		Cells: func(_ context.Context, p history.Period, _ time.Time) (history.CellRecords, error) {
			f.periods = append(f.periods, p)
			return f.cells, nil
		},
		Findings: func(_ context.Context, q history.FindingQuery) ([]history.Finding, error) {
			f.queries = append(f.queries, q)
			var out []history.Finding
			for _, x := range f.findings {
				if q.AfterID != 0 && x.ID >= q.AfterID {
					continue
				}
				out = append(out, x)
				if len(out) == q.Limit {
					break
				}
			}
			return out, nil
		},
		Acknowledge: func(_ context.Context, _ meta.Tx, id int64, by, note string) (history.Finding, error) {
			switch id {
			case f.resolvedID:
				return history.Finding{}, history.ErrFindingResolved
			case f.findings[0].ID:
				if f.acked == nil {
					f.acked = map[int64]string{}
				}
				f.acked[id] = by + "|" + note
				x := f.findings[0]
				x.Acknowledged, x.AcknowledgedBy, x.ByName, x.Note = time.Now(), by, "alice", note
				return x, nil
			}
			return history.Finding{}, history.ErrNoFinding
		},
	}
}

func historyFixture(now time.Time) *historyFake {
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	d := damagedDigest(0x5b)
	return &historyFake{
		problems: history.ProblemRecords{
			Intervals: []history.Interval{
				{Connection: "secondary", Kind: history.KindUnreachable, Replica: "web-0", Started: ago(50 * time.Hour), Ended: ago(49 * time.Hour), Code: "timeout", Occurrences: 2},
				{Connection: "secondary", Kind: history.KindUnreachable, Replica: "web-1", Started: ago(49*time.Hour + 30*time.Minute), Ended: ago(48 * time.Hour), Code: "timeout", Occurrences: 1},
			},
			Findings: []history.DayFindings{{
				Connection: "default", Day: ago(20 * time.Hour).Truncate(24 * time.Hour), N: 3, Bytes: 3072, First: ago(20 * time.Hour),
				LastResolved: ago(19 * time.Hour), Resolved: 3, Sources: []string{"scrub"}, Reasons: []string{"digest"},
			}},
		},
		cells: history.CellRecords{
			Counts:   []history.CellCount{{Connection: "secondary", Kind: "read_fallover", Cell: 2, N: 310}},
			Findings: []history.CellFindings{{Connection: "default", Cell: 29, N: 3, Bytes: 3072}},
			Totals:   []history.FindingTotal{{Connection: "default", Source: "scrub", Outcome: "rewritten", N: 3, Bytes: 3072}},
		},
		findings: []history.Finding{
			{
				ID: 42, Digest: d, Connection: "default", Status: "corrupt", Source: "download", Reason: "digest", StoredSize: 1024, BlobSize: 1024,
				Detected: ago(2 * time.Hour), Items: []history.Item{{Project: "web", Repository: "npm-local", Path: "lodash/-/lodash-4.17.21.tgz"}}, ItemCount: 3,
			},
			{
				ID: 41, Digest: d, Connection: "default", Status: "corrupt", Source: "scrub", Reason: "digest", StoredSize: 1024, BlobSize: 1024,
				Detected: ago(20 * time.Hour), Resolved: ago(19 * time.Hour), Resolution: "rewritten", Items: []history.Item{},
			},
		},
		resolvedID: 41,
	}
}

func TestR_STOR_111_TheHistoryInTheAPI(t *testing.T) {
	val := newValidator(t)
	now := time.Now().UTC()
	sum, views := storageFixture()
	f := historyFixture(now)
	services := f.services(views)
	services.Overview = func(context.Context) (summary.OverviewInput, error) {
		return summary.OverviewInput{Views: views, Latest: &sum, MinCopies: 1, DownloadVerify: true}, nil
	}
	store, events := &mirrorStore{}, &recordedEvents{}
	s := newSetup(t, func(o *Options) {
		o.API.Config = historyAPIConfig()
		o.API.Store, o.API.Audit = store, events
		o.Storage = services
	}, authed)
	h := val.Handler(t, s.handler)
	get := func(path string, into any) string {
		t.Helper()
		w := httpRecorder(h, http.MethodGet, path)
		if w.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
		if err := json.Unmarshal(w.Body.Bytes(), into); err != nil {
			t.Fatal(err)
		}
		return w.Body.String()
	}

	t.Run("R-STOR-111 the cells of 30 days by default, 24 of an hour for 24h, every connection with its findings' totals", func(t *testing.T) {
		var out gen.StorageHistory
		get("/api/v1/storage/history", &out)
		if out.Period != "30d" || out.BucketSeconds != 86400 || len(out.Connections) != 2 || len(out.Connections[0].Buckets) != 30 {
			t.Fatalf("history = %+v", out)
		}
		def, sec := out.Connections[0], out.Connections[1]
		if def.Id != "default" || def.Buckets[29].DamagedCopies != 3 || def.Buckets[29].Tone != "warning" || def.Findings.ByOutcome["rewritten"] != 3 ||
			def.Findings.BySource["scrub"] != 3 || sec.Buckets[2].ReadFallovers != 310 || sec.Buckets[0].Tone != "none" {
			t.Errorf("connections = %+v", out.Connections)
		}
		get("/api/v1/storage/history?period=24h", &out)
		if out.Period != "24h" || len(out.Connections[0].Buckets) != 24 || out.BucketSeconds != 3600 || f.periods[len(f.periods)-1].Name != "24h" {
			t.Errorf("24h = %+v", out)
		}
	})

	t.Run("R-STOR-111 the problems newest first, paged by the cursor, narrowed by kind, connection and state", func(t *testing.T) {
		var page gen.StorageProblemList
		get("/api/v1/storage/problems?limit=1", &page)
		if len(page.Items) != 1 || page.NextCursor == nil || page.Items[0].Kind != "copies_damaged" || page.Items[0].Count != 3 || page.Items[0].EndedAt == nil {
			t.Fatalf("first page = %+v", page)
		}
		get("/api/v1/storage/problems?limit=1&cursor="+*page.NextCursor, &page)
		p := page.Items[0]
		if len(page.Items) != 1 || page.NextCursor != nil || p.Kind != "unreachable" || p.Connection != "secondary" || p.Replicas != 2 || p.Occurrences != 3 ||
			p.Seconds != 7200 || p.Tone != "warning" || len(p.Codes) != 1 {
			t.Errorf("second page = %+v", page)
		}
		get("/api/v1/storage/problems?kind=unreachable&connection=secondary&state=ended", &page)
		if len(page.Items) != 1 || page.Items[0].Kind != "unreachable" {
			t.Errorf("narrowed = %+v", page)
		}
		get("/api/v1/storage/problems?state=ongoing", &page)
		if len(page.Items) != 0 {
			t.Errorf("ongoing = %+v", page)
		}
		get("/api/v1/storage/problems?period=24h", &page)
		if len(page.Items) != 2 || !f.from[len(f.from)-1].Before(now.Add(-23*time.Hour)) || f.from[len(f.from)-1].Before(now.Add(-25*time.Hour)) {
			t.Errorf("the period's start = %v", f.from[len(f.from)-1])
		}
	})

	t.Run("R-STOR-111 the findings paged with their items, the state and the connection handed on", func(t *testing.T) {
		var page gen.StorageFindingList
		get("/api/v1/storage/findings?limit=1&state=open&connection=default", &page)
		if len(page.Items) != 1 || page.NextCursor == nil {
			t.Fatalf("first page = %+v", page)
		}
		fi := page.Items[0]
		if fi.Id != 42 || fi.Source != "download" || fi.ResolvedAt != nil || fi.Resolution != nil || fi.Acknowledged != nil || fi.ItemCount != 3 ||
			len(fi.Items) != 1 || fi.Items[0].Repository != "npm-local" {
			t.Errorf("finding = %+v", fi)
		}
		q := f.queries[len(f.queries)-1]
		if q.State != history.FindingOpen || q.Connection != "default" || q.Limit != 2 || q.AfterID != 0 || q.From.After(now.Add(-29*24*time.Hour)) {
			t.Errorf("query = %+v", q)
		}
		get("/api/v1/storage/findings?limit=1&state=open&connection=default&cursor="+*page.NextCursor, &page)
		q = f.queries[len(f.queries)-1]
		if q.AfterID != 42 || !q.AfterDetected.Equal(f.findings[0].Detected.Truncate(time.Microsecond)) || len(page.Items) != 1 || page.Items[0].Id != 41 ||
			*page.Items[0].Resolution != "rewritten" {
			t.Errorf("second page %+v, query %+v", page, q)
		}
	})

	t.Run("R-STOR-111 a period, a kind or a state that is none, or a cursor of another list, answers 400", func(t *testing.T) {
		for _, p := range []string{
			"/api/v1/storage/history?period=2w", "/api/v1/storage/overview?period=week", "/api/v1/storage/problems?kind=fallover",
			"/api/v1/storage/findings?state=closed", "/api/v1/storage/problems?cursor=bm9wZQ", "/api/v1/storage/findings?connection=Default",
		} {
			if w := httpRecorder(h, http.MethodGet, p); w.Code != http.StatusBadRequest {
				t.Errorf("%s: %d %s", p, w.Code, w.Body.String())
			}
		}
	})

	t.Run("R-STOR-107 R-STOR-111 the overview names the problems of its period and the worst of them", func(t *testing.T) {
		var out gen.StorageOverview
		get("/api/v1/storage/overview?period=7d", &out)
		pr := out.Problems
		if pr.Period != "7d" || pr.Count != 2 || pr.State != "ongoing" || pr.Worst == nil || pr.Worst.Kind != "unreachable" || pr.Worst.Seconds != 7200 {
			t.Errorf("problems = %+v (secondary is unreachable now on a replica, so ongoing)", pr)
		}
		if len(out.Encryption.ProviderApplied) != 1 || out.Encryption.ProviderApplied[0] != "default" {
			t.Errorf("encryption = %+v", out.Encryption)
		}
	})

	post := func(handler http.Handler, path, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		handler.ServeHTTP(w, req)
		return w
	}

	t.Run("R-STOR-111 an acknowledgement answers the finding, never resolves it, and is audited in its transaction", func(t *testing.T) {
		before, commits := len(events.events), store.commits
		w := post(h, "/api/v1/storage/findings/42/acknowledge", `{"note":"The lifecycle rule removed it."}`)
		if w.Code != http.StatusOK {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
		var out gen.StorageFinding
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		if out.Acknowledged == nil || out.Acknowledged.By == nil || out.Acknowledged.By.Id != callerUUID || out.Acknowledged.Note != "The lifecycle rule removed it." ||
			out.ResolvedAt != nil || f.acked[42] != callerUUID+"|The lifecycle rule removed it." {
			t.Errorf("finding = %s", w.Body.String())
		}
		if len(events.events) != before+1 || store.commits != commits+1 {
			t.Fatalf("events %d, commits %d", len(events.events)-before, store.commits-commits)
		}
		ev := events.events[len(events.events)-1]
		want := `{"connection":"default","finding":42,"note":"The lifecycle rule removed it.","sha256":"` + damagedDigest(0x5b).Hex() + `"}`
		if ev.Type != audit.EventStorageFindingAcknowledged || ev.Subject.Kind != "storage_finding" || ev.Subject.ID != "42" || ev.Actor.ID != callerUUID ||
			string(ev.Details) != want {
			t.Errorf("event = %+v, details %s", ev, ev.Details)
		}
		if err := matchesEventSchema(ev.Type, ev.Details); err != nil {
			t.Errorf("the details do not match the registered schema: %v", err)
		}
		if w := post(h, "/api/v1/storage/findings/42/acknowledge", ""); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"note":""`) {
			t.Errorf("without a body: %d %s", w.Code, w.Body.String())
		}
	})

	t.Run("R-STOR-111 a resolved finding answers 409 finding_resolved, an unknown one 404, a long note 422, and none is audited", func(t *testing.T) {
		before := len(events.events)
		if w := post(h, "/api/v1/storage/findings/41/acknowledge", `{}`); w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "finding_resolved") {
			t.Errorf("resolved: %d %s", w.Code, w.Body.String())
		}
		if w := post(h, "/api/v1/storage/findings/7/acknowledge", `{}`); w.Code != http.StatusNotFound {
			t.Errorf("unknown: %d %s", w.Code, w.Body.String())
		}
		if w := post(h, "/api/v1/storage/findings/42/acknowledge", `{"note":"`+strings.Repeat("x", 501)+`"}`); w.Code != http.StatusUnprocessableEntity &&
			w.Code != http.StatusBadRequest {
			t.Errorf("long note: %d %s", w.Code, w.Body.String())
		}
		if w := post(h, "/api/v1/storage/findings/42/acknowledge", `{"resolve":true}`); w.Code != http.StatusBadRequest {
			t.Errorf("unknown member: %d %s", w.Code, w.Body.String())
		}
		if len(events.events) != before {
			t.Errorf("%d events for refused acknowledgements", len(events.events)-before)
		}
		failing := newSetup(t, func(o *Options) {
			o.API.Config = historyAPIConfig()
			o.API.Store, o.API.Audit = store, failingRecorder{}
			o.Storage = services
		}, authed)
		if w := post(failing.handler, "/api/v1/storage/findings/42/acknowledge", `{}`); w.Code != http.StatusInternalServerError {
			t.Errorf("an acknowledgement the audit log does not hold: %d %s", w.Code, w.Body.String())
		}
	})

	t.Run("R-STOR-111 the reads need settings:read and the acknowledgement settings:write; a refused caller reads nothing; 503 without the services", func(t *testing.T) {
		for op, perm := range map[string]string{
			"getStorageHistory": "settings:read", "listStorageProblems": "settings:read", "listStorageFindings": "settings:read",
			"acknowledgeStorageFinding": "settings:write",
		} {
			if r := requirementOf(t, op); r.Permission != perm {
				t.Errorf("%s: %+v", op, r)
			}
		}
		denied := newSetup(t, func(o *Options) {
			o.API.Config = historyAPIConfig()
			o.Access = denyAccess{}
			o.API.Store, o.API.Audit = store, events
			o.Storage = services
		}, authed)
		reads, acks := len(f.queries)+len(f.periods)+len(f.from), len(f.acked)
		for _, p := range []string{"/api/v1/storage/history", "/api/v1/storage/problems", "/api/v1/storage/findings"} {
			if w := denied.get(p); w.Code != http.StatusForbidden {
				t.Errorf("%s: %d", p, w.Code)
			}
		}
		if w := post(denied.handler, "/api/v1/storage/findings/42/acknowledge", `{}`); w.Code != http.StatusForbidden {
			t.Errorf("acknowledge: %d", w.Code)
		}
		if len(f.queries)+len(f.periods)+len(f.from) != reads || len(f.acked) != acks {
			t.Error("a refused caller's request reached the records")
		}
		bare := newSetup(t, func(o *Options) { o.API.Config = historyAPIConfig() }, authed)
		for _, p := range []string{"/api/v1/storage/history", "/api/v1/storage/problems", "/api/v1/storage/findings"} {
			if w := bare.get(p); w.Code != http.StatusServiceUnavailable {
				t.Errorf("%s: %d", p, w.Code)
			}
		}
		if w := post(bare.handler, "/api/v1/storage/findings/42/acknowledge", `{}`); w.Code != http.StatusServiceUnavailable {
			t.Errorf("acknowledge: %d", w.Code)
		}
	})
}
