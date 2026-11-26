//go:build integration

package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/meta"
)

// rowsWorld is the audit trail of R-OPS-055's test: what the screen is to show as rows, written by two replicas.
type rowsWorld struct {
	store *audit.Store
	t0    time.Time
	// ids of the events of each act, in the order they were written.
	pull, pullAgain, anon, ui, leftPad []string
	refusal, old, created              string
	all                                int
}

// download is one artifact.downloaded the way items.Store.Served writes it from R-OPS-054 on; unit "" writes one of before.
func download(at time.Time, actor audit.Actor, ip, repo, path, sha string, size int, pkg, version, unit string) audit.Event {
	d := map[string]any{"path": path, "sha256": sha}
	if unit != "" {
		d["size"], d["package"], d["unit"] = size, pkg, unit
		if version != "" {
			d["version"] = version
		}
	}
	raw, _ := json.Marshal(d)
	return audit.Event{
		Type: audit.EventArtifactDownloaded, Actor: actor, Outcome: audit.Success, At: at,
		Source:  audit.Source{ClientAddr: ip, UserAgent: "docker/27.3.1", TraceID: fmt.Sprintf("%032x", at.UnixNano())},
		Subject: audit.Subject{Kind: "repository", Name: repo}, Repository: repo, Project: strings.Split(repo, "/")[0], Path: path,
		Details: raw,
	}
}

func sha(i int) string { return fmt.Sprintf("%064x", i) }

// downloadsCfg is the writers' configuration with audit.record_downloads on.
func downloadsCfg() config.Audit {
	c := auditCfg()
	c.RecordDownloads = true
	return c
}

// newRowsWorld writes, through two replicas, a pull of an image of 13 items by alice (its tag manifest and twelve tiny layers, the
// layers served in turn by both replicas), an anonymous pull of the same image beside it, an npm install of two packages by bob, a
// refusal, an event written before the events named their unit, a repository's creation, and alice's second pull of the image by
// digest 90 seconds after her first ended.
func newRowsWorld(t *testing.T, e *env) *rowsWorld {
	t.Helper()
	db := e.open()
	mk := func(replica string) *audit.Writer {
		w, err := audit.New(audit.Options{DB: db, Replica: replica, Config: downloadsCfg(), Metrics: &metrics{}, Log: quiet, AppLog: quiet})
		if err != nil {
			t.Fatal(err)
		}
		w.Start()
		return w
	}
	a, b := mk("quader-packages-a"), mk("quader-packages-b")
	ctx := context.Background()
	w := &rowsWorld{store: audit.NewStore(db), t0: time.Now().Add(-time.Hour).Truncate(time.Millisecond)}
	alice := audit.Actor{Kind: "user", ID: "5f0e1111-2222-4333-8444-555566667701", Name: "alice", TokenID: "c41d1111-2222-4333-8444-555566667701", AuthMethod: "bearer"}
	bob := audit.Actor{Kind: "user", ID: "5f0e1111-2222-4333-8444-555566667702", Name: "bob", AuthMethod: "session"}
	anon := audit.Actor{Kind: "anonymous"}
	rec := func(wr *audit.Writer, ev audit.Event) string {
		// The event's ID is made in Record; the trace names the event, and the read finds it by that.
		wr.Record(ctx, ev)
		w.all++
		return ev.Source.TraceID
	}
	s := func(sec float64) time.Time { return w.t0.Add(time.Duration(sec * float64(time.Second))) }
	w.pull = append(w.pull, rec(a, download(s(0), alice, "10.0.0.5", "platform/images", "acme/app/_tags/1.4.0", sha(1), 1574, "acme/app", "1.4.0", "acme/app")))
	for i := range 12 {
		r := a
		if i%2 == 1 {
			r = b
		}
		w.pull = append(w.pull, rec(r, download(s(1+float64(i)), alice, "10.0.0.5", "platform/images", "acme/app/_blobs/sha256:"+sha(100+i), sha(100+i), 512+i, "acme/app", "", "acme/app")))
		if i == 2 {
			// beside the pull, the same image anonymously from another address: another caller, another row
			for j := range 4 {
				w.anon = append(w.anon, rec(b, download(s(3.5+float64(j)), anon, "10.0.0.9", "platform/images", "acme/app/_blobs/sha256:"+sha(200+j), sha(200+j), 64, "acme/app", "", "acme/app")))
			}
		}
	}
	w.ui = append(w.ui, rec(a, download(s(5.2), bob, "10.0.0.7", "web/npm-remote", "@acme/ui/-/ui-2.4.0.tgz", sha(300), 4821, "@acme/ui", "2.4.0", "@acme/ui@2.4.0")))
	w.leftPad = append(w.leftPad, rec(b, download(s(5.3), bob, "10.0.0.7", "web/npm-remote", "left-pad/-/left-pad-1.3.0.tgz", sha(301), 1200, "left-pad", "1.3.0", "left-pad@1.3.0")))
	refusal := download(s(7.1), bob, "10.0.0.7", "web/npm-remote", "evil/-/evil-1.0.0.tgz", sha(302), 0, "", "", "")
	refusal.Type, refusal.Outcome, refusal.Details = "malware.download_blocked", audit.Denied, json.RawMessage(`{"path":"evil/-/evil-1.0.0.tgz","sha256":"`+sha(302)+`","reason":"infected"}`)
	w.refusal = rec(a, refusal)
	w.old = rec(b, download(s(8.4), alice, "10.0.0.5", "platform/images", "acme/old/_blobs/sha256:"+sha(400), sha(400), 0, "", "", ""))
	created := audit.Event{
		Type: "probe.hit.logged", Actor: bob, Outcome: audit.Success, At: s(9.3),
		Source: audit.Source{TraceID: fmt.Sprintf("%032x", s(9.3).UnixNano())}, Subject: audit.Subject{Kind: "repository", Name: "web/new"},
	}
	w.created = rec(a, created)
	// The second pull: 90 s after the first one's last layer (s(12)), by digest, so it names no version.
	for i := range 3 {
		w.pullAgain = append(w.pullAgain, rec(b, download(s(102+float64(i)), alice, "10.0.0.5", "platform/images", "acme/app/_blobs/sha256:"+sha(500+i), sha(500+i), 10, "acme/app", "", "acme/app")))
	}
	for _, wr := range []*audit.Writer{a, b} {
		fctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		if err := wr.Flush(fctx); err != nil {
			t.Fatal(err)
		}
		cancel()
	}
	// The read below finds the events by their trace; the IDs come from the table.
	byTrace := map[string]string{}
	rows, err := e.admin.Query(ctx, `SELECT trace_id, id::text FROM quader.audit_event`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var tr, id string
		if err := rows.Scan(&tr, &id); err != nil {
			t.Fatal(err)
		}
		byTrace[tr] = id
	}
	rows.Close()
	ids := func(traces []string) []string {
		out := make([]string, len(traces))
		for i, tr := range traces {
			out[i] = byTrace[tr]
		}
		return out
	}
	w.pull, w.pullAgain, w.anon, w.ui, w.leftPad = ids(w.pull), ids(w.pullAgain), ids(w.anon), ids(w.ui), ids(w.leftPad)
	w.refusal, w.old, w.created = byTrace[w.refusal], byTrace[w.old], byTrace[w.created]
	return w
}

func (w *rowsWorld) filter() audit.Filter {
	return audit.Filter{From: w.t0.Add(-time.Minute), To: w.t0.Add(time.Hour)}
}

// rowsAll walks every page of the rows.
func rowsAll(t *testing.T, s *audit.Store, f audit.Filter, limit int) []audit.Row {
	t.Helper()
	var all []audit.Row
	var after *audit.Cursor
	for range 100 {
		res, err := s.Rows(context.Background(), f, audit.Page{Limit: limit, After: after})
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Items) > limit || (res.Next != nil && len(res.Items) != limit) {
			t.Fatalf("a page of %d rows for limit %d (next %v)", len(res.Items), limit, res.Next)
		}
		all = append(all, res.Items...)
		if res.Next == nil {
			return all
		}
		after = res.Next
	}
	t.Fatal("the paging does not end")
	return nil
}

// shape is a row as the test compares it: its first event and, of a download, its items' IDs.
func shape(r audit.Row) string {
	if r.Group == nil {
		return r.Event.ID
	}
	ids := make([]string, 0, len(r.Group.First))
	for _, it := range r.Group.First {
		ids = append(ids, it.ID)
	}
	return fmt.Sprintf("%s:%d:%s", r.Event.ID, r.Group.Items, strings.Join(ids, ","))
}

func TestR_OPS_055_ADownloadIsOneRowAndItsEventsStayOnePerItem(t *testing.T) {
	e := newEnv(t)
	w := newRowsWorld(t, e)
	ctx := context.Background()

	t.Run("R-OPS-054 the events stay one per item: the events list holds every item", func(t *testing.T) {
		events := pageAll(t, w.store, w.filter(), 1000, false)
		if len(events) != w.all {
			t.Fatalf("the events list holds %d events, %d were written", len(events), w.all)
		}
	})

	rows := rowsAll(t, w.store, w.filter(), 100)
	byFirst := map[string]audit.Row{}
	for _, r := range rows {
		byFirst[r.Event.ID] = r
	}
	t.Run("R-OPS-055 each act is one row, newest first by its first event", func(t *testing.T) {
		want := []string{w.pullAgain[0], w.created, w.old, w.refusal, w.leftPad[0], w.ui[0], w.anon[0], w.pull[0]}
		got := make([]string, len(rows))
		for i, r := range rows {
			got[i] = r.Event.ID
		}
		if !slices.Equal(got, want) {
			t.Fatalf("rows %v, want %v", got, want)
		}
	})
	t.Run("R-OPS-055 a pull's row: its items over both replicas, its tag, its size, its first ten items", func(t *testing.T) {
		d := byFirst[w.pull[0]].Group
		size := 1574
		for i := range 12 {
			size += 512 + i
		}
		if d == nil || d.Items != 13 || d.More || d.Unit != "acme/app" || d.Package != "acme/app" || d.Version != "1.4.0" ||
			!d.SizeKnown || d.Size != int64(size) || len(d.First) != audit.DownloadPreview || !d.Last.Equal(w.t0.Add(12*time.Second)) {
			t.Fatalf("the pull's row: %+v", d)
		}
		for i, it := range d.First {
			if it.ID != w.pull[i] {
				t.Fatalf("item %d is %s, want %s", i, it.ID, w.pull[i])
			}
		}
		if d.First[1].Replica != "quader-packages-a" || d.First[2].Replica != "quader-packages-b" {
			t.Errorf("the items' replicas: %q %q", d.First[1].Replica, d.First[2].Replica)
		}
	})
	t.Run("R-OPS-055 another caller of the same image, two packages of one install, a pull a pause later are rows of their own", func(t *testing.T) {
		if d := byFirst[w.anon[0]].Group; d == nil || d.Items != 4 || d.Version != "" {
			t.Errorf("the anonymous pull: %+v", d)
		}
		for _, id := range []string{w.ui[0], w.leftPad[0]} {
			if d := byFirst[id].Group; d == nil || d.Items != 1 || d.Version == "" {
				t.Errorf("an npm package: %+v", d)
			}
		}
		if d := byFirst[w.pullAgain[0]].Group; d == nil || d.Items != 3 || d.Version != "" {
			t.Errorf("the pull by digest: %+v", d)
		}
	})
	t.Run("R-OPS-055 a refusal, an event written before the unit and any other event are single rows", func(t *testing.T) {
		for _, id := range []string{w.refusal, w.old, w.created} {
			if r, ok := byFirst[id]; !ok || r.Group != nil {
				t.Errorf("event %s: %+v", id, r)
			}
		}
	})
	t.Run("R-OPS-055 no page cuts a row: every page size gives the same rows", func(t *testing.T) {
		want := make([]string, len(rows))
		for i, r := range rows {
			want[i] = shape(r)
		}
		for _, limit := range []int{1, 2, 3, 5} {
			got := rowsAll(t, w.store, w.filter(), limit)
			shapes := make([]string, len(got))
			for i, r := range got {
				shapes[i] = shape(r)
			}
			if !slices.Equal(shapes, want) {
				t.Errorf("limit %d: %v, want %v", limit, shapes, want)
			}
		}
	})
	t.Run("R-OPS-055 the filters choose the events, the rows group what they chose", func(t *testing.T) {
		f := w.filter()
		f.Actor = "alice"
		got := rowsAll(t, w.store, f, 100)
		if len(got) != 3 || got[0].Event.ID != w.pullAgain[0] || got[1].Event.ID != w.old || got[2].Group.Items != 13 {
			t.Errorf("alice's rows: %d", len(got))
		}
		f = w.filter()
		f.Outcome = audit.Denied
		if got := rowsAll(t, w.store, f, 100); len(got) != 1 || got[0].Event.ID != w.refusal {
			t.Errorf("the denied rows: %d", len(got))
		}
		f = w.filter()
		f.TraceID = fmt.Sprintf("%032x", w.t0.Add(4*time.Second).UnixNano()) // the pull's fourth layer
		if got := rowsAll(t, w.store, f, 100); len(got) != 1 || got[0].Group == nil || got[0].Group.Items != 1 ||
			got[0].Event.ID != w.pull[4] {
			t.Errorf("the rows of one trace: %+v", got)
		}
		// A range that starts inside the pull shows its items within the range, from the first one there.
		f = w.filter()
		f.From = w.t0.Add(5500 * time.Millisecond)
		found := false
		for _, r := range rowsAll(t, w.store, f, 100) {
			if r.Event.ID == w.pull[6] {
				found = true
				if r.Group == nil || r.Group.Items != 7 {
					t.Errorf("the pull from 5.5 s on: %+v", r.Group)
				}
			}
		}
		if !found {
			t.Error("the pull from 5.5 s on has no row that begins with its first item in the range")
		}
	})
	t.Run("R-OPS-055 the same events form the same rows, also after newer events and a grown download", func(t *testing.T) {
		db := e.open()
		wr, err := audit.New(audit.Options{DB: db, Replica: "quader-packages-c", Config: downloadsCfg(), Metrics: &metrics{}, Log: quiet, AppLog: quiet})
		if err != nil {
			t.Fatal(err)
		}
		wr.Start()
		alice := byFirst[w.pullAgain[0]].Event.Actor
		alice.Kind = "user"
		grown := download(w.t0.Add(104*time.Second+30*time.Second), alice, "10.0.0.5", "platform/images", "acme/app/_blobs/sha256:"+sha(600), sha(600), 10, "acme/app", "", "acme/app")
		wr.Record(ctx, grown)
		wr.Record(ctx, audit.Event{Type: "probe.hit.logged", Actor: audit.Actor{Kind: "user", Name: "cy"}, Outcome: audit.Success, At: w.t0.Add(20 * time.Minute)})
		fctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		if err := wr.Flush(fctx); err != nil {
			t.Fatal(err)
		}
		again := rowsAll(t, w.store, w.filter(), 100)
		if len(again) != len(rows)+1 {
			t.Fatalf("%d rows, want %d", len(again), len(rows)+1)
		}
		for i, r := range again[1:] {
			old := rows[i]
			if r.Event.ID != old.Event.ID {
				t.Fatalf("row %d begins with %s, before %s", i, r.Event.ID, old.Event.ID)
			}
			if r.Event.ID == w.pullAgain[0] {
				if r.Group.Items != 4 {
					t.Errorf("the grown pull holds %d items, want 4", r.Group.Items)
				}
			} else if shape(r) != shape(old) {
				t.Errorf("row %d changed: %s, before %s", i, shape(r), shape(old))
			}
		}
	})
	t.Run("R-OPS-055 a download's events: every item, oldest first, paged; nothing else is a download", func(t *testing.T) {
		var got []string
		var after *audit.Cursor
		for range 10 {
			res, err := w.store.GroupItems(ctx, w.pull[0], audit.Page{Limit: 5, After: after})
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range res.Items {
				got = append(got, e.ID)
			}
			if res.Next == nil {
				break
			}
			after = res.Next
		}
		if !slices.Equal(got, w.pull) {
			t.Fatalf("the pull's events: %v, want %v", got, w.pull)
		}
		for _, id := range []string{w.pull[1], w.old, w.refusal, w.created, "01933b1e-7c2a-7f4e-9d1b-6a2f0c9e8b11", "x"} {
			if _, err := w.store.GroupItems(ctx, id, audit.Page{}); errs.KindOf(err) != errs.NotFound {
				t.Errorf("%s: %v, want not found", id, err)
			}
		}
		// A cursor of another download is no item of this one.
		other := byFirst[w.anon[0]].Group.First[1]
		for _, c := range []audit.Cursor{{OccurredAt: other.OccurredAt, ID: other.ID}, {OccurredAt: other.OccurredAt, ID: w.anon[0]}} {
			if _, err := w.store.GroupItems(ctx, w.pull[0], audit.Page{After: &c}); errs.KindOf(err) != errs.Invalid {
				t.Errorf("a cursor of another download %v: %v", c, err)
			}
		}
	})
	t.Run("R-OPS-055 the data keys handed to a Bastion for one pull are one row of their own, beside its downloads", func(t *testing.T) {
		// bastion.data_key.handed is transactional: its time is the transaction's, now.
		since := time.Now().Add(-time.Second)
		db := e.open()
		wr, err := audit.New(audit.Options{DB: db, Replica: "quader-packages-a", Config: downloadsCfg(), Metrics: &metrics{}, Log: quiet, AppLog: quiet})
		if err != nil {
			t.Fatal(err)
		}
		wr.Start()
		priya := audit.Actor{Kind: "user", ID: "5f0e1111-2222-4333-8444-555566667709", Name: "priya", TokenID: "c41d1111-2222-4333-8444-555566667709", AuthMethod: "bearer"}
		for i := range 3 {
			d := download(time.Time{}, priya, "10.0.0.20", "platform/images", "acme/app/_blobs/sha256:"+sha(700+i), sha(700+i), 0, "acme/app", "", "acme/app")
			key := d
			key.Type, key.Subject = audit.EventBastionDataKeyHanded, audit.Subject{Kind: "bastion", ID: "0193b4a0-0000-7000-8000-00000000b001", Name: "dmz"}
			key.Details = json.RawMessage(`{"bastion":"dmz","replica":"","sha256":"` + sha(700+i) + `","connection":"archive","storageKeyId":"k1",` +
				`"linkExpiresAt":"2026-11-26T12:00:00Z","package":"acme/app","unit":"acme/app"}`)
			if err := db.InTx(ctx, func(tx meta.Tx) error { return wr.RecordTx(ctx, tx, key) }); err != nil {
				t.Fatal(err)
			}
			d.At = time.Now()
			wr.Record(ctx, d)
		}
		fctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		if err := wr.Flush(fctx); err != nil {
			t.Fatal(err)
		}
		got := rowsAll(t, w.store, audit.Filter{From: since, To: time.Now().Add(time.Minute), Actor: "priya"}, 100)
		if len(got) != 2 {
			t.Fatalf("%d rows, want the keys' and the downloads'", len(got))
		}
		actions := map[string]int{}
		for _, r := range got {
			if r.Group == nil {
				t.Fatalf("a row of no download: %+v", r.Event)
			}
			actions[r.Event.Action] = r.Group.Items
		}
		if actions[audit.EventBastionDataKeyHanded] != 3 || actions[audit.EventArtifactDownloaded] != 3 {
			t.Errorf("rows %v", actions)
		}
	})
}
