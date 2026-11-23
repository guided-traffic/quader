package ansible

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/ansible/collection/collectiontest"
	"github.com/guided-traffic/quader/internal/formats/formattest"
)

// TestR_SIG_068_TheVersionDocumentAnswersWhatTheArchivesDownloadWould: ansible-galaxy prints the text of an error of the version
// document and only the status of the archive's (Q-SIG-241), so a local repository's version document asks the checks of the archive's
// download (ItemStore.Check) and answers their refusal or pending answer in Galaxy's error shape, the detail first; a version whose
// archive passes is served as before, and the check is asked of the archive, by its path. Unit, milliseconds, a collection of a few
// hundred bytes.
func TestR_SIG_068_TheVersionDocumentAnswersWhatTheArchivesDownloadWould(t *testing.T) {
	text := "Quader refused acme.tools:1.0.0 of it/main: it has no signature of its publisher or its build, and this repository serves " +
		"only artifacts with a verified signature. Details: https://quader.test/ui/projects/it/repositories/main/files/x"
	for _, c := range []struct {
		name   string
		answer error
		status int
		code   string
	}{
		{"a refusal of the signature gate", errs.E(errs.Forbidden, errs.CodeSignatureNotVerified, text), http.StatusForbidden, "signature_not_verified"},
		{"a pending check", errs.E(errs.Unavailable, errs.CodeSignatureCheckPending, "Quader is checking the signatures of acme.tools:1.0.0. Try again in a minute.",
			errs.WithRetryAfter(30*time.Second)), http.StatusServiceUnavailable, "signature_check_pending"},
		{"a refusal of the malware gate", errs.E(errs.Forbidden, errs.CodeMalwareFound, "Quader refused it: malware."), http.StatusForbidden, "malware_found"},
		{"a pass", nil, http.StatusOK, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			var asked []string
			o := hostOptions()
			o.Check = func(_ context.Context, _ *formats.Request, it formats.Item) error {
				asked = append(asked, it.Ref.Path)
				return c.answer
			}
			h := formattest.NewHost(New(), o)
			if _, err := publish(h, collectiontest.Build(collectionSpec("1.0.0")), true, ""); err != nil {
				t.Fatal(err)
			}
			r := get(t, h, "v3/collections/acme/tools/versions/1.0.0/")
			if r.Code != c.status || len(asked) != 1 || asked[0] != ArtifactPath("acme", "tools", "1.0.0") {
				t.Fatalf("status %d (want %d), the check asked of %v", r.Code, c.status, asked)
			}
			if c.answer == nil {
				return
			}
			var b struct {
				Errors []struct{ Status, Code, Detail string }
			}
			if err := json.Unmarshal(r.Body.Bytes(), &b); err != nil || len(b.Errors) != 1 || b.Errors[0].Code != c.code ||
				b.Errors[0].Detail != detailOf(c.answer) {
				t.Fatalf("body %s", r.Body.String())
			}
			if c.status == http.StatusServiceUnavailable && r.Header().Get("Retry-After") != "30" {
				t.Fatalf("Retry-After %q", r.Header().Get("Retry-After"))
			}
		})
	}
}

// cachedRemote is a remote engine whose cache holds the archives it names, and nothing else.
type cachedRemote struct {
	*formattest.MemRemote
	archives map[string]bool
}

func (r cachedRemote) Cached(_ context.Context, rq *formats.Request, p formats.Path) (formats.Item, bool, error) {
	if !r.archives[p.Path] {
		return formats.Item{}, false, nil
	}
	return formats.Item{Ref: formats.ItemRef{Repo: rq.RepoRef(), Path: p.Path}}, true, nil
}

// TestR_SIG_068_ARemotesAndAVirtualsVersionDocumentAnswerWhatTheirArchivesDownloadWould: a remote repository asks the checks of the
// archive its cache holds, and not of one it does not hold yet; a virtual repository asks them of the archive its resolution would serve
// first from what its members store (Resolver.Check, Q-SIG-241). Unit, milliseconds, the kit's remote documents.
func TestR_SIG_068_ARemotesAndAVirtualsVersionDocumentAnswerWhatTheirArchivesDownloadWould(t *testing.T) {
	archive := ArtifactPath("acme", "tools", "1.0.0")
	refusal := errs.E(errs.Forbidden, errs.CodeSignatureNotVerified, "Quader refused acme.tools:1.0.0 of it/main: no trust anchor this repository accepts knows the signer of its publisher's or its build's signature.")
	t.Run("remote", func(t *testing.T) {
		for _, cached := range []bool{true, false} {
			docs := map[string][]byte{}
			for _, d := range remoteFixture().Docs {
				docs[d.Path] = d.Body
			}
			var asked []string
			o := hostOptions()
			o.Class = formats.Remote
			o.Remote = cachedRemote{MemRemote: &formattest.MemRemote{Docs: docs}, archives: map[string]bool{archive: cached}}
			o.Check = func(_ context.Context, _ *formats.Request, it formats.Item) error {
				asked = append(asked, it.Ref.Path)
				return refusal
			}
			r := get(t, formattest.NewHost(New(), o), "v3/collections/acme/tools/versions/1.0.0/")
			switch {
			case cached && (r.Code != http.StatusForbidden || len(asked) != 1 || asked[0] != archive):
				t.Fatalf("a cached archive: status %d, asked %v: %s", r.Code, asked, r.Body.String())
			case !cached && (r.Code != http.StatusOK || len(asked) != 0):
				t.Fatalf("an archive not cached: status %d, asked %v", r.Code, asked)
			}
		}
	})
	t.Run("virtual", func(t *testing.T) {
		w := newWorld(t, local("loc", 1, 0), remote("rem", 2, 1))
		w.doc("collections/acme/tools/1.0.0.json", "rem", upstreamVersion("acme", "tools", "1.0.0", "upstream.example", sha(3)))
		w.res.refuse = map[string]error{archive: refusal}
		r := w.get(versionsPath + "1.0.0/")
		if r.Code != http.StatusForbidden || len(w.res.checked) != 1 || w.res.checked[0] != archive || !strings.Contains(r.Body.String(), "knows the signer") {
			t.Fatalf("status %d, checked %v: %s", r.Code, w.res.checked, r.Body.String())
		}
		delete(w.res.refuse, archive)
		if r := w.get(versionsPath + "1.0.0/"); r.Code != http.StatusOK {
			t.Fatalf("a passing archive: %d", r.Code)
		}
	})
}

func detailOf(err error) string {
	p, _ := errs.ProblemOf(err, "", "")
	return p.Detail
}
