package usage

import (
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/limits"
)

// TestR_LIM_027_AWellKnownMemberThroughTwoVirtualRepositoriesIsTwoRows: via_repository_id is part of the row's key (Q-LIM-63), so the
// usage of one serving repository reached through two virtual repositories, and directly, stays three rows, each naming its own.
func TestR_LIM_027_AWellKnownMemberThroughTwoVirtualRepositoriesIsTwoRows(t *testing.T) {
	b := NewBuffer(Options{})
	at := time.Unix(1_800_000_000, 0)
	user := limits.Subject{Kind: limits.KindUser, Ref: "u-1", Credential: "session"}
	for _, via := range []int64{5, 9, 0, 5} {
		b.Add(limits.UsageDelta{At: at, Subject: user, Place: limits.InstancePlace(), RepoID: 6, ViaRepoID: via, Requests: 1, Cached: 1024})
	}
	if len(b.rows) != 3 {
		t.Fatalf("%d rows, want 3", len(b.rows))
	}
	got := map[int64]int64{}
	for k, c := range b.rows {
		r := rowOf("5m", k, c)
		if r.Repo != 6 {
			t.Fatalf("row %+v names another serving repository", r)
		}
		got[r.Via] = r.RQ
	}
	if got[5] != 2 || got[9] != 1 || got[0] != 1 {
		t.Fatalf("requests by via = %v", got)
	}
}

// TestR_CLAMAV_010_ACertificateSubjectsRowNamesTheSerial: the usage row of a certificate subject keeps the certificate's serial as its
// credential, so a renewed certificate of the same subject is a second credential of one budget (Q-CLAMAV-12); an anonymous row keeps none.
func TestR_CLAMAV_010_ACertificateSubjectsRowNamesTheSerial(t *testing.T) {
	b := NewBuffer(Options{})
	at := time.Unix(1_800_000_000, 0)
	for _, s := range []limits.Subject{
		{Kind: limits.KindCertificate, Ref: "CN=scan-01.example.com", Credential: "a1b2c"},
		{Kind: limits.KindCertificate, Ref: "CN=scan-01.example.com", Credential: "a1b2d"},
		{Kind: limits.KindAnonymous, Ref: "198.51.100.7", Credential: "x"},
	} {
		b.Add(limits.UsageDelta{At: at, Subject: s, Place: limits.InstancePlace(), RepoID: 6, Requests: 1})
	}
	creds := map[string]bool{}
	for k := range b.rows {
		creds[string(k.kind)+"/"+k.credential] = true
	}
	if len(creds) != 3 || !creds["certificate/a1b2c"] || !creds["certificate/a1b2d"] || !creds["anonymous/"] {
		t.Fatalf("rows %v", creds)
	}
}
