package authz

import (
	"testing"

	"github.com/guided-traffic/quader/internal/auth/principal"
	"github.com/guided-traffic/quader/internal/formats/catalog"
	"github.com/guided-traffic/quader/internal/qname"
	"github.com/guided-traffic/quader/internal/wellknown"
)

// certSub is the certificate subject of an endpoint with auth.client_certificates, as edge.SubjectOf builds it.
func certSub(endpoint string) Subject {
	return Subject{Kind: principal.KindCertificate, Name: "CN=scan-01.example.com", AuthMethod: "client_certificate", Endpoint: endpoint, CertSerial: "a1b2c"}
}

var upClamav = qname.Repo{Project: wellknown.ProjectKey, Key: wellknown.ClamavKey}

// certificateFixture: upstream/clamav at a level with its switch on or off, and a local repository of the project it at internal
// that admits no certificate reader.
func certificateFixture(level string, on bool) *fixture {
	f := newFixture().repo("clamav", "local", "clamav", "").repo("generic-local", "local", "generic", "")
	f.project(pUpstream, wellknown.ProjectKey).assign("clamav", pUpstream)
	f.levels("clamav", level, level).levels("generic-local", "internal", "internal")
	for i := range f.s.Repositories {
		if f.s.Repositories[i].Key == "clamav" {
			f.s.Repositories[i].ClientsEnabled = on
		}
	}
	return f
}

// certificateRegistry is the fixtures' registry with the format clamav, whose declaration lets clients in (wellknown.Declared).
func certificateRegistry() *catalog.Registry {
	r := testRegistry()
	r.Register(catalog.Entry{Facts: catalog.Facts{ID: "clamav", Classes: []catalog.Class{catalog.Local}, Layout: "clamav", PatternSubject: catalog.SubjectPath, SystemOnly: true}, Exposure: catalog.Exposure{Exposable: true}})
	return r
}

// TestR_CLAMAV_010_TheCertificateSubjectsRule: read of upstream/clamav at internal and public while its switch is on is admitted; at
// private, while off, of another repository, a write, populate and a capability are refused with DenyHidden; an exposure rule that
// hides upstream/clamav refuses it; the subject holds no role and is not internal (R-AUTHZ-002, R-AUTHZ-030, Q-CLAMAV-9, Q-CLAMAV-13).
func TestR_CLAMAV_010_TheCertificateSubjectsRule(t *testing.T) {
	ctx := t.Context()
	decide := func(t *testing.T, f *fixture, mod func(*Options), sub Subject, c Coordinate) Decision {
		t.Helper()
		e := newTestEngine(t, f, func(o *Options) {
			o.Formats = certificateRegistry()
			if mod != nil {
				mod(o)
			}
		})
		idx, err := e.Begin(ctx, sub)
		if err != nil {
			t.Fatal(err)
		}
		return e.DecideOn(ctx, idx, sub, c)
	}
	readClamav := Coordinate{Repository: upClamav, Kind: KindMetadata, Path: "daily/28190/daily.cvd", Actions: []Action{Read}}
	for _, tc := range []struct {
		name  string
		level string
		on    bool
		c     Coordinate
		want  Outcome
	}{
		{"read at internal while on", "internal", true, readClamav, Allow},
		{"read at public while on", "public", true, readClamav, Allow},
		{"read at private while on", "private", true, readClamav, DenyHidden},
		{"read while off", "internal", false, readClamav, DenyHidden},
		{"read of another repository at internal", "internal", true, read("generic-local", "", "a.txt"), DenyHidden},
		{"a write", "public", true, Coordinate{Repository: upClamav, Kind: KindWrite, Path: "daily/1/daily.cvd", Actions: []Action{Deploy}}, DenyHidden},
		{"delete", "public", true, Coordinate{Repository: upClamav, Kind: KindWrite, Path: "daily/1/daily.cvd", Actions: []Action{Delete}}, DenyHidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if d := decide(t, certificateFixture(tc.level, tc.on), nil, certSub("default"), tc.c); d.Outcome != tc.want {
				t.Fatalf("decision %+v, want %d", d, tc.want)
			}
		})
	}
	t.Run("R-EP-013 an endpoint whose exposure rules do not name upstream/clamav refuses it", func(t *testing.T) {
		ceiling := func(o *Options) {
			o.Ceiling = NewExposureCeiling(map[string]string{"default": ExposureModeAll, "clamav": ExposureModeRules}, certificateRegistry())
		}
		if d := decide(t, certificateFixture("internal", true), ceiling, certSub("clamav"), readClamav); d.Outcome == Allow {
			t.Fatalf("decision %+v", d)
		}
		f := certificateFixture("internal", true).exposure(ruleDef{endpoint: "clamav", name: "definitions", repo: "clamav"})
		if d := decide(t, f, ceiling, certSub("clamav"), readClamav); d.Outcome != Allow {
			t.Fatalf("with a rule that names it: %+v", d)
		}
	})
	t.Run("R-AUTHZ-002 it holds no capability and no role and is not internal", func(t *testing.T) {
		e := newTestEngine(t, certificateFixture("internal", true), func(o *Options) { o.Formats = certificateRegistry() })
		sub := certSub("default")
		idx, err := e.Begin(ctx, sub)
		if err != nil {
			t.Fatal(err)
		}
		if e.HoldsOn(idx, sub, string(SettingsRead)) || sub.Internal() || idx.rolesOf(sub) != 0 {
			t.Fatal("the certificate subject holds something")
		}
		if d := e.PopulateOn(ctx, idx, sub, Coordinate{Repository: upClamav, Kind: KindArtifact, Path: "daily/1/daily.cvd", Actions: []Action{Read}}); d.Outcome == Allow {
			t.Fatalf("populate: %+v", d)
		}
		keys, err := e.ReadableOn(ctx, idx, sub, []qname.Repo{upClamav, qr("generic-local")})
		if err != nil || len(keys) != 1 || keys[0] != upClamav {
			t.Fatalf("readable %v, %v", keys, err)
		}
	})
}
