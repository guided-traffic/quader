package npm

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/formattest"
	"github.com/guided-traffic/quader/internal/formats/npm/npmtest"
	"github.com/guided-traffic/quader/internal/formats/npm/rows"
	"github.com/guided-traffic/quader/internal/repository"
)

// quaderKeys are Quader's keys as the data port answers them: the current identity's and a previous one's with its end of signing.
func quaderKeys() []rows.RegistryKey {
	return []rows.RegistryKey{
		{KeyID: "SHA256:current", KeyType: "ecdsa-sha2-nistp256", Scheme: "ecdsa-sha2-nistp256", Key: "MFkw-current"},
		{KeyID: "SHA256:previous", KeyType: "ecdsa-sha2-nistp256", Scheme: "ecdsa-sha2-nistp256", Key: "MFkw-previous", Expires: "2027-02-01T10:00:00.000Z"},
	}
}

const quaderKeysJSON = `{"keys":[` +
	`{"expires":null,"keyid":"SHA256:current","keytype":"ecdsa-sha2-nistp256","scheme":"ecdsa-sha2-nistp256","key":"MFkw-current"},` +
	`{"expires":"2027-02-01T10:00:00.000Z","keyid":"SHA256:previous","keytype":"ecdsa-sha2-nistp256","scheme":"ecdsa-sha2-nistp256","key":"MFkw-previous"}]}`

// TestR_SIG_050_ALocalRepositoryAnswersQuadersKeys: the keys endpoint of a local repository answers Quader's keys from the data port in
// npmjs's form, the expiry null for a key that signs and the moment a previous one stopped signing; with no identity current the data
// port answers none and the document is {"keys":[]} (R-SIG-045). Unit, milliseconds, bytes.
func TestR_SIG_050_ALocalRepositoryAnswersQuadersKeys(t *testing.T) {
	rf := newRepo(t)
	if r := rf.get("-/npm/v1/keys"); r.Code != 200 || r.Body.String() != `{"keys":[]}` {
		t.Fatalf("without an identity: %d %s", r.Code, r.Body)
	}
	rf.st.Keys = quaderKeys()
	if r := rf.get("-/npm/v1/keys"); r.Code != 200 || r.Body.String() != quaderKeysJSON {
		t.Fatalf("with an identity: %d %s", r.Code, r.Body)
	}
}

// TestR_SIG_050_AVirtualRepositoryAddsQuadersKeysForALocalMember: a virtual repository answers the union of its members' keys by keyid
// in member order: a remote member's upstream keys and, for its local members, Quader's keys once; a disabled local member adds none, and
// a virtual repository of remotes alone answers its upstreams' keys (R-SIG-028, R-SIG-050, H-1176). Unit, milliseconds, bytes.
func TestR_SIG_050_AVirtualRepositoryAddsQuadersKeysForALocalMember(t *testing.T) {
	stub := `{"keys":[{"expires":null,"keyid":"SHA256:stub","keytype":"ecdsa-sha2-nistp256","scheme":"ecdsa-sha2-nistp256","key":"MFkw-stub"}]}`
	eng := &scriptedRemote{metadata: func(string) (*formats.RemoteDocument, error) {
		return &formats.RemoteDocument{Bytes: func(context.Context) ([]byte, error) { return []byte(stub), nil }}, nil
	}}
	member := func(id, key string, class formats.Class, disabled bool) repository.Repository {
		return repository.Repository{ID: id, Name: qr(key), Class: class, Format: "npm", Lifecycle: repository.Active, Config: []byte(`{}`), Disabled: disabled}
	}
	keyIDs := func(members ...repository.Repository) []string {
		twin := npmtest.New()
		twin.Keys = quaderKeys()
		h := formattest.NewHost(New(), formattest.HostOptions{Class: formats.Virtual, RepoKey: "npm-all", Data: twin, Remote: eng})
		*h.Repo() = repository.Assemble(*h.Repo(), members, nil, 60)
		resp, err := h.Serve(context.Background(), http.MethodGet, "-/npm/v1/keys", nil, false)
		if err != nil || resp.Code != 200 {
			t.Fatalf("%v %d %s", err, resp.Code, resp.Body)
		}
		var d struct {
			Keys []struct {
				KeyID string `json:"keyid"`
			} `json:"keys"`
		}
		if err := json.Unmarshal(resp.Body.Bytes(), &d); err != nil {
			t.Fatal(err)
		}
		out := []string{}
		for _, k := range d.Keys {
			out = append(out, k.KeyID)
		}
		return out
	}
	for name, tc := range map[string]struct {
		members []repository.Repository
		want    []string
	}{
		"local before remote": {
			[]repository.Repository{member("2", "lib", formats.Local, false), member("3", "up", formats.Remote, false), member("4", "lib2", formats.Local, false)},
			[]string{"SHA256:current", "SHA256:previous", "SHA256:stub"},
		},
		"remote before local": {
			[]repository.Repository{member("3", "up", formats.Remote, false), member("2", "lib", formats.Local, false)},
			[]string{"SHA256:stub", "SHA256:current", "SHA256:previous"},
		},
		"a disabled local member": {
			[]repository.Repository{member("2", "lib", formats.Local, true), member("3", "up", formats.Remote, false)},
			[]string{"SHA256:stub"},
		},
		"remotes alone": {[]repository.Repository{member("3", "up", formats.Remote, false)}, []string{"SHA256:stub"}},
	} {
		if got := keyIDs(tc.members...); !equalStrings(got, tc.want) {
			t.Errorf("%s: keys %v, want %v", name, got, tc.want)
		}
	}
}

// TestR_SIG_050_DistSignaturesIsRenderedFromTheRowsInEveryDocument: a version whose rows carry Quader's registry signatures renders them
// as dist.signatures in the full, the abbreviated and the version document, beside dist.integrity; a version without one carries no
// dist.signatures. The publish hands the data port the tarball's item, whose work the job signing takes (R-SIG-049). Unit,
// milliseconds, bytes.
func TestR_SIG_050_DistSignaturesIsRenderedFromTheRowsInEveryDocument(t *testing.T) {
	rf := newRepo(t)
	rf.publish("@acme/ui", "1.0.0")
	rf.publish("@acme/ui", "1.1.0")
	if len(rf.st.Published) != 2 || rf.st.Published[0].ItemID == 0 || rf.st.Published[0].ItemID == rf.st.Published[1].ItemID {
		t.Fatalf("the publishes hand no tarball item: %+v", rf.st.Published)
	}
	sigs := `[{"keyid":"SHA256:current","sig":"MEUCIQ=="}]`
	rf.st.SetSignatures(rf.h.Ref().ID, "@acme/ui", "1.0.0", []byte(sigs))

	type dist struct {
		Integrity  string          `json:"integrity"`
		Signatures json.RawMessage `json:"signatures"`
	}
	distOf := func(r *formattest.Response) map[string]dist {
		var d struct {
			Versions map[string]struct {
				Dist dist `json:"dist"`
			} `json:"versions"`
		}
		if err := json.Unmarshal(r.Body.Bytes(), &d); err != nil || r.Code != 200 {
			t.Fatalf("%d %s: %v", r.Code, r.Body, err)
		}
		out := map[string]dist{}
		for v, e := range d.Versions {
			out[v] = e.Dist
		}
		return out
	}
	for name, r := range map[string]*formattest.Response{
		"full":        rf.get("@acme%2fui"),
		"abbreviated": rf.get("@acme%2fui", "Accept", "application/vnd.npm.install-v1+json"),
	} {
		d := distOf(r)
		if string(d["1.0.0"].Signatures) != sigs || d["1.0.0"].Integrity == "" {
			t.Errorf("%s: 1.0.0 dist %+v", name, d["1.0.0"])
		}
		if d["1.1.0"].Signatures != nil {
			t.Errorf("%s: 1.1.0 carries signatures %s", name, d["1.1.0"].Signatures)
		}
	}
	var one struct {
		Dist dist `json:"dist"`
	}
	r := rf.get("@acme%2fui/1.0.0")
	if err := json.Unmarshal(r.Body.Bytes(), &one); err != nil || string(one.Dist.Signatures) != sigs {
		t.Errorf("the version document: %d %s", r.Code, r.Body)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
