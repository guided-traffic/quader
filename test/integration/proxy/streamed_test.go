//go:build integration

package proxy

import (
	"context"
	"net/http"
	"testing"

	"github.com/guided-traffic/quader/internal/items"
	"github.com/guided-traffic/quader/internal/proxy"
	"github.com/guided-traffic/quader/internal/proxy/proxytest"
)

// TestR_STOR_015_TheStreamedFirstAnswerOfAMissIsADownload: the client that leads the fetch of an artifact gets the body while it
// arrives, not through Serve, which counts the stored answers (R-STOR-015). That answer is a download as much as the later ones: the
// engine hands the stored item to Request.Streamed once the client has the whole body and the item is stored. A hit and a ranged
// miss are answered through Serve and do not call it; a HEAD of a miss stores nothing and calls nothing.
func TestR_STOR_015_TheStreamedFirstAnswerOfAMissIsADownload(t *testing.T) {
	e := newEnv(t)
	up := e.upstream()
	big := proxytest.Body("big", 256<<10) // beyond the withheld tail: streamed while it is fetched
	up.Set("/pkg/big/-/big-1.tgz", proxytest.Route{Body: big, Type: "application/octet-stream"})
	up.Set("/pkg/ranged/-/ranged-1.tgz", proxytest.Route{Body: proxytest.Body("ranged", 256<<10), Type: "application/octet-stream"})
	rem := e.remote("streamed", up)
	r := e.newReplica("a", replicaOpts{})

	var streamed []items.Item
	var serves int
	count := func(rq *proxy.Request) {
		rq.Streamed = func(_ context.Context, it items.Item) { streamed = append(streamed, it) }
		inner := r.serveFunc()
		rq.Serve = func(ctx context.Context, w http.ResponseWriter, it items.Item) error {
			serves++
			return inner(ctx, w, it)
		}
	}

	if s := r.artifactWith(rem, http.MethodHead, "pkg/big/-/big-1.tgz", nil, count); s.status != http.StatusOK || len(streamed) != 0 {
		t.Fatalf("HEAD of a miss: %d, streamed %d", s.status, len(streamed))
	}
	if s := r.artifact(rem, "pkg/big/-/big-1.tgz", count); s.status != http.StatusOK || len(s.body) != len(big) {
		t.Fatalf("the miss: %d, %d bytes", s.status, len(s.body))
	}
	if len(streamed) != 1 || serves != 0 || streamed[0].Path != "pkg/big/-/big-1.tgz" || streamed[0].ID == 0 || streamed[0].Size != int64(len(big)) {
		t.Fatalf("the streamed miss called Streamed %d times with %+v and Serve %d times, want once with the stored item", len(streamed), streamed, serves)
	}
	if s := r.artifact(rem, "pkg/big/-/big-1.tgz", count); s.status != http.StatusOK || len(streamed) != 1 || serves != 1 {
		t.Fatalf("the hit: %d, streamed %d, served %d: a stored answer counts through Serve alone", s.status, len(streamed), serves)
	}
	hdr := http.Header{"Range": {"bytes=0-99"}}
	if s := r.artifactWith(rem, http.MethodGet, "pkg/ranged/-/ranged-1.tgz", hdr, count); s.err != nil || len(streamed) != 1 || serves != 2 {
		t.Fatalf("a ranged miss: %v, streamed %d, served %d: it is answered from the stored item", s.err, len(streamed), serves)
	}
}
