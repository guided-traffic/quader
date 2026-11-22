package downloadlinks

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/blob/blobtest"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/items"
)

// TestR_STOR_072_TheLinksHost: a link's host may be no host of Quader and lie below none, nor match or lie below a wildcard host;
// with virtual-hosted addressing the bucket's name is part of the host that is compared (Q-STOR-288).
func TestR_STOR_072_TheLinksHost(t *testing.T) {
	var s config.Server
	s.BaseURL = "https://quader.example.com"
	s.Endpoints = map[string]config.Endpoint{"default": {WildcardHost: "*.packages.example.com"}}
	rule := NewHostRule(s)
	for _, tc := range []struct {
		name, address, bucket string
		pathStyle             bool
		refused               bool
	}{
		{"R-STOR-072 objects.example.com beside quader.example.com is accepted", "https://objects.example.com", "quader", true, false},
		{"R-STOR-072 quader.example.com is refused", "https://quader.example.com", "quader", true, true},
		{"R-STOR-072 s3.quader.example.com below it is refused", "https://s3.quader.example.com:9000", "quader", true, true},
		{"R-STOR-072 blobs.packages.example.com below a wildcard host is refused", "https://blobs.packages.example.com", "quader", true, true},
		{"R-STOR-072 the bucket host quader.quader.example.com of virtual-hosted addressing is refused", "https://quader.example.com", "quader", false, true},
		{"R-STOR-072 the bucket host of virtual-hosted addressing below another domain is accepted", "https://objects.example.com", "quader", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			host := LinkHost(tc.address, tc.bucket, tc.pathStyle)
			if _, refused := rule.Refuses(host); refused != tc.refused {
				t.Fatalf("%s: refused %v", host, refused)
			}
		})
	}
	t.Run("R-STOR-074 a public address is an https origin and its host no host of Quader", func(t *testing.T) {
		for addr, ok := range map[string]bool{
			"https://objects.example.com": true, "https://objects.example.com:8443": true, "https://objects.example.com/": true,
			"http://objects.example.com": false, "https://objects.example.com/bucket": false, "https://u:p@objects.example.com": false,
			"https://objects.example.com?x=1": false, "objects.example.com": false, "https://s3.quader.example.com": false,
		} {
			if err := CheckAddress(addr, rule); (err == nil) != ok {
				t.Errorf("%s: %v", addr, err)
			}
		}
	})
}

// TestR_STOR_014_TheSettings: a new installation is off with 1 MiB and 60 s; a write is refused outside the ranges, for an address
// that is not an https origin or lies below a host of Quader, and for a connection the installation does not have; a merge patch's
// null returns a connection to its endpoint.
func TestR_STOR_014_TheSettings(t *testing.T) {
	d := Defaults()
	if d.Enabled || d.MinSize != 1048576 || d.LinkLifeSeconds != 60 || len(d.PublicAddresses) != 0 {
		t.Fatalf("defaults %+v", d)
	}
	rule := HostRuleOf([]string{"quader.example.com"})
	addr := "https://objects.example.com"
	on, size, life := true, int64(0), 3601
	next := d.Apply(Patch{Enabled: &on, MinSize: &size, LinkLifeSeconds: &life, PublicAddresses: map[string]*string{"default": &addr, "other": &addr}})
	bad := "https://s3.quader.example.com"
	next.PublicAddresses["secondary"] = bad
	got := map[string]string{}
	for _, v := range Validate(next, []string{"default", "secondary"}, rule) {
		got[v.Pointer] = v.Code
	}
	want := map[string]string{"/linkLifeSeconds": CodeInvalidValue, "/publicAddresses/other": CodeInvalidValue, "/publicAddresses/secondary": CodeInvalidValue}
	if len(got) != len(want) {
		t.Fatalf("violations %v", got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: %q", k, got[k])
		}
	}
	back := next.Apply(Patch{PublicAddresses: map[string]*string{"default": nil}})
	if _, ok := back.PublicAddresses["default"]; ok || next.PublicAddresses["default"] != addr {
		t.Fatalf("null did not return the connection to its endpoint: %v (the source %v)", back.PublicAddresses, next.PublicAddresses)
	}
}

// rig is a replica's side over in-memory connections, each with the endpoint https://mem.invalid of blobtest's links.
type rig struct {
	links  *Links
	stores map[string]*blobtest.Store
	conns  *blob.Connections
}

func newRig(t *testing.T, mutate func(id string, c *config.StorageConnection)) *rig {
	t.Helper()
	r := &rig{stores: map[string]*blobtest.Store{}}
	var list []*blob.Connection
	for _, id := range []string{"plain", "second"} {
		m := blobtest.New()
		r.stores[id] = m
		p := "quader"
		el := config.StorageConnection{ID: id, Bucket: "blobs", Prefix: &p, Endpoint: "https://" + blobtest.PresignHost, ForcePathStyle: true}
		if mutate != nil {
			mutate(id, &el)
		}
		list = append(list, blob.NewConnection(el, blob.NewGuard(m, config.StorageS3Breaker{FailureThreshold: 1, OpenDuration: time.Minute})))
	}
	conns, err := blob.NewConnections(list...)
	if err != nil {
		t.Fatal(err)
	}
	r.conns = conns
	r.links = New(Options{Connections: conns, Rule: HostRuleOf([]string{"quader.example.com"}), Replica: "r1"})
	s := Defaults()
	s.Enabled = true
	r.links.SetSettings(s)
	r.links.RunLinkChecks(context.Background())
	return r
}

func reasons(rs []Reason) string {
	var out []string
	for _, r := range rs {
		out = append(out, r.Code)
	}
	return strings.Join(out, ",")
}

// TestR_STOR_074_WhichConnectionsCanServeLinks: a connection serves links when it is not encrypted, its address is https and no host
// of Quader, and its link check passed on the replica; the reasons name what is missing; a store that ignores an override can serve
// a handover and no link, and an encrypted connection a handover too (R-STOR-096, Q-STOR-346).
func TestR_STOR_074_WhichConnectionsCanServeLinks(t *testing.T) {
	t.Run("R-STOR-074 two good connections serve links and handovers", func(t *testing.T) {
		r := newRig(t, nil)
		for _, st := range r.links.States() {
			if !st.Usable || !st.HandoverUsable || st.PublicAddress != "https://mem.invalid" {
				t.Errorf("%+v", st)
			}
		}
	})
	r := newRig(t, func(id string, c *config.StorageConnection) {
		if id == "second" {
			c.Encrypted = true
		}
	})
	r.stores["plain"].IgnoreOverride("response-content-disposition")
	r.links.RunLinkChecks(context.Background())
	plain, _ := r.links.State("plain")
	second, _ := r.links.State("second")
	t.Run("R-STOR-074 R-STOR-096 a store that ignores response-content-disposition serves a handover and no link", func(t *testing.T) {
		if plain.Usable || reasons(plain.Reasons) != ReasonLinkCheckFailed || !strings.Contains(plain.Reasons[0].Detail, "Content-Disposition") || !plain.HandoverUsable {
			t.Fatalf("%+v", plain)
		}
	})
	t.Run("R-STOR-074 an encrypted connection serves no link and can serve a handover", func(t *testing.T) {
		if second.Usable || reasons(second.Reasons) != ReasonEncrypted || !second.HandoverUsable {
			t.Fatalf("%+v", second)
		}
	})
	t.Run("R-STOR-074 an address below a host of Quader, and an endpoint over http without an address", func(t *testing.T) {
		r := newRig(t, func(id string, c *config.StorageConnection) {
			if id == "second" {
				c.Endpoint, c.AllowInsecureEndpoint = "http://store.internal:9000", true
			}
		})
		s := r.links.Settings()
		s.PublicAddresses = map[string]string{"plain": "https://s3.quader.example.com"}
		r.links.SetSettings(s)
		p, _ := r.links.State("plain")
		sec, _ := r.links.State("second")
		if reasons(p.Reasons) != ReasonQuaderHost || p.Reasons[0].Detail != "quader.example.com" || p.HandoverUsable {
			t.Errorf("plain %+v", p)
		}
		if reasons(sec.Reasons) != ReasonNotHTTPS+","+ReasonLinkCheckFailed && reasons(sec.Reasons) != ReasonNotHTTPS {
			t.Errorf("second %+v", sec)
		}
	})
}

// item is a stored file of the rig: its object on every connection.
func (r *rig) item(t *testing.T, size int) items.Item {
	t.Helper()
	var d blob.Digest
	d[0], d[31] = 0x0a, 0x0b
	key := blob.Key("quader", d)
	data := make([]byte, size)
	var copies []blob.Copy
	for _, id := range []string{"plain", "second"} {
		r.stores[id].Set(key, data, time.Now())
		copies = append(copies, blob.Copy{Connection: id, ObjectKey: key, StoredSize: int64(size), State: blob.CopyPresent})
	}
	return items.Item{Path: "a/-/a 1.0.0.tgz", Kind: items.KindFile, Size: int64(size), MediaType: "application/octet-stream", Digest: d, Copies: copies, CopiesRead: true}
}

// linkDisposition is the disposition the serve point hands to Sign for the rig's item (its encoding is the edge layer's).
const linkDisposition = "attachment; filename*=UTF-8''a%201.0.0.tgz"

// TestR_STOR_074_TheConnectionOfALink: the link comes from the first copy in the read order on a connection that can serve links,
// not from an encrypted one, not from a copy marked damaged, not from a connection whose circuit is open; with none the download
// streams; the link names the public address, the item's type, its file name and no-store, and a credential that ends within 5 s,
// or a signing error, streams (Q-STOR-291).
func TestR_STOR_074_TheConnectionOfALink(t *testing.T) {
	ctx := context.Background()
	t.Run("R-STOR-074 the first good connection signs, against its public address, with the three overrides", func(t *testing.T) {
		r := newRig(t, nil)
		s := r.links.Settings()
		s.PublicAddresses = map[string]string{"plain": "https://objects.example.com"}
		r.links.SetSettings(s)
		link, declined := r.links.Sign(ctx, r.item(t, 64), linkDisposition)
		if declined != "" {
			t.Fatal(declined)
		}
		u, _ := url.Parse(link.URL)
		q := u.Query()
		if u.Host != "objects.example.com" || q.Get("response-content-type") != "application/octet-stream" ||
			q.Get("response-content-disposition") != "attachment; filename*=UTF-8''a%201.0.0.tgz" || q.Get("response-cache-control") != CacheControl ||
			q.Get("X-Amz-Expires") != "60" {
			t.Fatalf("%s", link.URL)
		}
	})
	t.Run("R-STOR-074 an encrypted first connection, a damaged copy and an open circuit hand the link to the next", func(t *testing.T) {
		r := newRig(t, func(id string, c *config.StorageConnection) {
			if id == "plain" {
				c.Encrypted = true
			}
		})
		it := r.item(t, 64)
		link, declined := r.links.Sign(ctx, it, linkDisposition)
		if declined != "" || !strings.Contains(link.URL, "mem.invalid") {
			t.Fatalf("%q %s", declined, link.URL)
		}
		it.Copies[1].State = blob.CopyCorrupt
		if _, declined := r.links.Sign(ctx, it, linkDisposition); declined != DeclinedConnection {
			t.Fatalf("a damaged copy on the only plain connection: %q", declined)
		}
	})
	t.Run("R-STOR-074 a connection whose circuit is open signs nothing, and the download streams", func(t *testing.T) {
		r := newRig(t, nil)
		for _, m := range r.stores {
			m.SetFault(func(string, string) error {
				return &blob.StoreError{Op: "HeadObject", Kind: blob.KindTransient, Status: 503}
			})
		}
		for _, c := range r.conns.List() {
			_, _ = c.Objects().Head(ctx, "k")
		}
		if _, declined := r.links.Sign(ctx, r.item(t, 64), linkDisposition); declined != DeclinedConnection {
			t.Fatalf("%q", declined)
		}
	})
	t.Run("R-STOR-072 a credential that ends in 3 s and a signing error stream", func(t *testing.T) {
		r := newRig(t, nil)
		it := r.item(t, 64)
		r.stores["plain"].SetCredentialExpiry(time.Now().Add(3 * time.Second))
		if _, declined := r.links.Sign(ctx, it, linkDisposition); declined != DeclinedCredential {
			t.Fatalf("%q", declined)
		}
		r.stores["plain"].SetCredentialExpiry(time.Time{})
		r.stores["plain"].SetFault(func(op, _ string) error {
			if op == "presign" {
				return errors.New("no signer")
			}
			return nil
		})
		if _, declined := r.links.Sign(ctx, it, linkDisposition); declined != DeclinedSigning {
			t.Fatalf("%q", declined)
		}
	})
}
