package token

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// The record of a technical token (R-AUTH-045, Q-AUTH-415, Q-AUTH-416).

func TestR_AUTH_045_TheNameOfATechnicalTokenIsTheSystemThatUsesIt(t *testing.T) {
	long := "nightly-mirror-of-the-customer-portal-artifacts-for-the-dr-site"
	for _, name := range []string{"nightly-mirror", "a", "release-2", long} {
		if err := validTechnical(name, ""); err != nil {
			t.Errorf("%q: %v", name, err)
		}
	}
	for _, name := range []string{"", "-mirror", "Nightly", "nightly mirror", "a_b", long + "x"} {
		err := validTechnical(name, "")
		if e, ok := errs.As(err); !ok || len(e.Fields) != 1 || e.Fields[0].Pointer != "/name" {
			t.Errorf("%q: %v", name, err)
		}
	}
	description := make([]byte, 1025)
	for i := range description {
		description[i] = 'x'
	}
	if e, ok := errs.As(validTechnical("ok", string(description))); !ok || e.Fields[0].Pointer != "/description" {
		t.Error("a description of 1025 characters is accepted")
	}
}

func TestR_AUTH_045_ASecretOfATechnicalTokenIsCreatedAsOne(t *testing.T) {
	if err := notTechnical(CreateRequest{Type: TypeSAT}); err == nil {
		t.Fatal("a service-account token is created without its technical token")
	}
	if err := notTechnical(CreateRequest{Type: TypePAT}); err != nil {
		t.Fatal(err)
	}
	r := secretRequest(CreateRequest{Type: TypePAT, Description: "x"}, "nightly-mirror")
	if r.Type != TypeSAT || r.Description != "nightly-mirror" {
		t.Fatalf("%+v", r)
	}
}

func TestR_AUTH_045_TheStateAndTheSecretsTheRecordShows(t *testing.T) {
	at := func(d time.Duration) pgtype.Timestamptz {
		return pgtype.Timestamptz{Time: time.Now().Add(d), Valid: true}
	}
	row := func(id string, live bool, revoked bool) q.IdentityTechnicalTokenSecretsRow {
		r := q.IdentityTechnicalTokenSecretsRow{ID: id, Hint: "qdr_sat_" + id, Scope: []byte(`{"inherit":true}`), CreatedAt: at(-time.Hour), ExpiresAt: at(time.Hour), Live: live}
		if revoked {
			r.RevokedAt = at(-time.Minute)
		}
		return r
	}
	t.Run("the live secrets of a rotation's overlap, newest first", func(t *testing.T) {
		got, state := secretsOf([]q.IdentityTechnicalTokenSecretsRow{row("b", true, false), row("a", true, false), row("old", false, true)})
		if state != StateActive || len(got) != 2 || got[0].ID != "b" {
			t.Fatalf("%s %+v", state, got)
		}
	})
	t.Run("a token whose last secret was revoked is revoked, with that secret", func(t *testing.T) {
		got, state := secretsOf([]q.IdentityTechnicalTokenSecretsRow{row("b", false, true), row("a", false, false)})
		if state != StateRevoked || len(got) != 1 || got[0].ID != "b" || got[0].RevokedAt == nil {
			t.Fatalf("%s %+v", state, got)
		}
	})
	t.Run("a token whose last secret expired is expired", func(t *testing.T) {
		got, state := secretsOf([]q.IdentityTechnicalTokenSecretsRow{row("b", false, false)})
		if state != StateExpired || len(got) != 1 {
			t.Fatalf("%s %+v", state, got)
		}
	})
	t.Run("a token without a secret row is expired with none", func(t *testing.T) {
		got, state := secretsOf(nil)
		if state != StateExpired || got == nil || len(got) != 0 {
			t.Fatalf("%s %+v", state, got)
		}
	})
}

// The server stores no phrase it makes up (R-UI-214, Q-UI-1653): the token of an upload from the screen and the tokens of the npm web
// login and of the device login are created without a description, every other token still needs one of 1 to 1024 characters.
func TestR_UI_214_TheTokenOfAnUploadFromTheScreenHasNoDescription(t *testing.T) {
	long := string(make([]rune, 1025))
	for _, c := range []struct {
		r  CreateRequest
		ok bool
	}{
		{CreateRequest{Type: TypePAT, Via: ViaUpload}, true},
		{CreateRequest{Type: TypePAT, Via: ViaNpmWebLogin}, true},
		{CreateRequest{Type: TypePAT, Via: ViaDeviceLogin}, true},
		{CreateRequest{Type: TypePAT, Via: ViaDeviceLogin, Description: long}, false},
		{CreateRequest{Type: TypePAT, Via: "ui", Description: "laptop"}, true},
		{CreateRequest{Type: TypePAT, Via: "ui"}, false},
		{CreateRequest{Type: TypePAT, Via: "api"}, false},
		{CreateRequest{Type: TypePAT, Via: ViaUpload, Description: long}, false},
	} {
		if err := checkDescription(c.r); (err == nil) != c.ok {
			t.Errorf("via %q, %d characters: %v", c.r.Via, len([]rune(c.r.Description)), err)
		}
	}
}
