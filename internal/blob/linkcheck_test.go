package blob_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/blob/blobtest"
)

// TestR_STOR_074_TheLinkCheck: the link check passes against a store that honours the three overrides, fails against one that
// ignores response-content-disposition, leaves no probe behind, and a failed check stops nothing else: the store's objects are
// read as before (Q-STOR-294).
func TestR_STOR_074_TheLinkCheck(t *testing.T) {
	ctx := context.Background()
	t.Run("R-STOR-074 a store that honours the three overrides passes both parts and keeps no probe", func(t *testing.T) {
		m := blobtest.New()
		r := blob.CheckLink(ctx, m, m, "quader", "replica-1")
		if !r.Passed() || !r.Works || !r.Overrides || r.Detail != "" {
			t.Fatalf("%+v", r)
		}
		if keys := m.Keys(""); len(keys) != 0 {
			t.Fatalf("the probe is left: %v", keys)
		}
	})
	t.Run("R-STOR-074 a store that ignores response-content-disposition fails the check, and its objects are read as before", func(t *testing.T) {
		m := blobtest.New()
		m.IgnoreOverride("response-content-disposition")
		m.Set("quader/sha256/aa/bb/x", []byte("payload"), time.Now())
		r := blob.CheckLink(ctx, m, m, "quader", "replica-1")
		if r.Passed() || !r.Ran || !strings.Contains(r.Detail, "response-content-disposition") {
			t.Fatalf("%+v", r)
		}
		body, _, err := m.Get(ctx, "quader/sha256/aa/bb/x", nil)
		if err != nil {
			t.Fatal(err)
		}
		got, _ := io.ReadAll(body)
		_ = body.Close()
		if !bytes.Equal(got, []byte("payload")) {
			t.Fatalf("read %q", got)
		}
	})
	t.Run("R-STOR-074 a store whose probe cannot be written has not answered the check", func(t *testing.T) {
		m := blobtest.New()
		m.SetFault(func(op, _ string) error {
			if op == "put" {
				return &blob.StoreError{Op: "PutObject", Kind: blob.KindTransient, Status: 503}
			}
			return nil
		})
		if r := blob.CheckLink(ctx, m, m, "quader", "replica-1"); r.Ran || r.Passed() {
			t.Fatalf("%+v", r)
		}
	})
}

// TestR_STOR_096_TheLinkCheckHasTwoParts: a store that ignores response-content-disposition passes the first part (the link
// works) and fails the second (the overrides), so its connection can serve a handover to a Bastion and no client's link
// (Q-STOR-346).
func TestR_STOR_096_TheLinkCheckHasTwoParts(t *testing.T) {
	m := blobtest.New()
	m.IgnoreOverride("response-content-disposition")
	r := blob.CheckLink(context.Background(), m, m, "", "replica-1")
	if !r.Ran || !r.Works || r.Overrides || r.Passed() {
		t.Fatalf("%+v", r)
	}
}

// TestR_STOR_072_TheLinksLife: a link lives its life in whole seconds, the earlier of it and the signing credential's expiry, and
// a credential that ends within 5 s signs none (Q-STOR-290).
func TestR_STOR_072_TheLinksLife(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	for _, tc := range []struct {
		name    string
		life    time.Duration
		expires time.Time
		want    time.Duration
		err     error
	}{
		{"R-STOR-072 a static key: the life", 60 * time.Second, time.Time{}, 60 * time.Second, nil},
		{"R-STOR-072 a credential of 30 s bounds a life of 60 s", 60 * time.Second, now.Add(30500 * time.Millisecond), 30 * time.Second, nil},
		{"R-STOR-072 a credential of 3 s signs no link", 60 * time.Second, now.Add(3 * time.Second), 0, blob.ErrCredentialExpiring},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := blob.LinkLife(now, tc.life, tc.expires)
			if got != tc.want || !errors.Is(err, tc.err) {
				t.Fatalf("%s, %v", got, err)
			}
		})
	}
	m := blobtest.New()
	m.Set("k", []byte("x"), now)
	m.SetCredentialExpiry(time.Now().Add(3 * time.Second))
	if _, err := m.Presign(context.Background(), "k", blob.PresignOptions{Life: time.Minute}); !errors.Is(err, blob.ErrCredentialExpiring) {
		t.Fatalf("the in-memory store signed with a credential of 3 s: %v", err)
	}
}
