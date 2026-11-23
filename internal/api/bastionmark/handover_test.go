package bastionmark

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastionmain"
)

// handoverValue is the encoded value of a plain copy's handover, as the serve point notes it.
func handoverValue(t *testing.T) string {
	t.Helper()
	v, err := iface.EncodeHandover(iface.Handover{
		Link:      "https://objects.example.com/quader-blobs/quader/sha256/9f/86/9f86?X-Amz-Signature=abc",
		ExpiresAt: time.Now().Add(time.Minute).UTC().Truncate(time.Second), Connection: "default",
		SHA256: "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08", Size: 3, StoredSize: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// TestR_BAS_073_OnlyARecognizedForwardThatOfferedGetsAHandover: the class handover and the value in Quader-Bastion-Handover go to
// the 200 of a recognized forward that offered one and whose serve point noted a value, with the rest of the marking (the counted
// download, the package and its shape, R-BAS-082); a forward that did not offer, an ordinary request that sends the offer's header
// and a value a handler set itself get none; a noted value on any other status is no handover.
func TestR_BAS_073_OnlyARecognizedForwardThatOfferedGetsAHandover(t *testing.T) {
	value := handoverValue(t)
	const file = "/api/mk/web/mk-local/f/x.bin"
	t.Run("R-BAS-073 a recognized forward that offered: the class handover, the value, the counted download and the package", func(t *testing.T) {
		r := newRig(t)
		noted := false
		r.notes = func(ctx context.Context) {
			bastionmain.NotePackage(ctx, "left-pad")
			bastionmain.NoteCounted(ctx)
			noted = bastionmain.NoteHandover(ctx, value)
		}
		r.respond = func(w http.ResponseWriter) { w.Header().Set("Content-Length", "0") }
		rec := r.do(http.MethodGet, file, hRecognize, "forward-offer", hCred, "token")
		m := markingOf(t, rec)
		if !noted || m.Class != iface.ClassHandover || rec.Header().Get(iface.HeaderHandover) != value || !m.Counted ||
			m.Package != "left-pad" || m.PackageRead != iface.ReadFull || m.Repository != "web/mk-local" || m.Read != iface.ReadFull {
			t.Fatalf("noted %v, marking %+v, header %q", noted, m, rec.Header().Get(iface.HeaderHandover))
		}
		if got := counter(t, r.metrics, string(iface.ClassHandover)); got != 1 {
			t.Fatalf("forwarded requests of the class handover: %v", got)
		}
	})
	t.Run("R-BAS-073 a forward without the offer: NoteHandover refuses, the class is artifact and no value goes out", func(t *testing.T) {
		r := newRig(t)
		noted := true
		r.notes = func(ctx context.Context) { noted = bastionmain.NoteHandover(ctx, value) }
		rec := r.do(http.MethodGet, file, hRecognize, "forward", hCred, "token")
		if m := markingOf(t, rec); noted || m.Class != iface.ClassArtifact || rec.Header().Get(iface.HeaderHandover) != "" {
			t.Fatalf("noted %v, marking %+v, header %q", noted, m, rec.Header().Get(iface.HeaderHandover))
		}
	})
	t.Run("R-BAS-073 an ordinary request that sends the offer's header gets the file and no Quader-Bastion- header", func(t *testing.T) {
		r := newRig(t)
		noted := true
		r.notes = func(ctx context.Context) { noted = bastionmain.NoteHandover(ctx, value) }
		// A defect of a handler that wrote the header itself is stripped like every marking header of an ordinary request.
		r.respond = func(w http.ResponseWriter) { w.Header().Set(iface.HeaderHandover, value) }
		rec := r.do(http.MethodGet, file, iface.HeaderHandover, "accept; max-size=1048576", hCred, "token")
		if noted || rec.Code != http.StatusOK || len(hasMarking(rec.Header())) != 0 {
			t.Fatalf("noted %v, %d, marking headers %v", noted, rec.Code, hasMarking(rec.Header()))
		}
	})
	t.Run("R-BAS-073 the marking decides on the recognition it was given, not on one a handler forged into its context", func(t *testing.T) {
		r := newRig(t)
		noted := false
		r.notes = func(ctx context.Context) {
			// Only the recognition stage may call WithRecognition; a defect that does so behind it reaches the record, not the marking.
			forged := bastionmain.WithRecognition(ctx, bastionmain.Recognition{
				Bastion: "edge-1", Interface: iface.InterfaceVersion,
				Offer: iface.HandoverOffer{MaxSize: 1 << 20}, Offered: true,
			})
			noted = bastionmain.NoteHandover(forged, value)
		}
		rec := r.do(http.MethodGet, file, hRecognize, "forward", hCred, "token")
		if m := markingOf(t, rec); !noted || m.Class != iface.ClassArtifact || rec.Header().Get(iface.HeaderHandover) != "" {
			t.Fatalf("noted %v, marking %+v, header %q", noted, m, rec.Header().Get(iface.HeaderHandover))
		}
	})
	t.Run("R-BAS-073 a value a handler writes itself never reaches a Bastion: only the noted value does", func(t *testing.T) {
		r := newRig(t)
		r.respond = func(w http.ResponseWriter) { w.Header().Set(iface.HeaderHandover, value) }
		rec := r.do(http.MethodGet, file, hRecognize, "forward-offer", hCred, "token")
		if m := markingOf(t, rec); m.Class != iface.ClassArtifact || rec.Header().Get(iface.HeaderHandover) != "" {
			t.Fatalf("marking %+v, header %q", m, rec.Header().Get(iface.HeaderHandover))
		}
	})
	for _, status := range []int{http.StatusNotModified, http.StatusNotFound, http.StatusTooManyRequests} {
		t.Run("R-BAS-073 a noted value on a "+http.StatusText(status)+" is no handover", func(t *testing.T) {
			r := newRig(t)
			r.status = status
			r.notes = func(ctx context.Context) { bastionmain.NoteHandover(ctx, value) }
			rec := r.do(http.MethodGet, file, hRecognize, "forward-offer", hCred, "token")
			if m := markingOf(t, rec); m.Class == iface.ClassHandover || rec.Header().Get(iface.HeaderHandover) != "" {
				t.Fatalf("marking %+v, header %q", m, rec.Header().Get(iface.HeaderHandover))
			}
		})
	}
}
