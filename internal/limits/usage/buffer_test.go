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
