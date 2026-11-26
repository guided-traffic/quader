//go:build integration

package audit

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/audit"
)

// changeEvent is one stored event of a change, inserted with its own time.
type changeEvent struct {
	sec                                           float64
	action, category, resource, actorID, name, ip string
	details                                       map[string]any
}

// TestR_OPS_056_AChangeIsOneRowAndTheFiltersChooseItsEvents writes the events of a push (three files, their allowed writes and the
// version published), two system actors' deletes of one package at once and an upload written before the events named their unit, and
// reads them as rows: a push is one row with the version's event and its writes another, the two system actors are two callers, and a
// filter by action, category, kind of resource or actor keeps what it chose of an act as a row. Integration tier, about 1 s; 13 events of
// a few hundred bytes, inserted by SQL with their times.
func TestR_OPS_056_AChangeIsOneRowAndTheFiltersChooseItsEvents(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	t0 := time.Now().Add(-30 * time.Minute).Truncate(time.Millisecond)
	const alice, token = "5f0e1111-2222-4333-8444-555566667711", "c41d1111-2222-4333-8444-555566667711"
	file := func(path string, size int) map[string]any {
		return map[string]any{"path": path, "sha256": sha(size), "size": size, "overwritten": false, "package": "acme/app", "unit": "acme/app"}
	}
	write := map[string]any{"package": "acme/app", "unit": "acme/app", "reason": "allow_grant"}
	events := []changeEvent{
		{0, audit.EventArtifactDeployed, "content", "repository", alice, "alice", "10.0.0.5", file("acme/app/_blobs/sha256:"+sha(1), 100)},
		{0, "authz.write.allowed", "authz", "repository", alice, "alice", "10.0.0.5", write},
		{1, audit.EventArtifactDeployed, "content", "repository", alice, "alice", "10.0.0.5", file("acme/app/_blobs/sha256:"+sha(2), 200)},
		{1, "authz.write.allowed", "authz", "repository", alice, "alice", "10.0.0.5", write},
		{2, audit.EventArtifactDeployed, "content", "repository", alice, "alice", "10.0.0.5", file("acme/app/_tags/1.4.0", 30)},
		{2, "authz.write.allowed", "authz", "repository", alice, "alice", "10.0.0.5", write},
		{2, audit.EventPackagePublished, "content", "package", alice, "alice", "10.0.0.5", map[string]any{
			"package": "acme/app", "version": "1.4.0",
			"format": "oci", "unit": "acme/app", "files": []map[string]any{{"path": "acme/app", "sha256": sha(3), "size": 30}},
		}},
		{5, audit.EventArtifactDeleted, "content", "repository", "", "removal", "", map[string]any{"path": "foo/1.0/foo.bin", "sha256": sha(4), "size": 4, "package": "foo", "unit": "foo"}},
		{5, audit.EventArtifactDeleted, "content", "repository", "", "retention", "", map[string]any{"path": "foo/1.1/foo.bin", "sha256": sha(5), "size": 5, "package": "foo", "unit": "foo"}},
		{6, audit.EventArtifactDeleted, "content", "repository", "", "removal", "", map[string]any{"path": "foo/2.0/foo.bin", "sha256": sha(6), "size": 6, "package": "foo", "unit": "foo"}},
		{7, audit.EventArtifactDeployed, "content", "repository", alice, "alice", "10.0.0.5", map[string]any{"path": "old.bin", "sha256": sha(7), "size": 7, "overwritten": false}},
	}
	ids := make([]string, len(events))
	for i, ev := range events {
		raw, _ := json.Marshal(ev.details)
		at := t0.Add(time.Duration(ev.sec*float64(time.Second)) + time.Duration(i)*time.Millisecond)
		kind, actorID, tok := "user", any(ev.actorID), any(token)
		if ev.actorID == "" {
			kind, actorID, tok = "system", nil, nil
		}
		var ip any
		if ev.ip != "" {
			ip = ev.ip
		}
		err := e.admin.QueryRow(ctx, `INSERT INTO quader.audit_event (id, occurred_at, action, category, outcome, actor_type, actor_id, actor_name,
				token_id, source_ip, resource_type, resource_id, repository, project, path, details)
			VALUES ($12::uuid, $1, $2, $3, 'success', $4, $5, $6, $7, $8::inet, $9, 'platform/images', 'platform/images', 'platform', $10, $11)
			RETURNING id::text`, at, ev.action, ev.category, kind, actorID, ev.name, tok, ip, ev.resource, ev.details["path"], raw, uuid7(at)).Scan(&ids[i])
		if err != nil {
			t.Fatal(err)
		}
	}
	store := audit.NewStore(e.open())
	read := func(f audit.Filter) map[string]string {
		f.From, f.To = t0.Add(-time.Minute), t0.Add(time.Hour)
		out := map[string]string{}
		for _, r := range rowsAll(t, store, f, 2) {
			if r.Group == nil {
				out["event "+r.Event.ID] = r.Event.Action
				continue
			}
			out[fmt.Sprintf("%s %s %s", r.Group.Act, r.Event.Actor.Name, r.Group.Unit)] = fmt.Sprintf("%d %d %v %s", r.Group.Items,
				r.Group.Size, r.Group.SizeKnown, r.Group.Version)
		}
		return out
	}
	check := func(name string, f audit.Filter, want map[string]string) {
		t.Run(name, func(t *testing.T) {
			got := read(f)
			if len(got) != len(want) {
				t.Fatalf("rows %v, want %v", got, want)
			}
			for k, v := range want {
				if got[k] != v {
					t.Errorf("row %q: %q, want %q (rows %v)", k, got[k], v, got)
				}
			}
		})
	}
	check("R-OPS-056 a push is one row with its version, its writes another, two system actors are two callers", audit.Filter{}, map[string]string{
		"push alice acme/app": "4 330 true 1.4.0", "writes alice acme/app": "3 0 false ",
		"delete removal foo": "2 10 true ", "delete retention foo": "1 5 true ", "event " + ids[10]: audit.EventArtifactDeployed,
	})
	check("R-OPS-056 a filter by action keeps the version's event as a row of its own act", audit.Filter{Action: audit.EventPackagePublished},
		map[string]string{"push alice acme/app": "1 0 false 1.4.0"})
	check("R-OPS-056 a filter by category keeps the writes", audit.Filter{Category: "authz"}, map[string]string{"writes alice acme/app": "3 0 false "})
	check("R-OPS-056 a filter by the kind of resource keeps the files of the push, an upload without its version", audit.Filter{
		ResourceType: "repository",
		Actor:        alice,
	}, map[string]string{"upload alice acme/app": "3 330 true ", "writes alice acme/app": "3 0 false ", "event " + ids[10]: audit.EventArtifactDeployed})
	check("R-OPS-017 a filter by an actor's name finds a system actor's events through its name", audit.Filter{Actor: "removal"},
		map[string]string{"delete removal foo": "2 10 true "})

	t.Run("R-OPS-056 a row's events are every event of its act, whatever filter showed the row", func(t *testing.T) {
		res, err := store.GroupItems(ctx, ids[0], audit.Page{Limit: 100})
		if err != nil || len(res.Items) != 4 || res.Items[3].ID != ids[6] {
			t.Fatalf("the push's events %+v, %v", res.Items, err)
		}
		if _, err := store.GroupItems(ctx, ids[1], audit.Page{}); err != nil {
			t.Errorf("the writes are a row of their own: %v", err)
		}
		if _, err := store.GroupItems(ctx, ids[2], audit.Page{}); err == nil {
			t.Error("the push's second file begins no row")
		}
	})
}

// uuid7 is a UUIDv7 of the time at, as the writer makes an event's ID: the read of one event looks for it around the time it carries.
func uuid7(at time.Time) string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	ms := uint64(at.UnixMilli()) //nolint:gosec // after 1970
	for i := range 6 {
		b[i] = byte(ms >> (40 - 8*i))
	}
	b[6] = b[6]&0x0f | 0x70
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b[:])
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}
