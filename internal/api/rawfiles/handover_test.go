package rawfiles

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastionmain"
	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/blob/blobtest"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/downloadlinks"
	"github.com/guided-traffic/quader/internal/files"
	"github.com/guided-traffic/quader/internal/items"
	"github.com/guided-traffic/quader/internal/malware"
	"github.com/guided-traffic/quader/internal/malware/gate"
)

// servedStore is the store of access_test.go that records what was served and opened.
type servedStore struct {
	store
	served []items.ServedEvent
	opened int
}

func (s *servedStore) Served(_ context.Context, e items.ServedEvent) { s.served = append(s.served, e) }

func (s *servedStore) Open(ctx context.Context, it items.Item, rng *items.Range) (io.ReadCloser, error) {
	s.opened++
	return s.store.Open(ctx, it, rng)
}

type switchOn string

func (s switchOn) On(id string) bool { return id == string(s) }

// TestR_STOR_095_AFileOfFilesIsHandedOver: a file of /files/ is handed over like a format's artifact to a recognized forward that
// offered, decided on the gates /files/ decides (R-STOR-095 steps 2 and 3): the file's 200 without its bytes and the value noted
// for the marking, counted once with the delivery handover; a blob without a malware verdict under a gate that is not strict, and
// an ordinary request that sends the offer, stream it.
func TestR_STOR_095_AFileOfFilesIsHandedOver(t *testing.T) {
	const bastionID = "0192f1c4-0000-7000-8000-0000000000b1"
	m := blobtest.New()
	p := "quader"
	conns, err := blob.NewConnections(blob.NewConnection(
		config.StorageConnection{ID: "default", Bucket: "blobs", Prefix: &p, Endpoint: "https://" + blobtest.PresignHost, ForcePathStyle: true},
		blob.NewGuard(m, config.StorageS3Breaker{FailureThreshold: 1, OpenDuration: time.Minute})))
	if err != nil {
		t.Fatal(err)
	}
	links := downloadlinks.New(downloadlinks.Options{Connections: conns, Rule: downloadlinks.HostRuleOf([]string{"quader.example.com"}), Handovers: switchOn(bastionID), Replica: "r1"})
	set := downloadlinks.Defaults()
	set.MinSize = 0
	links.SetSettings(set)
	links.RunLinkChecks(context.Background())
	body := []byte("a file of a few bytes")
	var sum blob.Digest
	sum[0] = 0x5f
	key := blob.Key("quader", sum)
	m.Set(key, body, time.Now())
	file := items.Item{
		ID: 3, RepoID: 5, Repository: qr("generic-local"), Path: "dist/a.bin", Kind: items.KindFile, Size: int64(len(body)), Digest: sum,
		SHA512: make([]byte, 64), ContentStatus: blob.StatusOK, ModifiedAt: time.Now(),
		Copies: []blob.Copy{{Connection: "default", ObjectKey: key, StoredSize: int64(len(body)), State: blob.CopyPresent}}, CopiesRead: true,
	}
	serve := func(t *testing.T, st *servedStore, malw *gate.Gate, recognized bool, hdr ...string) (*httptest.ResponseRecorder, *bastionmain.Absence) {
		t.Helper()
		svc, err := files.New(files.Options{Items: st, Repos: repos{}, Malware: malw})
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodGet, "/files/it/generic-local/dist/a.bin", nil)
		for i := 0; i+1 < len(hdr); i += 2 {
			req.Header.Set(hdr[i], hdr[i+1])
		}
		ctx := req.Context()
		if recognized {
			ctx = bastionmain.WithRecognition(ctx, bastionmain.Recognition{
				Bastion: "dmz", BastionID: bastionID, Interface: iface.InterfaceVersion, Offer: iface.HandoverOffer{MaxSize: 1 << 20}, Offered: true,
			})
		}
		ctx, a := bastionmain.WithAbsence(ctx)
		rec := httptest.NewRecorder()
		New(Options{Service: svc, Decider: &decider{}, Links: links}).ServeHTTP(rec, req.WithContext(ctx))
		return rec, a
	}
	t.Run("R-STOR-095 a recognized forward that offered", func(t *testing.T) {
		st := &servedStore{store: store{file: file, body: body}}
		rec, a := serve(t, st, nil, true)
		if rec.Code != http.StatusOK || rec.Body.Len() != 0 || rec.Header().Get("Content-Length") != "0" || st.opened != 0 || rec.Header().Get("ETag") != file.ETag() {
			t.Fatalf("%d, %d bytes, opened %d, headers %v", rec.Code, rec.Body.Len(), st.opened, rec.Header())
		}
		v, err := iface.DecodeHandover(a.Handover())
		if err != nil || v.SHA256 != sum.Hex() || v.Size != file.Size {
			t.Fatalf("value %v: %v", v, err)
		}
		resp, err := m.Do(httptest.NewRequest(http.MethodGet, v.Link, nil))
		if err != nil {
			t.Fatal(err)
		}
		got, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if !bytes.Equal(got, body) {
			t.Fatalf("the link served %q", got)
		}
		if len(st.served) != 1 || st.served[0].Delivery != items.DeliveryHandover || st.served[0].Bastion != "dmz" || !st.served[0].CountStats {
			t.Fatalf("served %+v", st.served)
		}
	})
	t.Run("R-STOR-095 a blob without a malware verdict under a gate that is not strict streams", func(t *testing.T) {
		unchecked := file
		unchecked.Malware = malware.Verdict{}
		st := &servedStore{store: store{file: unchecked, body: body}}
		rec, a := serve(t, st, gate.New(gate.Options{Enabled: true}), true)
		if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), body) || a.Handover() != "" {
			t.Fatalf("%d %q", rec.Code, rec.Body.String())
		}
	})
	t.Run("R-STOR-095 an ordinary request that sends the offer streams", func(t *testing.T) {
		st := &servedStore{store: store{file: file, body: body}}
		rec, a := serve(t, st, nil, false, iface.HeaderHandover, "accept; max-size=1048576")
		if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), body) || a.Handover() != "" || len(st.served) != 1 || st.served[0].Delivery != "" {
			t.Fatalf("%d %q", rec.Code, rec.Body.String())
		}
	})
}
