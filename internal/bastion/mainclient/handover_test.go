package mainclient_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastion/mainclient"
)

// handoverOf is a plain handover of the fake's file "blob": a kilobyte, a link that lives for a minute.
func handoverOf() iface.Handover {
	return iface.Handover{
		Link: "https://objects.example.com/quader-blobs/sha256/" + strings.Repeat("ab", 32) + "?X-Amz-Signature=00", ExpiresAt: time.Now().Add(time.Minute),
		Connection: "default", SHA256: strings.Repeat("ab", 32), Size: 1024, StoredSize: 1024,
	}
}

// TestR_BAS_073_TheClientAndTheFakeSpeakVersion6: at interface version 6 the handshake carries the registration's switch; a
// caller cannot send an offer through the client, so the fake hands nothing over and streams; the fake hands over to a
// forward that offered, while its switch is on and the blob is within the offer, with the answer R-BAS-073 describes; and an
// answer of the class handover, or one that carries a handover's value, is a bad answer to the client, which offers none yet.
func TestR_BAS_073_TheClientAndTheFakeSpeakVersion6(t *testing.T) {
	r := newRig(t, nil)
	ctx := context.Background()
	for _, on := range []bool{false, true} {
		r.main.SetHandover(on)
		hs, err := r.c.Handshake(ctx)
		// The handover came with version 6; since version 8 it is a switch of the configuration (R-BAS-093).
		if err != nil || hs.Configuration.Switches.Handover != on || hs.Configuration.Validate() != nil || hs.InterfaceVersion != iface.InterfaceVersion || iface.InterfaceVersion < 6 {
			t.Fatalf("handshake with the switch %v: %+v %v", on, hs, err)
		}
	}

	h := handoverOf()
	mk := iface.Marking{Class: iface.ClassArtifact, Repository: "web/generic", Read: iface.ReadFull, Subject: iface.Subject{Kind: iface.SubjectAnonymous}}
	r.main.SetForward(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("ETag", `"`+h.SHA256+`"`)
		if r.main.HandOver(w, req, mk, h) {
			return
		}
		r.main.Mark(w, mk)
		_, _ = w.Write([]byte("bytes"))
	})
	resp, err := r.c.Do(ctx, forward(http.MethodGet, "/files/web/generic/blob", iface.HeaderHandover, "accept; max-size=1048576"))
	if err != nil || resp.Marking.Class != iface.ClassArtifact || closeBody(t, resp) != "bytes" {
		t.Fatalf("a caller's offer: %+v %v", resp, err)
	}
	if fw := r.main.Forwards(); len(fw) != 1 || fw[0].Header.Get(iface.HeaderHandover) != "" {
		t.Fatalf("the caller's offer reached the main installation: %+v", fw)
	}

	// The fake's own rule, with a forward as the Bastion's engine will send it.
	type answer struct {
		status int
		header http.Header
		body   []byte
	}
	raw := func(offer string) answer {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.main.URL()+"/files/web/generic/blob", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set(iface.HeaderInterface, strconv.Itoa(iface.InterfaceVersion))
		req.Header.Set(iface.HeaderAuthorization, iface.BearerScheme+" "+r.token)
		if offer != "" {
			req.Header.Set(iface.HeaderHandover, offer)
		}
		resp, err := (&http.Client{Transport: r.main.Transport()}).Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		return answer{status: resp.StatusCode, header: resp.Header, body: body}
	}
	got := raw("accept; max-size=1024")
	m, marked, err := iface.ParseMarking(got.header)
	back, ok, herr := iface.HandoverFrom(got.header)
	if got.status != http.StatusOK || len(got.body) != 0 || got.header.Get("Content-Length") != "0" || !marked || err != nil ||
		m.Class != iface.ClassHandover || !m.Counted || m.Repository != "web/generic" || got.header.Get("ETag") != `"`+h.SHA256+`"` ||
		!ok || herr != nil || back.Link != h.Link || back.Size != h.Size {
		t.Fatalf("the handover answer: %d %q %v marking %+v %v; value %+v %v %v", got.status, got.body, got.header, m, err, back, ok, herr)
	}
	if offer, ok := r.main.Forwards()[1].Offer(); !ok || offer.MaxSize != 1024 {
		t.Fatalf("the fake recorded the offer %+v %v", offer, ok)
	}
	for name, offer := range map[string]string{"no offer": "", "a blob above the offer": "accept; max-size=1023", "a malformed offer": "accept; max-size=01"} {
		if got := raw(offer); got.header.Get(iface.HeaderHandover) != "" || got.header.Get(iface.HeaderClass) != string(iface.ClassArtifact) || string(got.body) != "bytes" {
			t.Errorf("%s: %v %q", name, got.header, got.body)
		}
	}
	r.main.SetHandover(false)
	if got := raw("accept; max-size=1048576"); got.header.Get(iface.HeaderHandover) != "" || string(got.body) != "bytes" {
		t.Errorf("the switch is off and the fake handed over: %v", got.header)
	}

	// A main installation that answers with a handover although the client offered none.
	value, err := iface.EncodeHandover(h)
	if err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct {
		class iface.Class
		value string
	}{
		"the class handover with its value":    {iface.ClassHandover, value},
		"the class handover without its value": {iface.ClassHandover, ""},
		"a value on an artifact":               {iface.ClassArtifact, value},
	} {
		r.main.SetForward(func(w http.ResponseWriter, _ *http.Request) {
			r.main.Mark(w, iface.Marking{Class: c.class, Subject: iface.Subject{Kind: iface.SubjectAnonymous}, Counted: true})
			if c.value != "" {
				w.Header().Set(iface.HeaderHandover, c.value)
			}
			w.Header().Set("Content-Length", "0")
		})
		if resp, err := r.c.Do(ctx, forward(http.MethodGet, "/files/web/generic/blob")); !errors.Is(err, mainclient.ErrBadAnswer) {
			t.Errorf("%s: %+v %v, want ErrBadAnswer", name, resp, err)
		}
	}
}

// TestR_BAS_073_TheClientTakesAHandoverOnlyWhenTheForwardOffered: the engine's offer reaches the main installation as the
// one value of the header, after the caller's own was dropped; an answer of the class handover to a forward that offered
// is a Response with the value, the marking and the file's headers and without any Quader-Bastion- header; a value the
// interface refuses, or another status, arrives as HandoverErr for the engine to fall back; a value on an answer of
// another class stays a bad answer, offered or not. The Bastion's own continuation sends its Range and If-Range, never
// the caller's.
func TestR_BAS_073_TheClientTakesAHandoverOnlyWhenTheForwardOffered(t *testing.T) {
	r := newRig(t, nil)
	ctx := context.Background()
	r.main.SetHandover(true)
	h := handoverOf()
	mk := iface.Marking{Class: iface.ClassArtifact, Repository: "web/generic", Read: iface.ReadFull, Subject: iface.Subject{Kind: iface.SubjectAnonymous}}
	r.main.SetForward(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("ETag", `"`+h.SHA256+`"`)
		w.Header().Set("Content-Type", "application/octet-stream")
		if !r.main.HandOver(w, req, mk, h) {
			r.main.Mark(w, mk)
			_, _ = w.Write([]byte("bytes"))
		}
	})
	offered := func(hdr ...string) mainclient.Forward {
		f := forward(http.MethodGet, "/files/web/generic/blob", hdr...)
		f.Offer = iface.HandoverOffer{MaxSize: 1 << 20}
		return f
	}
	resp, err := r.c.Do(ctx, offered(iface.HeaderHandover, "accept; max-size=1"))
	if err != nil {
		t.Fatal(err)
	}
	if body := closeBody(t, resp); body != "" || resp.Status != http.StatusOK || resp.Marking.Class != iface.ClassHandover || !resp.Marking.Counted ||
		resp.Handover == nil || resp.HandoverErr != nil || resp.Handover.Link != h.Link || resp.Handover.Size != h.Size ||
		resp.Header.Get("Etag") != `"`+h.SHA256+`"` || resp.Header.Get("Content-Type") != "application/octet-stream" {
		t.Fatalf("the handover answer: %+v %q", resp, body)
	}
	for k := range resp.Header {
		if strings.HasPrefix(k, iface.HeaderPrefix) {
			t.Errorf("the engine sees %s", k)
		}
	}
	fw := r.main.Forwards()
	if got := fw[len(fw)-1].Header.Values(iface.HeaderHandover); len(got) != 1 || got[0] != "accept; max-size=1048576" {
		t.Fatalf("the offer the main installation received: %q", got)
	}

	// A value the interface refuses, or an answer that is not a 200, is for the engine to replace.
	for name, write := range map[string]func(w http.ResponseWriter){
		"a malformed value": func(w http.ResponseWriter) { w.Header().Set(iface.HeaderHandover, "not-base64url!") },
		"no value":          func(http.ResponseWriter) {},
		"a 203": func(w http.ResponseWriter) {
			v, _ := iface.EncodeHandover(handoverOf())
			w.Header().Set(iface.HeaderHandover, v)
			w.WriteHeader(http.StatusNonAuthoritativeInfo)
		},
	} {
		r.main.SetForward(func(w http.ResponseWriter, _ *http.Request) {
			r.main.Mark(w, iface.Marking{Class: iface.ClassHandover, Subject: iface.Subject{Kind: iface.SubjectAnonymous}, Counted: true})
			w.Header().Set("Content-Length", "0")
			write(w)
		})
		resp, err := r.c.Do(ctx, offered())
		if err != nil || resp.Handover != nil || resp.HandoverErr == nil || strings.Contains(resp.HandoverErr.Error(), "objects.example.com") {
			t.Errorf("%s: %+v %v", name, resp, err)
			continue
		}
		_ = closeBody(t, resp)
	}
	// A value on an answer of another class is a defect of the main installation, offered or not.
	value, _ := iface.EncodeHandover(handoverOf())
	r.main.SetForward(func(w http.ResponseWriter, _ *http.Request) {
		r.main.Mark(w, mk)
		w.Header().Set(iface.HeaderHandover, value)
		_, _ = w.Write([]byte("bytes"))
	})
	if resp, err := r.c.Do(ctx, offered()); !errors.Is(err, mainclient.ErrBadAnswer) {
		t.Errorf("a value on an artifact to a forward that offered: %+v %v", resp, err)
	}

	// The continuation: the Bastion's own Range and If-Range, without the offer, and not the caller's.
	r.main.SetForward(func(w http.ResponseWriter, _ *http.Request) {
		r.main.Mark(w, mk)
		w.WriteHeader(http.StatusPartialContent)
	})
	f := forward(http.MethodGet, "/files/web/generic/blob", "Range", "bytes=0-1", "If-Range", `"caller"`)
	f.RangeFrom, f.IfRange = 131072, `"`+h.SHA256+`"`
	resp, err = r.c.Do(ctx, f)
	if err != nil {
		t.Fatal(err)
	}
	_ = closeBody(t, resp)
	fw = r.main.Forwards()
	last := fw[len(fw)-1].Header
	if last.Values("Range")[0] != "bytes=131072-" || len(last.Values("Range")) != 1 || last.Get("If-Range") != `"`+h.SHA256+`"` || last.Get(iface.HeaderHandover) != "" {
		t.Fatalf("the continuation's headers: %v", last)
	}
}
