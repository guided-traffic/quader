package gate

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/malware"
	mgate "github.com/guided-traffic/quader/internal/malware/gate"
	"github.com/guided-traffic/quader/internal/signatures"
	"github.com/guided-traffic/quader/internal/vuln"
	"github.com/guided-traffic/quader/internal/vuln/enforce"
)

// malwareStore is the malware gate's store: a verdict the test sets.
type malwareStore struct {
	malware.Store
	mu sync.Mutex
	v  malware.Verdict
}

func (m *malwareStore) Verdict(context.Context, blob.Digest) (malware.Verdict, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.v, nil
}

func (m *malwareStore) Ask(context.Context, blob.Digest, int64, int64) error { return nil }

func (m *malwareStore) set(v malware.Verdict) {
	m.mu.Lock()
	m.v = v
	m.mu.Unlock()
}

// serveOf is one serve point's three checks over fakes: the malware gate strict, the vulnerability block as decided, the signature gate
// on files-internal.
func serveOf(ms *malwareStore, mv malware.Verdict, block vuln.Block, st *fakeStore) Serve {
	mg := mgate.New(mgate.Options{Store: ms, Enabled: true, Wait: 5 * time.Second, Poll: 10 * time.Millisecond})
	sg := newGate(st, &recorder{}, Options{Wait: 5 * time.Second, Poll: 10 * time.Millisecond})
	return Serve{
		Malware: mg, MalwareOf: mgate.Download{Addressed: filesInternal.Name, HolderID: 7, Path: "tools/tool.tar.gz", Verdict: mv, Strict: true},
		Enforce: enforce.New(enforce.Options{}), VulnOf: enforce.Download{Addressed: "web/files-internal", Path: "tools/tool.tar.gz", Block: block},
		Signatures: sg, SignaturesOf: dl(filesInternal),
	}
}

func codeOf(t *testing.T, err error) string {
	t.Helper()
	if err == nil {
		return ""
	}
	return problem(t, err).Code
}

// TestR_SIG_069_OneDecisionOneWait: an infected and unsigned item answers malware_found; a blocked version beside an unsigned item
// answers vulnerability_blocked; a pending malware scan and an unsigned item answers signature_not_verified at once, without a wait; an
// unscannable item with a passing signature answers malware_not_checked; a pending scan and a pending signature wait together and are
// served when both decide; one still pending at the bound answers the malware gate's 503 first. Unit, milliseconds, fakes of the three
// checks.
func TestR_SIG_069_OneDecisionOneWait(t *testing.T) {
	clean := malware.Verdict{Known: true, State: malware.StateClean}
	infected := malware.Verdict{Known: true, State: malware.StateInfected, Signature: "Eicar-Test-Signature"}
	pending := malware.Verdict{}
	unscannable := malware.Verdict{Known: true, State: malware.StateUnscannable, Reason: "encrypted"}
	blocked := vuln.Block{Reason: vuln.BlockTriage, Enforced: true}
	passing := valid(fileBlob, TrustInternal, 3)

	t.Run("refusals in their order, without a wait", func(t *testing.T) {
		for _, tc := range []struct {
			name  string
			mv    malware.Verdict
			block vuln.Block
			rows  []Row
			want  string
		}{
			{"infected and unsigned", infected, vuln.Block{}, nil, errs.CodeMalwareFound},
			{"blocked and unsigned", clean, blocked, nil, errs.CodeVulnerabilityBlocked},
			{"pending scan and unsigned", pending, vuln.Block{}, nil, errs.CodeSignatureNotVerified},
			{"unscannable and signed", unscannable, vuln.Block{}, []Row{passing}, errs.CodeMalwareNotChecked},
			{"unscannable and unsigned", unscannable, vuln.Block{}, nil, errs.CodeSignatureNotVerified},
			{"missing data and signed", clean, vuln.Block{Reason: vuln.BlockMissingData, Enforced: true}, []Row{passing}, errs.CodeVulnerabilityDataMissing},
			{"clean and signed", clean, vuln.Block{}, []Row{passing}, ""},
		} {
			st := &fakeStore{}
			st.set(tc.rows...)
			ms := &malwareStore{v: tc.mv}
			started := time.Now()
			got := codeOf(t, serveOf(ms, tc.mv, tc.block, st).Run(context.Background()))
			if got != tc.want || time.Since(started) > time.Second {
				t.Errorf("%s: %q after %s, want %q at once", tc.name, got, time.Since(started), tc.want)
			}
		}
	})
	t.Run("a pending scan and a pending signature wait together", func(t *testing.T) {
		st := &fakeStore{}
		st.set(Row{Subject: fileBlob, State: signatures.StatePending})
		ms := &malwareStore{}
		go func() {
			time.Sleep(50 * time.Millisecond)
			ms.set(clean)
			time.Sleep(50 * time.Millisecond)
			st.set(passing)
		}()
		started := time.Now()
		if err := serveOf(ms, pending, vuln.Block{}, st).Run(context.Background()); err != nil {
			t.Fatal(err)
		}
		if took := time.Since(started); took < 90*time.Millisecond || took > 3*time.Second {
			t.Errorf("served after %s", took)
		}
	})
	t.Run("still pending at the bound: the malware gate's 503 first", func(t *testing.T) {
		st := &fakeStore{}
		st.set(Row{Subject: fileBlob, State: signatures.StatePending})
		ms := &malwareStore{}
		s := serveOf(ms, pending, vuln.Block{}, st)
		arrived := time.Now().Add(-5*time.Second + 100*time.Millisecond)
		s.MalwareOf.Arrived, s.SignaturesOf.Arrived = arrived, arrived
		if got := codeOf(t, s.Run(context.Background())); got != errs.CodeMalwareCheckPending {
			t.Errorf("%q", got)
		}
		ms.set(clean)
		s = serveOf(ms, clean, vuln.Block{}, st)
		s.SignaturesOf.Arrived = arrived
		if got := codeOf(t, s.Run(context.Background())); got != errs.CodeSignatureCheckPending {
			t.Errorf("only the signature pending: %q", got)
		}
	})
	t.Run("a refusal that arrives during the wait ends it", func(t *testing.T) {
		st := &fakeStore{}
		st.set(Row{Subject: fileBlob, State: signatures.StatePending})
		ms := &malwareStore{}
		go func() {
			time.Sleep(40 * time.Millisecond)
			ms.set(infected)
		}()
		if got := codeOf(t, serveOf(ms, pending, vuln.Block{}, st).Run(context.Background())); got != errs.CodeMalwareFound {
			t.Errorf("%q", got)
		}
	})
}

// TestR_OCI_040_AScreenIsHeldByTheDecisionOfTheDownload: Hold answers the code Run answers for every refusal in its order, from the same
// decision, and the code of the check a download would wait for, at once; it records no event and waits for nothing, so that a screen
// that shows what a stored file holds (an image's configuration, formats/oci.md R-OCI-040) shows a caller nothing a download of the file
// would not give the same caller. Unit, milliseconds, fakes of the three checks.
func TestR_OCI_040_AScreenIsHeldByTheDecisionOfTheDownload(t *testing.T) {
	clean := malware.Verdict{Known: true, State: malware.StateClean}
	infected := malware.Verdict{Known: true, State: malware.StateInfected, Signature: "Eicar-Test-Signature"}
	unscannable := malware.Verdict{Known: true, State: malware.StateUnscannable, Reason: "encrypted"}
	passing := valid(fileBlob, TrustInternal, 3)
	for _, tc := range []struct {
		name   string
		mv     malware.Verdict
		block  vuln.Block
		rows   []Row
		rules  error
		want   string
		served bool
	}{
		{"infected", infected, vuln.Block{}, []Row{passing}, nil, errs.CodeMalwareFound, false},
		{"blocked", clean, vuln.Block{Reason: vuln.BlockTriage, Enforced: true}, []Row{passing}, nil, errs.CodeVulnerabilityBlocked, false},
		{
			"not delivered", clean,
			vuln.Block{},
			[]Row{passing},
			errs.E(errs.Forbidden, errs.CodeNotAllowedInRepository, "not delivered"), errs.CodeNotAllowedInRepository, false,
		},
		{"unsigned", clean, vuln.Block{}, nil, nil, errs.CodeSignatureNotVerified, false},
		{"unscannable", unscannable, vuln.Block{}, []Row{passing}, nil, errs.CodeMalwareNotChecked, false},
		{"missing data", clean, vuln.Block{Reason: vuln.BlockMissingData, Enforced: true}, []Row{passing}, nil, errs.CodeVulnerabilityDataMissing, false},
		{"clean and signed", clean, vuln.Block{}, []Row{passing}, nil, "", true},
		{"a scan pending", malware.Verdict{}, vuln.Block{}, []Row{passing}, nil, errs.CodeMalwareCheckPending, true},
		{"a signature pending", clean, vuln.Block{}, []Row{{Subject: fileBlob, State: signatures.StatePending}}, nil, errs.CodeSignatureCheckPending, true},
	} {
		st := &fakeStore{}
		st.set(tc.rows...)
		rec := &recorder{}
		s := serveOf(&malwareStore{v: tc.mv}, tc.mv, tc.block, st)
		s.Signatures = newGate(st, rec, Options{Wait: 5 * time.Second, Poll: 10 * time.Millisecond})
		s.Rules = tc.rules
		started := time.Now()
		got, err := s.Hold(context.Background())
		if err != nil || got != tc.want || time.Since(started) > time.Second {
			t.Errorf("%s: Hold %q, %v after %s, want %q at once", tc.name, got, err, time.Since(started), tc.want)
		}
		if rec.count() != 0 {
			t.Errorf("%s: Hold recorded %d events", tc.name, rec.count())
		}
		if !tc.served {
			if run := codeOf(t, s.Run(context.Background())); run != tc.want {
				t.Errorf("%s: Run answers %q, Hold %q", tc.name, run, got)
			}
		}
	}
}
