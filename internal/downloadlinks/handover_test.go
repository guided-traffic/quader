package downloadlinks

import (
	"context"
	"net/url"
	"testing"

	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/config"
)

// TestR_STOR_096_TheCopyOfAHandover: of the copies a read would try now, the handover names the first on a connection that can
// serve a handover and is not encrypted (Q-STOR-341): an encrypted connection, a failed link check, a copy on a connection the
// replica does not configure and a plain copy of another length hand it to the next; a blob whose only servable copy is
// encrypted is streamed, since its data key is not built (R-STOR-095 steps 7 and 8); the link is signed against the public address
// for GET with no override, and the size must lie within minSize and the offer.
func TestR_STOR_096_TheCopyOfAHandover(t *testing.T) {
	ctx := context.Background()
	const offer = 1 << 20
	t.Run("R-STOR-095 the first good copy, against its public address, without overrides", func(t *testing.T) {
		r := newRig(t, nil)
		s := r.links.Settings()
		s.MinSize, s.PublicAddresses = 0, map[string]string{"plain": "https://objects.example.com"}
		r.links.SetSettings(s)
		h, declined := r.links.handover(ctx, r.item(t, 64), offer)
		if declined != "" || h.Connection != "plain" || h.Encrypted || h.Size != 64 || h.StoredSize != 64 {
			t.Fatalf("%q %v", declined, h)
		}
		u, _ := url.Parse(h.Link)
		q := u.Query()
		if u.Host != "objects.example.com" || q.Has("response-content-type") || q.Has("response-content-disposition") || q.Has("response-cache-control") ||
			q.Get("X-Amz-Expires") != "60" {
			t.Fatalf("%s", h.Link)
		}
	})
	t.Run("R-STOR-096 Q-STOR-341 an encrypted first connection hands the plain copy of the second", func(t *testing.T) {
		r := handoverRig(t, func(id string, c *config.StorageConnection) { c.Encrypted = id == "plain" })
		if h, declined := r.links.handover(ctx, r.item(t, 64), offer); declined != "" || h.Connection != "second" || h.Encrypted {
			t.Fatalf("%q %v", declined, h)
		}
	})
	t.Run("R-STOR-095 a blob whose only servable copy is encrypted is streamed (no data key in this stage)", func(t *testing.T) {
		r := handoverRig(t, func(_ string, c *config.StorageConnection) { c.Encrypted = true })
		if _, declined := r.links.handover(ctx, r.item(t, 64), offer); declined != HandoverDeclinedConnection {
			t.Fatalf("%q", declined)
		}
	})
	t.Run("R-STOR-096 a failed link check, a detached connection and a copy of another length hand it to the next", func(t *testing.T) {
		r := handoverRig(t, nil)
		r.conns.List()[0].SetLinkCheck(blob.LinkCheck{Ran: true, Detail: "the link answered 403"})
		r.links.SetSettings(r.links.Settings())
		if h, _ := r.links.handover(ctx, r.item(t, 64), offer); h.Connection != "second" {
			t.Fatalf("a failed link check: %v", h)
		}
		r = handoverRig(t, nil)
		it := r.item(t, 64)
		it.Copies[0].Connection = "detached"
		if h, _ := r.links.handover(ctx, it, offer); h.Connection != "second" {
			t.Fatalf("a detached connection: %v", h)
		}
		it = r.item(t, 64)
		it.Copies[0].StoredSize = 65
		if h, _ := r.links.handover(ctx, it, offer); h.Connection != "second" {
			t.Fatalf("a copy of another length: %v", h)
		}
		it.Copies[1].State = blob.CopyCorrupt
		if _, declined := r.links.handover(ctx, it, offer); declined != HandoverDeclinedConnection {
			t.Fatalf("no copy left: %q", declined)
		}
	})
	t.Run("R-STOR-095 the size lies within minSize and the offer's max-size", func(t *testing.T) {
		r := newRig(t, nil) // minSize is the default 1 MiB
		if _, declined := r.links.handover(ctx, r.item(t, 64), offer); declined != HandoverDeclinedSize {
			t.Fatalf("below minSize: %q", declined)
		}
		s := r.links.Settings()
		s.MinSize = 0
		r.links.SetSettings(s)
		if _, declined := r.links.handover(ctx, r.item(t, 64), 63); declined != HandoverDeclinedSize {
			t.Fatalf("above the offer: %q", declined)
		}
		if _, declined := r.links.handover(ctx, r.item(t, 64), 64); declined != "" {
			t.Fatalf("at the offer: %q", declined)
		}
	})
}

// handoverRig is the links' rig with minSize 0, so that the rig's small items may be handed over.
func handoverRig(t *testing.T, mutate func(id string, c *config.StorageConnection)) *rig {
	t.Helper()
	r := newRig(t, mutate)
	s := r.links.Settings()
	s.MinSize = 0
	r.links.SetSettings(s)
	return r
}
