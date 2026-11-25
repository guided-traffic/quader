//go:build integration

package ociremote

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/packages/view"
	"github.com/guided-traffic/quader/test/integration/testenv"
	"github.com/guided-traffic/quader/test/ocistub"
)

// TestR_SIG_026_ARemotesSignaturesAreFetchedInTheBackground: a client's pull of acme/signed:1.0 through a remote stores the image;
// the job signature-fetch of the worker then asks the stub for its signatures without a client asking: the referrers API once, the
// .sig and .att tags by HEAD, and stores the referrers and the .sig manifest, whose signatures the commit hook records at the
// repository. Without the referrers API the referrers tag is asked by HEAD; a tag the upstream does not have costs no GET. A remote
// whose detail setting signatures.fetch is false asks nothing. Integration, about 4 s, the stub of R-OCI-035, kilobytes.
func TestR_SIG_026_ARemotesSignaturesAreFetchedInTheBackground(t *testing.T) {
	e := newEnv(t, options{})
	off := map[string]any{"pattern": ocistub.LoopbackRealmHost, "sendCredentials": false}
	cdn := map[string]any{"pattern": ocistub.LoopbackCDNHost, "sendCredentials": false}
	e.repo("oci-nofetch", e.run.URL, "none", 300, off, cdn)
	e.st.Exec(`UPDATE repositories SET config = jsonb_set(config, '{signatures}', '{"fetch": false}') WHERE key = 'oci-nofetch'`)
	subject, ok := e.stub.Digest("acme/signed", "1.0")
	if !ok {
		t.Fatal("the stub has no acme/signed:1.0")
	}
	sigTag := ocistub.FallbackTag(subject) + ".sig"
	sigManifest, _ := e.stub.PutArtifact("acme/signed", sigTag, "application/vnd.oci.image.config.v1+json", []byte(`{}`),
		"application/vnd.dev.cosign.simplesigning.v1+json", []byte(`{"critical":{"image":{"docker-manifest-digest":"`+subject+`"}}}`))
	web := e.start(1)[0]
	e.st.Start(t, testenv.ReplicaOptions{Name: "worker-1", Role: "worker", DialOverride: e.dial})

	attachments := func(repo string) int64 {
		return e.st.Number(`SELECT count(*) FROM signature_attachments a JOIN repositories r ON r.id = a.repo_id WHERE r.key = $1`, repo)
	}
	pull := func(repo string) {
		t.Helper()
		if a := e.doAs(web, e.admin, "pull", http.MethodGet, repo, "acme/signed/manifests/1.0"); a.Status != http.StatusOK {
			t.Fatalf("pull through %s: %d %s", repo, a.Status, a.Body)
		}
	}

	if err := e.stub.SetMode(ocistub.Mode{Referrers: true}); err != nil {
		t.Fatal(err)
	}
	e.stub.Reset()
	pull(rAnon)
	// The two referrers (a signature and an SBOM, each an attachment) and the .sig tag's signature.
	eventually(t, 15*time.Second, "the signatures of the pulled image are stored", func() bool { return attachments(rAnon) >= 3 })
	c := e.stub.Counts()
	if n := answeredReferrers(c, "acme/signed"); n != 1 {
		t.Errorf("%d answered requests of the referrers API, want 1", n)
	}
	if h, g := c.ManifestHeads("acme/signed", sigTag), c.ManifestGets("acme/signed", sigTag); h != 1 || g != 1 {
		t.Errorf(".sig: %d HEAD and %d GET, want one each", h, g)
	}
	att := ocistub.FallbackTag(subject) + ".att"
	if h, g := c.ManifestHeads("acme/signed", att), c.ManifestGets("acme/signed", att); h != 1 || g != 0 {
		t.Errorf(".att, which the stub does not have: %d HEAD and %d GET, want one HEAD", h, g)
	}
	if n := c.ManifestHeads("acme/signed", ocistub.FallbackTag(subject)); n != 0 {
		t.Errorf("the referrers tag was asked %d times while the API answers", n)
	}
	if got := e.st.Text(`SELECT count(*)::text FROM oci_manifests m JOIN repositories r ON r.id = m.repo_id
	                     WHERE r.key = $1 AND m.subject IS NOT NULL`, rAnon); got != "2" {
		t.Errorf("%s referrers stored, want 2", got)
	}
	if got := e.st.Text(`SELECT count(*)::text FROM signature_attachments a JOIN repositories r ON r.id = a.repo_id
	                     WHERE r.key = $1 AND a.source = 'cosign_tag'`, rAnon); got != "1" {
		t.Errorf("%s attachments of the .sig tag (manifest %s), want 1", got, sigManifest)
	}
	// The version of the remote's tag lists what its manifest's file view shows, and the version list counts it (Q-SIG-219): the remote
	// holds the tag as a document and no file (R-OCI-036). The tag is listed once the GET of its manifest counted (R-API-081).
	e.fetched(rAnon, subject)
	id, scoped := view.ID("oci", "acme/signed"), "repository="+url.QueryEscape(testenv.DefaultProject+"/"+rAnon)
	var rep struct {
		Repositories []struct {
			State      string            `json:"state"`
			Signatures []json.RawMessage `json:"signatures"`
		} `json:"repositories"`
	}
	if a := e.st.Do(web, http.MethodGet, "/api/v1/packages/"+id+"/versions/1.0/signatures?"+scoped, e.admin, nil); a.Status != http.StatusOK ||
		json.Unmarshal(a.Body, &rep) != nil || len(rep.Repositories) != 1 || len(rep.Repositories[0].Signatures) == 0 || rep.Repositories[0].State == "none" {
		t.Errorf("the remote's version answers %d %s, want the image's signatures and a badge", a.Status, a.Body)
	}
	var list struct {
		Items []struct {
			Version    string `json:"version"`
			Signatures *struct {
				State string `json:"state"`
			} `json:"signatures"`
		} `json:"items"`
	}
	if a := e.st.Do(web, http.MethodGet, "/api/v1/packages/"+id+"/versions?"+scoped, e.admin, nil); a.Status != http.StatusOK ||
		json.Unmarshal(a.Body, &list) != nil || len(list.Items) != 1 || list.Items[0].Signatures == nil || list.Items[0].Signatures.State == "none" {
		t.Errorf("the remote's version list answers %d %s, want the tag with the badge of its manifest's signatures", a.Status, a.Body)
	}
	// An attachment tag is in no version list and no count of a remote (R-OCI-036, R-SIG-024): not once a client fetched the .sig tag
	// through the remote (cosign verify does), and not in a remote whose every stored tag is listed (a mirror).
	listed := func(repo, what string) {
		t.Helper()
		var got struct {
			Items []struct {
				Version string `json:"version"`
			} `json:"items"`
		}
		a := e.st.Do(web, http.MethodGet, "/api/v1/packages/"+id+"/versions?repository="+url.QueryEscape(testenv.DefaultProject+"/"+repo), e.admin, nil)
		if a.Status != http.StatusOK || json.Unmarshal(a.Body, &got) != nil || len(got.Items) != 1 || got.Items[0].Version != "1.0" {
			t.Errorf("%s: the version list answers %d %s, want the tag 1.0 alone", what, a.Status, a.Body)
		}
	}
	if a := e.doAs(web, e.admin, "pull", http.MethodGet, rAnon, "acme/signed/manifests/"+sigTag); a.Status != http.StatusOK {
		t.Fatalf("the client's GET of the .sig tag: %d %s", a.Status, a.Body)
	}
	e.fetched(rAnon, sigManifest)
	listed(rAnon, "after a client fetched the .sig tag")
	// The fetch state the gate of stage 3 reads (Q-SIG-106): the image's row is fetched, carries its digest and is due no more; it goes
	// with the manifest's row when the remote's manifest item is removed.
	fetchRow := func() string {
		return e.st.Text(`SELECT COALESCE((SELECT (f.fetched_at IS NOT NULL)::text || ' ' || (f.queued_at IS NULL)::text || ' ' || f.attempts::text || ' ' ||
		                     'sha256:' || encode(f.subject_sha256, 'hex') FROM signature_fetches f JOIN repositories r ON r.id = f.repo_id
		                     WHERE r.key = $1 AND f.target = $2), 'none')`, rAnon, "acme/signed@"+subject)
	}
	eventually(t, 10*time.Second, "the fetch row reads fetched", func() bool { return fetchRow() == "true true 0 "+subject })
	del := e.st.Do(web, http.MethodDelete, "/api/v1/projects/"+testenv.DefaultProject+"/repositories/"+rAnon+"/items?path=acme/signed/_manifests/"+subject, e.admin, nil)
	if del.Status != http.StatusNoContent {
		t.Fatalf("the removal of the cached manifest: %d %s", del.Status, del.Body)
	}
	if got := fetchRow(); got != "none" {
		t.Errorf("the fetch row after the manifest's removal: %s, want none", got)
	}

	// Without the referrers API: the referrers tag is asked by HEAD and its index read.
	if err := e.stub.SetMode(ocistub.Mode{}); err != nil {
		t.Fatal(err)
	}
	e.stub.Reset()
	pull(rBasic)
	eventually(t, 15*time.Second, "the signatures are found through the referrers tag", func() bool { return attachments(rBasic) >= 3 })
	if n := e.stub.Counts().ManifestHeads("acme/signed", ocistub.FallbackTag(subject)); n != 1 {
		t.Errorf("the referrers tag was asked %d times by HEAD, want 1", n)
	}
	// A mirror lists every tag it stores; the referrers tag and the .sig tag the fetch stored are still none of its versions.
	e.st.Exec(`INSERT INTO mirror_state (repo_id) SELECT id FROM repositories WHERE key = $1`, rBasic)
	listed(rBasic, "in a mirror")
	e.st.Exec(`DELETE FROM mirror_state WHERE repo_id = (SELECT id FROM repositories WHERE key = $1)`, rBasic)

	// signatures.fetch false: the pull is served and nothing more is asked.
	e.stub.Reset()
	pull("oci-nofetch")
	time.Sleep(2 * time.Second)
	if n := e.stub.Counts().ReferrersRequests("acme/signed") + e.stub.Counts().ManifestHeads("acme/signed", sigTag); n != 0 {
		t.Errorf("a remote whose fetch is off asked %d signature requests", n)
	}
	if got := attachments("oci-nofetch"); got != 0 {
		t.Errorf("%d attachments at a remote whose fetch is off, want 0", got)
	}
}

// answeredReferrers counts the referrers requests of a name that were not a 401 challenge: the token handshake sends each request twice.
func answeredReferrers(c ocistub.Counts, name string) int {
	n := 0
	for _, e := range c.Entries {
		if e.Kind == ocistub.KindReferrers && e.Name == name {
			n += e.Count - e.Statuses["401"]
		}
	}
	return n
}
