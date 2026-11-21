package npm

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
	"reflect"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/npm/npmtest"
	"github.com/guided-traffic/quader/internal/formats/npm/remote"
	"github.com/guided-traffic/quader/internal/mirror/discover"
)

// TestR_SIG_028_TheSignatureEndpointsAreRoutesOfTheirOwn: the keys and a version's attestations are matched for every spelling of a
// scoped name, are no deferred endpoint any more, and classify as documents (the attestations of their version). Unit, microseconds.
func TestR_SIG_028_TheSignatureEndpointsAreRoutesOfTheirOwn(t *testing.T) {
	for path, want := range map[string]string{
		"-/npm/v1/attestations/lodash@1.0.0":     "lodash",
		"-/npm/v1/attestations/@acme%2fui@1.2.0": "@acme%2fui",
		"-/npm/v1/attestations/@acme/ui@1.2.0":   "@acme/ui",
	} {
		p, ok := matchAttestations(path)
		if !ok || p["name"] != want {
			t.Errorf("%s: %v %v, want the name %q", path, p, ok, want)
		}
		if _, deferred := matchDeferred(path); deferred {
			t.Errorf("%s is still a deferred endpoint", path)
		}
	}
	for _, path := range []string{"-/npm/v1/attestations/lodash", "-/npm/v1/attestations/@1.0.0", "-/npm/v1/attestation/lodash@1.0.0"} {
		if _, ok := matchAttestations(path); ok {
			t.Errorf("%s matches the attestations", path)
		}
	}
	if _, ok := matchKeys(KeysPath); !ok {
		t.Error("the keys path does not match")
	}
	if _, deferred := matchDeferred(KeysPath); deferred {
		t.Error("the keys are still a deferred endpoint")
	}
	f := New()
	if got := f.Classify(formats.Path{Path: "-/npm/v1/attestations/@acme/ui@1.2.0"}); got.Kind != formats.PathMetadata || got.Package != "@acme/ui" || got.Version != "1.2.0" {
		t.Errorf("classify attestations = %+v", got)
	}
	if got := f.Classify(formats.Path{Path: KeysPath}); got.Kind != formats.PathMetadata || got.Package != "" {
		t.Errorf("classify keys = %+v", got)
	}
}

// TestR_SIG_028_AVirtualRepositoryServesTheUnionOfItsMembersKeys: the union by keyid in member order, a key two members announce
// once (Q-SIG-34). Unit, microseconds.
func TestR_SIG_028_AVirtualRepositoryServesTheUnionOfItsMembersKeys(t *testing.T) {
	a, _ := parseKeys([]byte(`{"keys":[{"keyid":"SHA256:a","key":"MFkw-a"},{"keyid":"SHA256:b","key":"MFkw-b"}]}`))
	b, _ := parseKeys([]byte(`{"keys":[{"keyid":"SHA256:b","key":"MFkw-b"},{"keyid":"SHA256:c","key":"MFkw-c"},{"key":"no id"}]}`))
	got := unionKeys([]keysDocument{a, b})
	var ids []string
	for _, k := range got.Keys {
		ids = append(ids, keyID(k))
	}
	if !reflect.DeepEqual(ids, []string{"SHA256:a", "SHA256:b", "SHA256:c"}) {
		t.Errorf("union %v", ids)
	}
	if empty := unionKeys(nil); empty.Keys == nil || len(empty.Keys) != 0 {
		t.Errorf("no member's keys = %#v, want an empty list", empty.Keys)
	}
	if _, err := parseKeys([]byte(`{"keys":"x"}`)); err == nil {
		t.Error("a keys document of the wrong shape is read")
	}
}

// TestR_SIG_028_TheAttestationsURLReachesTheServingRepository: dist.attestations.url of a served document names the serving
// repository's attestations endpoint (npm resolves its path against the registry URL, Q-SIG-35); its provenance and dist.signatures
// pass byte for byte. Unit, microseconds.
func TestR_SIG_028_TheAttestationsURLReachesTheServingRepository(t *testing.T) {
	entry := json.RawMessage(`{"name":"@acme/ui","version":"1.2.0","dist":{"integrity":"sha512-x","signatures":[{"keyid":"k","sig":"s"}],` +
		`"attestations":{"url":"https://registry.npmjs.org/-/npm/v1/attestations/@acme%2fui@1.2.0","provenance":{"predicateType":"https://slsa.dev/provenance/v1"}}}}`)
	registry := "https://quader.example.com/api/npm/web/npm-remote/"
	out, err := remote.Entry(entry, remote.TarballPrefix(registry, "@acme/ui"), "@acme/ui", "1.2.0", nil)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Dist struct {
			Signatures   json.RawMessage `json:"signatures"`
			Attestations struct {
				URL        string          `json:"url"`
				Provenance json.RawMessage `json:"provenance"`
			} `json:"attestations"`
		} `json:"dist"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if want := registry + "-/npm/v1/attestations/@acme/ui@1.2.0"; got.Dist.Attestations.URL != want {
		t.Errorf("url %q, want %q", got.Dist.Attestations.URL, want)
	}
	if string(got.Dist.Signatures) != `[{"keyid":"k","sig":"s"}]` || !bytes.Contains(got.Dist.Attestations.Provenance, []byte("slsa.dev")) {
		t.Errorf("a member changed: %s", out)
	}
	if _, ok := matchAttestations("-/npm/v1/attestations/@acme/ui@1.2.0"); !ok {
		t.Error("the rewritten path is not the endpoint's")
	}
	u, err := signatureUpstreamRequest(context.Background(), mustURL(t, "https://registry.npmjs.org/"), mustName("@acme/ui"), "1.2.0", false)
	if err != nil || u.URL.String() != "https://registry.npmjs.org/-/npm/v1/attestations/@acme%2Fui@1.2.0" {
		t.Errorf("the upstream request %v %v", u.URL, err)
	}
}

// storedUpstream is the upstream of a background run: stored package documents without a request, and the documents it was asked for.
type storedUpstream struct {
	discover.Upstream
	stored map[string]string
	asked  []string
}

func (s *storedUpstream) StoredDocument(_ context.Context, path, _ string) ([]byte, bool, error) {
	d, ok := s.stored[path]
	return []byte(d), ok, nil
}

func (s *storedUpstream) Document(_ context.Context, path, _ string) ([]byte, bool, error) {
	s.asked = append(s.asked, path)
	return []byte(`{}`), true, nil
}

func (s *storedUpstream) Base() *url.URL { u, _ := url.Parse("https://registry.npmjs.org/"); return u }

// TestR_SIG_026_TheFetchOfAnNPMVersionAsksForItsAttestationsWhenItNamesThem: a version whose stored entry names dist.attestations
// costs one request of its attestations and the keys; a version without them costs none; the target keys asks for the keys alone.
// Unit, microseconds.
func TestR_SIG_026_TheFetchOfAnNPMVersionAsksForItsAttestationsWhenItNamesThem(t *testing.T) {
	up := &storedUpstream{stored: map[string]string{
		"lodash": `{"versions":{"1.0.0":{"dist":{"attestations":{"url":"x"}}},"0.9.0":{"dist":{}}}}`,
	}}
	a := New().Remote().(remoteAdapter)
	for _, target := range []string{"lodash@1.0.0", "lodash@0.9.0", "lodash@2.0.0", "lodash", "keys"} {
		if err := a.FetchSignatures(context.Background(), up, target); err != nil {
			t.Fatalf("%s: %v", target, err)
		}
	}
	want := []string{"-/npm/v1/attestations/lodash@1.0.0", KeysPath, KeysPath}
	if !reflect.DeepEqual(up.asked, want) {
		t.Errorf("asked %v, want %v", up.asked, want)
	}
}

// TestR_SIG_026_AnNPMFillQueuesTheFetchOfTheKeys: the fill of a remote's package document queues the background fetch of the keys
// document (once a period: a refresh). Unit, microseconds.
func TestR_SIG_026_AnNPMFillQueuesTheFetchOfTheKeys(t *testing.T) {
	twin := npmtest.New()
	fill(t, New(), twin, upstreamBody("react", "2026-10-30T11:00:00.000Z", "18.0.0"), formats.FillEvent{Digest: [32]byte{1}, FetchedAt: time.Now()})
	if !reflect.DeepEqual(twin.Fetches, []string{"+keys"}) {
		t.Errorf("queued %v, want the refresh of the keys", twin.Fetches)
	}
}
