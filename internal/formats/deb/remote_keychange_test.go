package deb

import (
	"context"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/deb/rows"
	"github.com/guided-traffic/quader/internal/openpgp"
)

// keysStore is the part of the data port a release fill touches: the keys its transaction reads and the records it writes. The other
// methods of Store are not called by OnFill for a release and stay nil.
type keysStore struct {
	Store
	keys     string
	recorded []rows.ReleaseRecord
}

func (s *keysStore) UpstreamKeys(context.Context, formats.Tx, int64) (string, error) {
	return s.keys, nil
}

func (s *keysStore) RecordRelease(_ context.Context, _ formats.Tx, in rows.ReleaseRecord) (rows.ReleaseDecision, error) {
	s.recorded = append(s.recorded, in)
	return rows.ReleaseDecision{Stored: true}, nil
}

// testKey is a signing key of the tests with its armored public certificate and fingerprint.
type testKey struct {
	key         *openpgp.Key
	public, fpr string
}

func newTestKey(t *testing.T) testKey {
	t.Helper()
	now := time.Now()
	secret, public, _, err := openpgp.Generate("test", now.Add(-2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	key, err := openpgp.ParseSecret(secret, nil, now, openpgp.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	certs, err := openpgp.ParseStoredCertificates(public, openpgp.Limits{})
	if err != nil || len(certs) != 1 {
		t.Fatalf("the public certificate: %v", err)
	}
	return testKey{key: key, public: string(public), fpr: certs[0].Fingerprint}
}

// R-DEB-020, Q-DEB-133: a release whose fetch began under one set of upstream keys is recorded under the set its fill's transaction reads.
// A key change that committed while the upstream answered is not undone by the fill: a release that only a removed key signed is
// refused and nothing is recorded, a release a key of the new set also signs is recorded with that key, and a remote whose keys were
// all removed records it unverified.
func TestR_DEB_020_AFillIsRecordedUnderTheKeysItsTransactionReads(t *testing.T) {
	a, b := newTestKey(t), newTestKey(t)
	now := time.Now()
	doc := releaseText(now.Add(-time.Minute), "stable")
	signed := func(keys ...*openpgp.Key) []byte {
		out, err := openpgp.ClearSign(doc, keys, now.Add(-time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	byA, byAB := signed(a.key), signed(a.key, b.key)
	fill := func(prepared []byte, keysAtFetch, keysAtCommit string) (*keysStore, error) {
		t.Helper()
		p, err := prepare(t, keysAtFetch, "dists/stable/InRelease", prepared)
		if err != nil {
			t.Fatalf("prepare: %v", err)
		}
		st := &keysStore{keys: keysAtCommit}
		_, err = New().Remote().OnFill(context.Background(), nil, formats.FillEvent{
			Repo: formats.RepoRef{ID: 7}, Path: formats.Path{Path: "dists/stable/InRelease"}, Prepared: p, FetchedAt: now, Data: st,
		})
		return st, err
	}

	// The keys did not change: the signer of the fetch is recorded.
	st, err := fill(byA, a.public, a.public)
	if err != nil || len(st.recorded) != 1 || st.recorded[0].VerifiedBy != a.fpr {
		t.Fatalf("unchanged keys: %v, %+v", err, st.recorded)
	}
	// a was replaced by b while the upstream answered: the release only a signed is refused as a signature failure, nothing recorded.
	st, err = fill(byA, a.public, b.public)
	if codeOf(err) != errs.CodeDebUpstreamSignatureInvalid || len(st.recorded) != 0 {
		t.Errorf("a key removed during the fetch: %v, %d records", err, len(st.recorded))
	}
	// The release a and b sign is recorded with b, the key that is still in the set.
	st, err = fill(byAB, a.public, b.public)
	if err != nil || len(st.recorded) != 1 || st.recorded[0].VerifiedBy != b.fpr {
		t.Errorf("co-signed release: %v, %+v", err, st.recorded)
	}
	// Every key was removed: the release is recorded without a signer and without keys.
	st, err = fill(byA, a.public, "")
	if err != nil || len(st.recorded) != 1 || st.recorded[0].VerifiedBy != "" || st.recorded[0].KeysSet {
		t.Errorf("keys removed: %v, %+v", err, st.recorded)
	}
	// Keys were set while a remote without keys fetched: the release is verified now, and one the new key does not sign is refused.
	st, err = fill(byA, "", a.public)
	if err != nil || len(st.recorded) != 1 || st.recorded[0].VerifiedBy != a.fpr {
		t.Errorf("keys set during the fetch: %v, %+v", err, st.recorded)
	}
	if _, err = fill(byA, "", b.public); codeOf(err) != errs.CodeDebUpstreamSignatureInvalid {
		t.Errorf("a foreign key set during the fetch: %v", err)
	}
}
