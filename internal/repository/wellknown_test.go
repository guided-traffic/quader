package repository

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/guided-traffic/quader/internal/formats/catalog"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
	"github.com/guided-traffic/quader/internal/wellknown"
)

// TestR_REPO_068_ARepositoryOfUpstreamCarriesItsKindAndState: the read model of a repository of the project upstream carries the kind
// and the state derived from its row of upstream_repository and its disabled member (Q-REPO-375); every other repository carries
// none.
func TestR_REPO_068_ARepositoryOfUpstreamCarriesItsKindAndState(t *testing.T) {
	at := pgtype.Timestamptz{Time: time.Unix(1_700_000_000, 0), Valid: true}
	facts := map[int64]q.UpstreamRepository{
		1: {RepositoryID: 1, Kind: "preset"},
		2: {RepositoryID: 2, Kind: "preset", FirstSwitchedOnAt: at},
		3: {RepositoryID: 3, Kind: "preset", FirstSwitchedOnAt: at},
		4: {RepositoryID: 4, Kind: "preset", RetiredAt: at},
		5: {RepositoryID: 5, Kind: "system"},
	}
	for _, tc := range []struct {
		id        int64
		config    string
		project   string
		wantKind  wellknown.Kind
		wantState wellknown.State
	}{
		{1, `{"disabled": true}`, "upstream", wellknown.KindPreset, wellknown.StateNotSwitchedOn},
		{2, `{"disabled": true}`, "upstream", wellknown.KindPreset, wellknown.StateSwitchedOff},
		{3, `{}`, "upstream", wellknown.KindPreset, wellknown.StateActive},
		{4, `{"disabled": true}`, "upstream", wellknown.KindPreset, wellknown.StateRetired},
		{5, `{}`, "upstream", wellknown.KindSystem, ""},
		{6, `{}`, "web", "", ""},
		{1, `not json`, "upstream", wellknown.KindPreset, wellknown.StateNotSwitchedOn}, // fails closed: a configuration that does not load is disabled
	} {
		r, _ := toRepository(q.Repository{ID: tc.id, Key: "k", Class: "remote", Format: "npm", Config: []byte(tc.config), Lifecycle: "active"}, tc.project, catalog.NewRegistry())
		withUpstream(&r, facts)
		switch {
		case tc.wantKind == "" && r.Upstream != nil:
			t.Errorf("repository %d of %s carries %+v", tc.id, tc.project, r.Upstream)
		case tc.wantKind == "":
		case r.Upstream == nil || r.Upstream.Kind != tc.wantKind || r.Upstream.State != tc.wantState:
			t.Errorf("repository %d %s = %+v, want %s %q", tc.id, tc.config, r.Upstream, tc.wantKind, tc.wantState)
		}
	}
	var none *UpstreamFacts
	if none.Retired() || none.System() {
		t.Error("a repository of another project reads as retired or as a system repository")
	}
	if !(&UpstreamFacts{Kind: wellknown.KindPreset, State: wellknown.StateRetired}).Retired() || !(&UpstreamFacts{Kind: wellknown.KindSystem}).System() {
		t.Error("Retired or System misreads its facts")
	}
}
