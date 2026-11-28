package authz

import (
	"testing"

	"github.com/guided-traffic/quader/internal/errs"
)

// bastionPopulateFixture is a public remote repository that fills on read, npm-pubremote, and the public virtual repository over it,
// npm-vpubremote, both distributed by the Bastion partners; the bot reads everything.
func bastionPopulateFixture() *fixture {
	f := newFixture().
		repo("npm-pubremote", "remote", "npm", `{"remote":{"cacheFillAction":"read"}}`).
		repo("npm-vpubremote", "virtual", "npm", `{"virtual":{"members":["npm-pubremote"]}}`)
	f.levels("npm-pubremote", "public", "public").levels("npm-vpubremote", "public", "public")
	f.policy(policyDef{name: "bot-read", selectors: []string{"@any"}, entries: map[string][]string{idBot: {"read"}}})
	return f.distribute(bastionPartners, "npm-pubremote", "npm-vpubremote")
}

// TestR_AUTHZ_008_R_BAS_094_AnAnonymousCallerThroughABastionNeverPopulates: the anonymous caller reads a public remote repository
// through a Bastion that distributes it, but never makes it fetch from its upstream (bastion.md Q-BAS-531, H-2450): the populate check
// refuses it, a request with nothing cached is 403 populate_not_permitted, recorded as a refused populate only where anonymous
// denials are, and through a virtual repository the remote member is read from its cache only. The same anonymous caller at the main
// installation's own door, and a token holder through the Bastion, still populate. Milliseconds, an index of two repositories.
func TestR_AUTHZ_008_R_BAS_094_AnAnonymousCallerThroughABastionNeverPopulates(t *testing.T) {
	rec := &recorder{}
	e := newTestEngine(t, bastionPopulateFixture(), func(o *Options) { o.Recorder = rec })
	ctx := t.Context()
	anon := through(anonSub, bastionPartners)
	idx, err := e.Begin(ctx, anon)
	if err != nil {
		t.Fatal(err)
	}
	c := read("npm-pubremote", "left-pad", "left-pad/-/left-pad-1.3.0.tgz")
	cases := []struct {
		name   string
		sub    Subject
		want   Outcome
		reason string
	}{
		{"the anonymous caller through the Bastion reads but does not populate", anon, DenyVisible, ReasonPopulate},
		{"the anonymous caller at the main installation's door populates by the visibility level", anonSub, Allow, ReasonAllowVisibility},
		{"a token holder through the Bastion populates", bearer(through(botSub, bastionPartners)), Allow, ""},
	}
	for _, tc := range cases {
		t.Run("R-AUTHZ-008 "+tc.name, func(t *testing.T) {
			if d := e.MayPopulateOn(ctx, idx, tc.sub, c); d.Outcome != tc.want || (tc.reason != "" && d.Reason != tc.reason) {
				t.Fatalf("decision %+v, want outcome %d reason %q", d, tc.want, tc.reason)
			}
		})
	}
	t.Run("R-AUTHZ-008 the read itself is allowed", func(t *testing.T) {
		if d := e.Decide(ctx, anon, c); d.Outcome != Allow {
			t.Fatalf("read %+v", d)
		}
	})
	t.Run("R-AUTHZ-016 R-AUTHZ-029 a request with nothing cached is 403 populate_not_permitted, recorded as an anonymous denial", func(t *testing.T) {
		d := e.PopulateOn(ctx, idx, anon, c)
		if ee, ok := errs.As(d.Err()); !ok || ee.Status() != 403 || ee.Code != errs.CodePopulateNotPermitted {
			t.Fatalf("err = %v", d.Err())
		}
		// An anonymous denial is recorded only with audit.record_anonymous_denials, off by default: a flood of anonymous misses
		// writes no event per request.
		if evs := rec.list(); len(evs) != 0 {
			t.Fatalf("events %+v without record_anonymous_denials", evs)
		}
		withAnon := &recorder{}
		ea := newTestEngine(t, bastionPopulateFixture(), func(o *Options) { o.Recorder = withAnon; o.Audit.RecordAnonymousDenials = true })
		ia, err := ea.Begin(ctx, anon)
		if err != nil {
			t.Fatal(err)
		}
		ea.PopulateOn(ctx, ia, anon, c)
		if evs := withAnon.list(); len(evs) != 1 || evs[0].Type != EventDenied || evs[0].Reason != ReasonPopulate {
			t.Fatalf("events %+v with record_anonymous_denials", evs)
		}
	})
	t.Run("R-AUTHZ-015 through a virtual repository the remote member is read from its cache only", func(t *testing.T) {
		through := read("npm-vpubremote", "left-pad", "")
		if r, f := e.MemberUsableOn(ctx, idx, anon, qr("npm-pubremote"), through); !r || f {
			t.Fatalf("read %v fill %v, want true false", r, f)
		}
		if r, f := e.MemberUsableOn(ctx, idx, anonSub, qr("npm-pubremote"), through); !r || !f {
			t.Fatalf("at the main installation's door: read %v fill %v, want true true", r, f)
		}
	})
}
