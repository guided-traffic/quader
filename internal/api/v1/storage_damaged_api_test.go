package v1

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/api/v1/gen"
	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/blob/scrub"
)

// Tests of the list of damaged blobs and the recheck of one blob (storage.md R-STOR-027, Q-API-386) over in-memory services;
// the statements behind scrub.ListDamaged are the integration test's (test/integration/blob/scrub_test.go).

type damagedFake struct {
	rows    []scrub.DamagedBlob
	queries []scrub.DamagedQuery
	copies  []scrub.CopyRow
}

func (f *damagedFake) services() StorageServices {
	return StorageServices{
		Damaged: func(_ context.Context, q scrub.DamagedQuery) ([]scrub.DamagedBlob, error) {
			f.queries = append(f.queries, q)
			var out []scrub.DamagedBlob
			for _, r := range f.rows {
				if q.After != nil && r.Digest.Hex() <= blob.Digest(q.After).Hex() {
					continue
				}
				out = append(out, r)
				if len(out) == q.Limit {
					break
				}
			}
			return out, nil
		},
		Recheck: func(_ context.Context, d blob.Digest) ([]scrub.CopyRow, error) {
			if d != f.rows[0].Digest {
				return nil, scrub.ErrNoBlob
			}
			return f.copies, nil
		},
	}
}

func damagedDigest(b byte) blob.Digest {
	var d blob.Digest
	for i := range d {
		d[i] = b
	}
	return d
}

func TestR_STOR_027_TheDamagedBlobsList(t *testing.T) {
	val := newValidator(t)
	at := time.Date(2026, 11, 22, 10, 7, 12, 0, time.UTC)
	f := &damagedFake{
		rows: []scrub.DamagedBlob{
			{
				Digest: damagedDigest(0x1a), Size: 18, ContentStatus: "missing", ItemCount: 21,
				Copies: []scrub.DamagedCopy{{Connection: "default", State: "missing", StateChangedAt: at, CheckedAt: &at}},
				Items:  []scrub.DamagedItem{{Project: "platform", Repository: "npm-local", Path: "a/-/a-1.0.0.tgz", Trashed: true}},
			},
			{Digest: damagedDigest(0x2b), Size: 3, ContentStatus: "ok", Copies: []scrub.DamagedCopy{}, Items: []scrub.DamagedItem{}},
		},
		copies: []scrub.CopyRow{
			{Copy: blob.Copy{Connection: "default", State: blob.CopyPresent, VerifiedAt: at, CheckedAt: at}, StateChangedAt: at},
			{Copy: blob.Copy{Connection: "secondary", State: blob.CopyCorrupt}, StateChangedAt: at},
		},
	}
	s := newSetup(t, func(o *Options) {
		o.API.Config = apiConfigForTest()
		o.Storage = f.services()
	}, authed)
	h := val.Handler(t, s.handler)
	get := func(path string) (int, string) {
		w := httpRecorder(h, http.MethodGet, path)
		return w.Code, w.Body.String()
	}

	t.Run("R-STOR-027 a page lists each blob with its copies, its first items and their number, and the cursor leads on", func(t *testing.T) {
		code, body := get("/api/v1/storage/damaged-blobs?limit=1&scope=copies&connection=default&status=missing")
		if code != http.StatusOK {
			t.Fatalf("%d %s", code, body)
		}
		var page gen.DamagedBlobList
		if err := json.Unmarshal([]byte(body), &page); err != nil {
			t.Fatal(err)
		}
		if len(page.Items) != 1 || page.NextCursor == nil {
			t.Fatalf("page = %s", body)
		}
		e := page.Items[0]
		if e.Sha256 != damagedDigest(0x1a).Hex() || e.ItemCount != 21 || len(e.Items) != 1 || !e.Items[0].Trashed || e.Copies[0].VerifiedAt != nil ||
			e.Copies[0].CheckedAt == nil {
			t.Errorf("entry = %+v", e)
		}
		q := f.queries[len(f.queries)-1]
		if q.Scope != scrub.ScopeCopies || q.Connection != "default" || q.Status != "missing" || q.Limit != 2 {
			t.Errorf("query = %+v", q)
		}
		code, body = get("/api/v1/storage/damaged-blobs?limit=1&scope=copies&connection=default&status=missing&cursor=" + *page.NextCursor)
		if code != http.StatusOK || !strings.Contains(body, damagedDigest(0x2b).Hex()) || !strings.Contains(body, `"nextCursor":null`) {
			t.Errorf("second page: %d %s", code, body)
		}
	})

	t.Run("R-STOR-027 the default scope is blobs, a malformed cursor or scope is 400, and a caller without settings:read gets 403", func(t *testing.T) {
		if code, _ := get("/api/v1/storage/damaged-blobs"); code != http.StatusOK || f.queries[len(f.queries)-1].Scope != scrub.ScopeBlobs {
			t.Errorf("default scope: %d %+v", code, f.queries[len(f.queries)-1])
		}
		for _, p := range []string{"/api/v1/storage/damaged-blobs?cursor=bm9wZQ", "/api/v1/storage/damaged-blobs?scope=all"} {
			if code, body := get(p); code != http.StatusBadRequest {
				t.Errorf("%s: %d %s", p, code, body)
			}
		}
		denied := newSetup(t, func(o *Options) { o.API.Config = apiConfigForTest(); o.Access = denyAccess{}; o.Storage = f.services() }, authed)
		before := len(f.queries)
		if w := denied.get("/api/v1/storage/damaged-blobs"); w.Code != http.StatusForbidden || len(f.queries) != before {
			t.Errorf("%d %s", w.Code, w.Body.String())
		}
	})

	t.Run("R-STOR-027 the recheck answers the copies after the reads with the content state they give; an unknown blob is 404", func(t *testing.T) {
		w := httpRecorder(h, http.MethodPost, "/api/v1/storage/damaged-blobs/"+damagedDigest(0x1a).Hex()+"/recheck")
		if w.Code != http.StatusOK {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
		var out gen.DamagedBlobRecheck
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		if out.ContentStatus != "ok" || len(out.Copies) != 2 || out.Copies[1].State != "corrupt" || out.Copies[0].VerifiedAt == nil {
			t.Errorf("recheck = %s", w.Body.String())
		}
		if w := httpRecorder(h, http.MethodPost, "/api/v1/storage/damaged-blobs/"+damagedDigest(0x3c).Hex()+"/recheck"); w.Code != http.StatusNotFound {
			t.Errorf("unknown blob: %d %s", w.Code, w.Body.String())
		}
		if w := httpRecorder(h, http.MethodPost, "/api/v1/storage/damaged-blobs/ABC/recheck"); w.Code != http.StatusBadRequest {
			t.Errorf("malformed digest: %d %s", w.Code, w.Body.String())
		}
	})

	t.Run("R-STOR-027 without the services both operations answer 503", func(t *testing.T) {
		bare := newSetup(t, func(o *Options) { o.API.Config = apiConfigForTest() }, authed)
		if w := bare.get("/api/v1/storage/damaged-blobs"); w.Code != http.StatusServiceUnavailable {
			t.Errorf("list: %d", w.Code)
		}
		if w := httpRecorder(bare.handler, http.MethodPost, "/api/v1/storage/damaged-blobs/"+damagedDigest(1).Hex()+"/recheck"); w.Code != http.StatusServiceUnavailable {
			t.Errorf("recheck: %d", w.Code)
		}
	})
}

// httpRecorder sends one request without a body to h.
func httpRecorder(h http.Handler, method, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(method, path, nil))
	return w
}
