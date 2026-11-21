package v1

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sigstore/sigstore-go/pkg/root"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/signing"
	"github.com/guided-traffic/quader/internal/signing/signingtest"
	signingstore "github.com/guided-traffic/quader/internal/signing/store"
)

// Unit tests of the operations of Quader's signing identity (R-SIG-055) through the shared step and the response validator, against a
// store in memory: the permissions of each caller, the shapes, the public material, and that no answer carries key material. The
// integration area signing covers the store behind them.

// fakeSigningStore is the store of the tests: the identities it answers and the writes it saw.
type fakeSigningStore struct {
	mu      sync.Mutex
	ids     []signing.Identity
	refused *signingstore.Refusal
	backlog []signingstore.Backlog
	managed []bool
	calls   []string
	err     error
}

func (f *fakeSigningStore) record(call string, managed bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call)
	f.managed = append(f.managed, managed)
	return f.err
}

func (f *fakeSigningStore) Overview(context.Context) (signingstore.Overview, error) {
	return signingstore.Overview{Identities: f.ids, Refused: f.refused, Backlog: f.backlog}, nil
}

func (f *fakeSigningStore) Identities(context.Context) ([]signing.Identity, error) { return f.ids, nil }

func (f *fakeSigningStore) CreateKey(_ context.Context, _ audit.Actor, req signing.KeyRequest, managed bool) (signing.Identity, error) {
	if err := f.record("key:"+req.Subject.CN, managed); err != nil {
		return signing.Identity{}, err
	}
	return signing.Identity{ID: 9, Source: signing.SourceUpload, State: signing.StatePending, NPMKeyID: "SHA256:x", CSR: "-----BEGIN CERTIFICATE REQUEST-----", CreatedAt: sigT0()}, nil
}

func (f *fakeSigningStore) UploadCertificate(_ context.Context, _ audit.Actor, chain, _ []byte, managed bool) (signing.Identity, error) {
	if err := f.record("certificate:"+string(chain), managed); err != nil {
		return signing.Identity{}, err
	}
	return f.ids[0], nil
}

func (f *fakeSigningStore) Import(_ context.Context, _ audit.Actor, key, pass, _, _ []byte, managed bool) (signing.Identity, error) {
	if err := f.record("import:"+string(key)+":"+string(pass), managed); err != nil {
		return signing.Identity{}, err
	}
	return f.ids[0], nil
}

func (f *fakeSigningStore) DiscardPending(context.Context, audit.Actor) error {
	return f.record("discard", false)
}

func (f *fakeSigningStore) Remove(_ context.Context, _ audit.Actor, managed bool) error {
	return f.record("remove", managed)
}

func (f *fakeSigningStore) Retire(_ context.Context, _ audit.Actor, id int64) (signing.Identity, error) {
	if err := f.record("retire", false); err != nil {
		return signing.Identity{}, err
	}
	out := f.ids[len(f.ids)-1]
	out.ID, out.State, out.RetiredAt = id, signing.StateRetired, sigT0()
	return out, nil
}

// withIdentities is a current identity of the files, an announced and a pending upload, and a previous one, each of the test CA.
func withIdentities(t *testing.T) *fakeSigningStore {
	t.Helper()
	ca := signingtest.NewCA(t, "Example")
	ident := func(id int64, source, state string) signing.Identity {
		leaf, _ := ca.Issue(t, signingtest.Leaf{})
		out := signing.Identity{
			ID: id, Source: source, State: state, NPMKeyID: "SHA256:k" + state, Subject: leaf.Subject.String(), Issuer: leaf.Issuer.String(),
			Names: []string{"https://quader.example.com"}, NotBefore: leaf.NotBefore, NotAfter: leaf.NotAfter, CreatedAt: sigT0(),
			Certificate: leaf.Raw, CertSHA256: [32]byte{byte(id)}, Chain: [][]byte{ca.Intermediate.Raw, ca.Root.Raw},
		}
		switch state {
		case signing.StateAnnounced:
			out.AnnouncedAt = time.Now()
		case signing.StatePrevious:
			out.ExpiresAt = sigT0()
		}
		return out
	}
	pending := signing.Identity{ID: 4, Source: signing.SourceUpload, State: signing.StatePending, NPMKeyID: "SHA256:p", Subject: "CN=Quader signing", CSR: "-----BEGIN CERTIFICATE REQUEST-----\n", CreatedAt: sigT0(), CreatedBy: "user:alice"}
	return &fakeSigningStore{
		ids: []signing.Identity{
			ident(1, signing.SourceUpload, signing.StatePrevious), ident(2, signing.SourceConfiguration, signing.StateCurrent),
			ident(3, signing.SourceUpload, signing.StateAnnounced), pending,
		},
		refused: &signingstore.Refusal{Reason: "the key is RSA (key_type)", At: sigT0()},
		backlog: []signingstore.Backlog{{Kind: "npm", Priority: 0, Rows: 2, OldestDue: sigT0()}, {Kind: "attestation", Priority: 3, Failed: true, Rows: 1}},
	}
}

func signingSetup(env sigEnv, t *testing.T, st *fakeSigningStore, c sigCaller, managed bool) setup {
	t.Helper()
	return env.setup(t, newFakeSigStore(), c, func(o *Options) {
		o.Signing = SigningServices{Store: st, Snapshot: st, Enabled: true, Managed: managed, AnnouncePeriod: 10 * time.Minute}
	})
}

const signingImport = `{"key":"-----BEGIN PRIVATE KEY-----\nMIGHAgEA\n-----END PRIVATE KEY-----\n","passphrase":"s3cret","chain":"x"}`

// TestR_SIG_055_TheSigningOperationsAnswerEachCallerAsTheTableSays: an administrator and a security-admin read and write, an auditor
// reads and is refused every write, an Owner and a member of a project are refused, an anonymous caller is asked to authenticate, and
// the trusted root and the chain answer everyone.
func TestR_SIG_055_TheSigningOperationsAnswerEachCallerAsTheTableSays(t *testing.T) {
	env := newSigEnv(t)
	type op struct{ name, method, path, body string }
	ops := []op{
		{"read the identity", "GET", "/api/v1/signing/identity", ""},
		{"create a key", "POST", "/api/v1/signing/identity/key", `{"subject":{"CN":"Quader signing"}}`},
		{"upload a certificate", "PUT", "/api/v1/signing/identity/certificate", `{"chain":"x"}`},
		{"import", "POST", "/api/v1/signing/identity/import", signingImport},
		{"discard the pending key", "DELETE", "/api/v1/signing/identity/pending", ""},
		{"remove the identity", "DELETE", "/api/v1/signing/identity", ""},
		{"retire", "POST", "/api/v1/signing/identities/1/retire", ""},
		{"the trusted root", "GET", "/api/v1/signing/trusted-root.json", ""},
		{"the chain", "GET", "/api/v1/signing/chain.pem", ""},
	}
	want := map[string][]int{
		"admin":          {200, 201, 200, 201, 204, 204, 200, 200, 200},
		"security-admin": {200, 201, 200, 201, 204, 204, 200, 200, 200},
		"auditor":        {200, 403, 403, 403, 403, 403, 403, 200, 200},
		"owner":          {403, 403, 403, 403, 403, 403, 403, 200, 200},
		"member":         {403, 403, 403, 403, 403, 403, 403, 200, 200},
		"anonymous":      {401, 401, 401, 401, 401, 401, 401, 200, 200},
	}
	for name, c := range sigCallers() {
		for i, o := range ops {
			t.Run("R-SIG-055 "+name+" may "+o.name+": "+http.StatusText(want[name][i]), func(t *testing.T) {
				w := signingSetup(env, t, withIdentities(t), c, false).do(o.method, o.path, o.body, nil)
				if w.Code != want[name][i] {
					t.Fatalf("%d %s", w.Code, w.Body.String())
				}
				if strings.Contains(w.Body.String(), "PRIVATE KEY") || strings.Contains(w.Body.String(), "s3cret") {
					t.Fatalf("the answer carries key material: %s", w.Body.String())
				}
			})
		}
	}
}

// TestR_SIG_055_TheIdentityIsAnsweredWithItsStatesAndThePublicMaterial: the answer's state is the current identity's source, the
// announced one carries its switch time, the pending one its request, previous identities follow newest first, the refusal and the
// backlog per kind are there; the Helm values' flag reaches every write; the trusted root reads back with sigstore-go and the chain holds
// three certificates; without an identity both answer 404.
func TestR_SIG_055_TheIdentityIsAnsweredWithItsStatesAndThePublicMaterial(t *testing.T) {
	env := newSigEnv(t)
	admin := sigCallers()["admin"]
	st := withIdentities(t)
	s := signingSetup(env, t, st, admin, true)
	var got struct {
		State     string `json:"state"`
		Managed   bool   `json:"managed"`
		Current   *struct{ Source, Fingerprint string }
		Announced *struct {
			SwitchAt *time.Time `json:"switchAt"`
		}
		Pending *struct {
			Csr *string `json:"csr"`
		}
		Previous []struct{ ID int64 }
		Refused  *struct{ Reason string }
		Backlog  []struct {
			Kind        string
			Due, Failed int64
		}
	}
	sigBody(t, s.get("/api/v1/signing/identity"), &got)
	switch {
	case got.State != "configuration" || !got.Managed || got.Current == nil || got.Current.Fingerprint == "":
		t.Errorf("the current identity: %+v", got)
	case got.Announced == nil || got.Announced.SwitchAt == nil || time.Until(*got.Announced.SwitchAt) < 9*time.Minute:
		t.Errorf("the announced identity: %+v", got.Announced)
	case got.Pending == nil || got.Pending.Csr == nil || !strings.Contains(*got.Pending.Csr, "CERTIFICATE REQUEST"):
		t.Errorf("the pending key: %+v", got.Pending)
	case len(got.Previous) != 1 || got.Refused == nil || len(got.Backlog) != 2 || got.Backlog[0].Due != 2 || got.Backlog[1].Failed != 1:
		t.Errorf("previous %v, refused %v, backlog %+v", got.Previous, got.Refused, got.Backlog)
	}
	for _, o := range [][3]string{
		{"POST", "/api/v1/signing/identity/key", `{}`},
		{"PUT", "/api/v1/signing/identity/certificate", `{"chain":"x"}`},
		{"POST", "/api/v1/signing/identity/import", signingImport},
		{"DELETE", "/api/v1/signing/identity", ""},
	} {
		if w := s.do(o[0], o[1], o[2], nil); w.Code >= 300 {
			t.Fatalf("%s %s: %d %s", o[0], o[1], w.Code, w.Body.String())
		}
	}
	for i, m := range st.managed[:4] {
		if !m {
			t.Errorf("write %d (%s) did not carry the Helm values' flag", i, st.calls[i])
		}
	}
	if !strings.Contains(st.calls[2], "s3cret") {
		t.Error("the passphrase did not reach the store")
	}

	w := s.get("/api/v1/signing/trusted-root.json")
	if w.Code != 200 || w.Header().Get("Content-Type") != signing.TrustedRootMediaType {
		t.Fatalf("%d %s", w.Code, w.Header().Get("Content-Type"))
	}
	tr, err := root.NewTrustedRootFromJSON(w.Body.Bytes())
	if err != nil || len(tr.FulcioCertificateAuthorities()) != 1 {
		t.Fatalf("the trusted root: %v", err)
	}
	if w := s.get("/api/v1/signing/chain.pem"); w.Code != 200 || strings.Count(w.Body.String(), "BEGIN CERTIFICATE") != 3 {
		t.Fatalf("the chain: %d %s", w.Code, w.Body.String())
	}

	empty := signingSetup(env, t, &fakeSigningStore{}, admin, false)
	for _, p := range []string{"/api/v1/signing/trusted-root.json", "/api/v1/signing/chain.pem"} {
		problem(t, empty.get(p), 404, errs.CodeNotFound)
	}
	var none struct {
		State    string `json:"state"`
		Previous []any  `json:"previous"`
	}
	sigBody(t, empty.get("/api/v1/signing/identity"), &none)
	if none.State != "none" || none.Previous == nil {
		t.Errorf("without identities: %+v", none)
	}
	conflict := withIdentities(t)
	conflict.err = errs.E(errs.Conflict, signingstore.CodeManaged, "the Helm values name it")
	problem(t, signingSetup(env, t, conflict, admin, false).do("POST", "/api/v1/signing/identity/key", `{}`, nil), 409, signingstore.CodeManaged)
	without := env.setup(t, newFakeSigStore(), admin, func(o *Options) { o.Signing = SigningServices{} })
	if w := without.get("/api/v1/signing/identity"); w.Code != 503 {
		t.Errorf("without the store: %d", w.Code)
	}
	b, _ := json.Marshal(got)
	if strings.Contains(string(b), "PRIVATE") {
		t.Error("key material")
	}
}
