//go:build integration

package proxy

import (
	"context"
	"net/http"
	"sync"
	"testing"

	"github.com/guided-traffic/quader/internal/items"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/proxy"
	"github.com/guided-traffic/quader/internal/proxy/proxytest"
)

// contentHooks reads the content of every commit, as the signatures' detector does (signatures.md R-SIG-004), and keeps what it read.
type contentHooks struct {
	mu   sync.Mutex
	read map[string]string
	errs map[string]error
}

func (h *contentHooks) OnCommit(_ context.Context, _ meta.Tx, e items.CommitEvent) error {
	b, err := e.Content.Bytes(1 << 20)
	h.mu.Lock()
	defer h.mu.Unlock()
	h.read[e.Item.Path], h.errs[e.Item.Path] = string(b), err
	return nil
}

func (*contentHooks) OnItemsRemoved(context.Context, meta.Tx, items.RemovedEvent) error { return nil }

// TestR_SIG_004_TheHookOfAnAliasReadsTheDocumentItsFillStored: a metadata document fetched for the first time is stored and entered
// under its alias (an OCI manifest by its tag and by its digest, R-OCI-022) in one transaction; the storage hook of the alias reads the
// document's bytes. Before, the alias's hook read the blob through the object store on another connection, where the copy written in
// that transaction is not yet visible ("no connection of this replica holds a present copy"), so the signatures' detector never saw an
// image's digest path that a fill by tag had stored: an index's attestation entries and a referrer's subject were lost. One document of
// a few bytes, about 0.2 s.
func TestR_SIG_004_TheHookOfAnAliasReadsTheDocumentItsFillStored(t *testing.T) {
	e := newEnv(t)
	up := e.upstream()
	doc := []byte("alias pkg/by-digest\n{\"schemaVersion\":2,\"mediaType\":\"application/vnd.oci.image.index.v1+json\"}")
	up.Set("/pkg/by-tag", proxytest.Route{Body: doc, Type: "application/vnd.oci.image.index.v1+json"})
	rem := e.remote("alias-hook", up)
	hooks := &contentHooks{read: map[string]string{}, errs: map[string]error{}}
	r := e.newReplica("a", replicaOpts{hooks: hooks})
	if _, err := r.engine.Metadata(context.Background(), r.request(rem, "pkg/by-tag", proxy.Metadata)); err != nil {
		t.Fatal(err)
	}
	hooks.mu.Lock()
	defer hooks.mu.Unlock()
	if err := hooks.errs["pkg/by-digest"]; err != nil || hooks.read["pkg/by-digest"] != string(doc) {
		t.Fatalf("the alias's hook read %q, %v; want the document", hooks.read["pkg/by-digest"], err)
	}
}

// TestFillAliasesAndMediaType: a metadata document is stored with the media type the upstream sent, and a path the format names as
// an alias (Prepared.Aliases) is entered in the fill transaction as an item for the same blob and an artifact entry, so a second
// path serves the document with no request to the upstream (ADR-161, Q-OCI-25).
func TestFillAliasesAndMediaType(t *testing.T) {
	e := newEnv(t)
	up := e.upstream()
	doc := []byte("alias pkg/by-digest\n{\"schemaVersion\":2}")
	const mt = "application/vnd.docker.distribution.manifest.v2+json"
	up.Set("/pkg/by-tag", proxytest.Route{Body: doc, Type: mt + "; charset=utf-8"})
	rem := e.remote("alias", up)
	r := e.newReplica("a", replicaOpts{})

	d, err := r.engine.Metadata(context.Background(), r.request(rem, "pkg/by-tag", proxy.Metadata))
	if err != nil || d.MediaType != mt {
		t.Fatalf("the fetched document: media type %q, %v", d.MediaType, err)
	}
	if got := e.text("SELECT media_type FROM remote_cache_entries WHERE path = 'pkg/by-tag'"); got != mt {
		t.Errorf("the cache entry keeps the media type %q", got)
	}
	// A hit reads the stored type back.
	if d, err = r.engine.Metadata(context.Background(), r.request(rem, "pkg/by-tag", proxy.Metadata)); err != nil || d.MediaType != mt || !d.Status.Hit {
		t.Fatalf("the stored document: %q, hit %v, %v", d.MediaType, d.Status.Hit, err)
	}
	if n := e.number("SELECT count(*) FROM remote_cache_entries a JOIN remote_cache_entries m ON m.blob_sha256 = a.blob_sha256 " +
		"WHERE a.path = 'pkg/by-digest' AND a.kind = 'artifact' AND a.item_id IS NOT NULL AND m.path = 'pkg/by-tag' AND m.kind = 'metadata'"); n != 1 {
		t.Fatalf("the alias entry shares the blob of the document: %d", n)
	}
	if n := e.number("SELECT count(*) FROM items WHERE path = 'pkg/by-digest' AND blob_sha256 = $1", proxytest.SHA256(doc)); n != 1 {
		t.Fatalf("the alias item: %d", n)
	}
	before := up.Total()
	s := r.artifact(rem, "pkg/by-digest")
	if s.status != http.StatusOK || string(s.body) != string(doc) {
		t.Fatalf("the alias: %d %q", s.status, s.body)
	}
	if up.Total() != before {
		t.Errorf("the alias cost %d upstream requests", up.Total()-before)
	}
}
