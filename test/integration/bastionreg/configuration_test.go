//go:build integration

package bastionreg

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastionmain"
	"github.com/guided-traffic/quader/internal/limits"
	"github.com/guided-traffic/quader/internal/limits/store"
	"github.com/guided-traffic/quader/internal/limits/usage"
)

// TestR_BAS_093_TheConfigurationLivesOnTheRegistration: a new registration holds the limits of migration 00147, which are
// bastionmain.DefaultLimits, and the five kind rules of the preset Internet; the member limits needs limits:write (a security
// administrator with endpoints:write is refused) and its change raises the revision, records bastion.configuration.changed and reaches
// the replica's snapshot, from which the handshake answers; a preset is one request; a change of a rule of the place, or of a profile
// a rule of the place names, raises the revision of that registration and of no other (Q-BAS-320). Rows only, about 3 s.
func TestR_BAS_093_TheConfigurationLivesOnTheRegistration(t *testing.T) {
	e := newEnv(t)
	r, c := e.newCast("web-1", replicaOpts{})
	created := r.call("POST", bastions, c.admin, map[string]any{"name": "partners"})
	want(t, created, http.StatusCreated, "")
	bid := e.text(`SELECT id::text FROM principal WHERE kind = 'bastion' AND name = 'partners'`)
	got := created.json(t)
	if !sameJSON(t, got["limits"], limitsDoc(bastionmain.DefaultLimits(), iface.PresetInternet)) {
		t.Fatalf("a new registration's limits: %v, want DefaultLimits", got["limits"])
	}
	if n := e.number(`SELECT count(*) FROM limit_assignment a JOIN limit_profile p ON p.id = a.profile_id
	                   WHERE a.bastion_id = $1::uuid AND a.who_kind = 'every' AND a.builtin
	                     AND p.key = CASE a.who_ref WHEN 'anonymous' THEN 'bastion-anonymous' ELSE 'bastion-signed-in' END`, bid); n != 5 {
		t.Fatalf("%d kind rules of the preset Internet, want 5", n)
	}
	snap := bastionmain.NewRegistrationSnapshot(r.db, r.caches, quiet)
	if err := snap.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = snap.Stop(context.Background()) })
	revision := func() int64 { return e.number(`SELECT revision FROM principal WHERE id = $1::uuid`, bid) }
	config := func() iface.Configuration {
		c, _, _ := snap.Configuration(bid)
		return c
	}
	waitFor(t, "the snapshot's configuration of the new registration", 5*time.Second, func() bool {
		cfg, ok, err := snap.Configuration(bid)
		return ok && err == nil && cfg.Revision == revision() && cfg.Limits.Preset == iface.PresetInternet
	})

	t.Run("R-BAS-093 the limits need limits:write, the other members endpoints:write", func(t *testing.T) {
		want(t, r.call("PATCH", bastions+"/partners", c.security, map[string]any{"limits": map[string]any{"mode": "audit"}}, "If-Match", "*"), http.StatusForbidden, "")
		want(t, r.call("PATCH", bastions+"/partners", c.auditor, map[string]any{"description": "x"}, "If-Match", "*"), http.StatusForbidden, "")
		want(t, r.call("PATCH", bastions+"/partners", c.security, map[string]any{"description": "The partner network"}, "If-Match", "*"), http.StatusOK, "")
	})

	t.Run("R-BAS-093 a change of the limits raises the revision and reaches the snapshot", func(t *testing.T) {
		before := revision()
		p := r.call("PATCH", bastions+"/partners", c.admin, map[string]any{"limits": map[string]any{
			"mode": "audit", "anonymousCeiling": map[string]any{"requestsPerSecond": 10, "burst": 20},
		}}, "If-Match", "*")
		want(t, p, http.StatusOK, "")
		if revision() <= before {
			t.Fatalf("the revision stayed %d", before)
		}
		ev := e.events(audit.EventBastionConfigurationChanged)
		if len(ev) == 0 || ev[len(ev)-1].ResourceID != bid || !hasParts(ev[len(ev)-1].Details, "mode", "anonymousCeiling") {
			t.Fatalf("bastion.configuration.changed = %+v", ev)
		}
		waitFor(t, "the snapshot of the change", 5*time.Second, func() bool {
			cfg := config()
			return cfg.Limits.Mode == iface.ModeAudit && cfg.Limits.AnonymousCeiling.RequestsPerHour != nil && *cfg.Limits.AnonymousCeiling.Burst == 20
		})
		want(t, r.call("PATCH", bastions+"/partners", c.admin, map[string]any{"limits": map[string]any{"slots": map[string]any{"anonymousShare": 2}}}, "If-Match", "*"),
			http.StatusUnprocessableEntity, "")
	})

	t.Run("R-LIM-046 a preset is applied in one request", func(t *testing.T) {
		p := r.call("PATCH", bastions+"/partners", c.admin, map[string]any{"limits": map[string]any{"preset": "partnerNetwork"}}, "If-Match", "*")
		want(t, p, http.StatusOK, "")
		lim := p.json(t)["limits"].(map[string]any)
		if lim["preset"] != "partnerNetwork" || lim["mode"] != "enforce" {
			t.Fatalf("after the preset: %v", lim)
		}
		if k := e.text(`SELECT p.key FROM limit_assignment a JOIN limit_profile p ON p.id = a.profile_id
		                 WHERE a.bastion_id = $1::uuid AND a.who_kind = 'every' AND a.who_ref = 'user'`, bid); k != limits.ProfileStandard {
			t.Fatalf("the kind rule of users names %s, want standard", k)
		}
		if ev := e.events(audit.EventBastionConfigurationChanged); !hasParts(ev[len(ev)-1].Details, "preset", "mode", "rules") {
			t.Fatalf("bastion.configuration.changed = %+v", ev[len(ev)-1])
		}
	})

	st := store.New(store.Options{Runner: r.db, Changes: r.changes, Audit: r.writer})
	actor := audit.Actor{Kind: "user", Name: "admin"}
	t.Run("R-LIM-043 a rule of the place raises the revision and reaches the handshake", func(t *testing.T) {
		before := revision()
		rule := limits.Rule{Place: limits.BastionPlace(bid, "partners"), WhoKind: limits.WhoAddress, WhoRef: "198.51.100.0/24", Profile: limits.ProfileRestricted}
		if err := st.CreateRule(context.Background(), actor, &rule); err != nil {
			t.Fatal(err)
		}
		if revision() <= before {
			t.Fatal("the revision stayed")
		}
		waitFor(t, "the address rule in the snapshot", 5*time.Second, func() bool {
			cfg := config()
			return cfg.Revision == revision() && len(cfg.Limits.Place.AddressRules) == 1 && cfg.Limits.Place.AddressRules[0].Rule == rule.ID
		})
		r.flush()
		if ev := e.events(audit.EventBastionConfigurationChanged); !hasParts(ev[len(ev)-1].Details, "rules") {
			t.Fatalf("bastion.configuration.changed = %+v", ev[len(ev)-1])
		}
		if n := e.number(`SELECT count(*) FROM audit_event WHERE action = 'limits.assignment.created' AND details ->> 'place' = 'bastion:partners'`); n != 1 {
			t.Fatalf("limits.assignment.created at bastion:partners = %d", n)
		}
	})

	t.Run("R-LIM-043 a profile's change raises the revision of the registrations that name it and of no other", func(t *testing.T) {
		want(t, r.call("POST", bastions, c.admin, map[string]any{"name": "standby"}), http.StatusCreated, "")
		want(t, r.call("PATCH", bastions+"/standby", c.admin, map[string]any{"limits": map[string]any{"preset": "internal"}}, "If-Match", "*"), http.StatusOK, "")
		sid := e.text(`SELECT id::text FROM principal WHERE kind = 'bastion' AND name = 'standby'`)
		partners, standby := revision(), e.number(`SELECT revision FROM principal WHERE id = $1::uuid`, sid)
		snapAPI, err := st.Snapshot(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		var std limits.Profile
		for _, p := range snapAPI.Profiles {
			if p.Key == limits.ProfileStandard {
				std = p
			}
		}
		std.Figures[limits.RequestsPerHour]++
		if err := st.UpdateProfile(context.Background(), actor, std.Key, std.Version, std); err != nil {
			t.Fatal(err)
		}
		if revision() <= partners {
			t.Fatal("the revision of partners, whose place names standard, stayed")
		}
		if got := e.number(`SELECT revision FROM principal WHERE id = $1::uuid`, sid); got != standby {
			t.Fatalf("the revision of standby, whose place names no standard, moved from %d to %d", standby, got)
		}
	})
}

// TestR_LIM_045_ABatchsUsageAndStateAreStored: a batch of three usage rows and one overflow row reaches usage_bucket at the place
// bastion:<id> through the usage buffer, and its state replaces the replica's row of bastion_status, which the registration shows
// with the blocks that have not ended, beside the edge figure its contact reported (R-BAS-093). Rows only, under a second.
func TestR_LIM_045_ABatchsUsageAndStateAreStored(t *testing.T) {
	e := newEnv(t)
	r, c := e.newCast("web-1", replicaOpts{})
	want(t, r.call("POST", bastions, c.admin, map[string]any{"name": "partners"}), http.StatusCreated, "")
	bid := e.text(`SELECT id::text FROM principal WHERE kind = 'bastion' AND name = 'partners'`)
	user := e.text(`SELECT id::text FROM principal WHERE kind = 'user' LIMIT 1`)
	ctx := context.Background()
	rec := bastionmain.Recognition{Bastion: "partners", BastionID: bid, Replica: "quader-bastion-0", Interface: iface.InterfaceVersion}
	edge, kept := iface.EdgeLimit{RequestsPerSecond: 200, Connections: 1000}, true
	if err := bastionmain.NewContacts(bastionmain.ContactOptions{DB: r.db}).Record(ctx, rec, bastionmain.Report{
		UserAgent: "quader-bastion/0.1.0", EdgeLimit: &edge, CatalogueKept: &kept,
	}); err != nil {
		t.Fatal(err)
	}
	buf := usage.NewBuffer(usage.Options{Runner: r.db, Interval: time.Hour})
	in := bastionmain.NewIntake(bastionmain.IntakeOptions{DB: r.db, MaxBatch: 10, MaxAge: 24 * time.Hour, Usage: bastionmain.UsageOptions{Sink: buf, Writer: r.db}})
	now := time.Now().UTC()
	bucket := now.Truncate(5 * time.Minute)
	req := iface.DownloadsRequest{
		Events: []iface.DownloadEvent{},
		Usage: iface.UsageReport{BucketSeconds: 300, Rows: []iface.UsageRow{
			{BucketStart: bucket, Subject: "user:" + user, Credential: "0192f0d1-1c2a-7c3e-8a51-6f7d2b9c4e41", Repository: "web/npm", Requests: 812, CachedBytes: 4096, Limited: map[string]int64{"cachedBytesPerHour": 1}},
			{BucketStart: bucket, Subject: "anonymous:198.51.100.7", Repository: "web/npm", Requests: 3},
			{BucketStart: bucket, Subject: "technical_token:0192f0d1-1c2a-7c3e-8a51-6f7d2b9c4e42", Repository: "web/npm", Requests: 1, WouldLimit: map[string]int64{"requestsPerHour": 2}},
		}, Overflow: []iface.UsageOverflow{{BucketStart: bucket, SubjectKind: "anonymous", Repository: "web/npm", Subjects: 3112, Requests: 41000}}},
		Status: iface.ReplicaStatus{Replica: "quader-bastion-0", At: now, Blocks: []iface.StatusBlock{
			{Address: "203.0.113.0/24", Until: now.Add(10 * time.Minute), Reason: "refusals"},
			{Address: "203.0.114.0/24", Until: now.Add(-time.Minute), Reason: "refusals"},
		}},
	}
	out, err := in.Accept(ctx, rec, req)
	if err != nil || out.UsageDropped != 0 {
		t.Fatalf("Accept: %+v %v", out, err)
	}
	if err := buf.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	place := limits.BastionPlaceKeyPrefix + bid
	if n := e.number(`SELECT count(*) FROM usage_bucket WHERE granularity = '5m' AND place = $1`, place); n != 4 {
		t.Fatalf("%d five-minute rows at %s, want 4", n, place)
	}
	if n := e.number(`SELECT requests FROM usage_bucket WHERE granularity = '1h' AND place = $1 AND subject_ref = 'overflow'`, place); n != 41000 {
		t.Fatalf("the overflow row's hour holds %d requests", n)
	}
	if n := e.number(`SELECT count(*) FROM usage_limit_event WHERE place = $1 AND granularity = '5m'`, place); n != 2 {
		t.Fatalf("%d limit events, want 2 (one limited, one would-limit)", n)
	}
	reg := r.call("GET", bastions+"/partners", c.admin, nil)
	want(t, reg, http.StatusOK, "")
	var doc struct {
		Status   []iface.ReplicaStatus `json:"status"`
		Contacts []struct {
			EdgeLimit     *string `json:"edgeLimit"`
			CatalogueKept *bool   `json:"catalogueKept"`
		} `json:"contacts"`
	}
	if err := json.Unmarshal(reg.Body, &doc); err != nil || len(doc.Status) != 1 || len(doc.Status[0].Blocks) != 1 || doc.Status[0].Blocks[0].Address != "203.0.113.0/24" {
		t.Fatalf("the registration's status: %s", reg.Body)
	}
	if c := doc.Contacts; len(c) != 1 || c[0].EdgeLimit == nil || *c[0].EdgeLimit != edge.Header() || c[0].CatalogueKept == nil || !*c[0].CatalogueKept {
		t.Fatalf("the contact's edge figure and catalogue: %s", reg.Body)
	}
}

// limitsDoc is the member limits of the API for a registration's own limits and the preset it reads.
func limitsDoc(l bastionmain.Limits, preset iface.LimitsPreset) map[string]any {
	return map[string]any{
		"mode": l.Mode, "preset": preset, "flood": l.Flood, "slots": l.Slots, "throughput": l.Throughput, "anonymousCeiling": l.AnonymousCeiling,
	}
}

// sameJSON compares two values by their JSON.
func sameJSON(t *testing.T, a, b any) bool {
	t.Helper()
	norm := func(v any) string {
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		var x any
		_ = json.Unmarshal(raw, &x)
		out, _ := json.Marshal(x)
		return string(out)
	}
	return norm(a) == norm(b)
}

// hasParts reports whether the details of bastion.configuration.changed name every part.
func hasParts(d map[string]any, parts ...string) bool {
	raw, _ := d["parts"].([]any)
	var have []string
	for _, p := range raw {
		s, _ := p.(string)
		have = append(have, s)
	}
	for _, p := range parts {
		if !slices.Contains(have, p) {
			return false
		}
	}
	return strings.Join(have, ",") != ""
}
