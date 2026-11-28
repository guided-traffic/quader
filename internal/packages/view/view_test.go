package view

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/guided-traffic/quader/internal/formats/versionkey"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
	"github.com/guided-traffic/quader/internal/qname"
	"github.com/guided-traffic/quader/internal/repository"
)

func TestR_API_029_PackageIDRoundTrips(t *testing.T) {
	for _, c := range []struct{ format, name string }{
		{"npm", "left-pad"},
		{"npm", "@team-a/ui"},
		{"nuget", "Contoso.Logging"},
		{"maven", "org.acme:lib"},
		{"npm", strings.Repeat("a", 214)},
		{"npm", "ünï/cödé"},
	} {
		id := ID(c.format, c.name)
		if !strings.HasPrefix(id, "pkg_") || strings.ToLower(id) != id {
			t.Errorf("%s: %q", c.name, id)
		}
		for _, r := range id[4:] {
			if (r < 'a' || r > 'z') && (r < '2' || r > '7') {
				t.Errorf("%s: %q holds %q, outside [0-9A-Za-z]", c.name, id, r)
			}
		}
		f, n, ok := ParseID(id)
		if !ok || f != c.format || n != c.name {
			t.Errorf("%q parses to %q %q %v", id, f, n, ok)
		}
	}
	if ID("npm", "a") == ID("nuget", "a") || ID("npm", "ab") == ID("npm", "a") {
		t.Error("two packages share an id")
	}
}

func TestR_API_029_ParseIDRefusesWhatIsNoID(t *testing.T) {
	for _, bad := range []string{"", "pkg_", "pkg_!!!", "pkg_a", "npm", "lookup", ID("npm", "x")[1:], "pkg_" + "mfzwk", "pkg_01JBQ6V7S3K9"} {
		if f, n, ok := ParseID(bad); ok {
			t.Errorf("%q parses to %q %q", bad, f, n)
		}
	}
}

// testIDs are the database IDs the unit tests give their repositories.
var testIDs = map[string]string{
	"npm-a": "1", "npm-b": "2", "npm-remote": "3", "npm-inner": "4", "npm-outer": "5", "npm-empty": "6", "npm-all": "7", "npm-secret": "8",
}

func repoOf(key string, class repository.Class) repository.Repository {
	return repository.Repository{ID: testIDs[key], Name: qr(key), Class: class, Format: "npm"}
}

// idOf is the database ID of a test repository.
func idOf(topo Topology, key string) int64 { return topo.ID(qr(key)) }

func idsOf(topo Topology, keys ...string) []int64 {
	var out []int64
	for _, k := range keys {
		out = append(out, idOf(topo, k))
	}
	return out
}

func TestR_API_029_TopologyExpandsVirtualRepositories(t *testing.T) {
	a, b, c := repoOf("npm-a", repository.Local), repoOf("npm-b", repository.Local), repoOf("npm-remote", repository.Remote)
	inner := repository.Assemble(repoOf("npm-inner", repository.Virtual), []repository.Repository{a, c}, nil, 0)
	outer := repository.Assemble(repoOf("npm-outer", repository.Virtual), []repository.Repository{inner, b}, nil, 0)
	empty := repository.Assemble(repoOf("npm-empty", repository.Virtual), nil, nil, 0)
	topo := NewTopology([]repository.Repository{a, b, c, inner, outer, empty})

	if got, want := topo.Physical(), idsOf(topo, "npm-a", "npm-b", "npm-remote"); !slices.Equal(got, want) {
		t.Errorf("Physical %v", got)
	}
	if got, want := topo.Virtual(), idsOf(topo, "npm-empty", "npm-inner", "npm-outer"); !slices.Equal(got, want) {
		t.Errorf("Virtual %v", got)
	}
	for _, tc := range []struct {
		in   []string
		want []string
	}{
		{[]string{"npm-a"}, []string{"npm-a"}},
		{[]string{"npm-inner"}, []string{"npm-a", "npm-remote"}},
		{[]string{"npm-outer"}, []string{"npm-a", "npm-b", "npm-remote"}},
		{[]string{"npm-outer", "npm-a", "npm-inner"}, []string{"npm-a", "npm-b", "npm-remote"}},
		{[]string{"npm-empty"}, nil},
		{[]string{"ghost"}, nil},
		{nil, nil},
	} {
		want := idsOf(topo, tc.want...)
		slices.Sort(want)
		if got := topo.Expand(topo.IDs(namesOf(tc.in...))); !slices.Equal(got, want) {
			t.Errorf("Expand(%v) = %v, want %v", tc.in, got, want)
		}
	}
	if got := topo.Containers(idOf(topo, "npm-a")); !slices.Equal(got, idsOf(topo, "npm-inner", "npm-outer")) {
		t.Errorf("Containers(npm-a) = %v", got)
	}
	if got := topo.Containers(idOf(topo, "npm-b")); !slices.Equal(got, idsOf(topo, "npm-outer")) {
		t.Errorf("Containers(npm-b) = %v", got)
	}
	if c, ok := topo.Class(idOf(topo, "npm-remote")); !ok || c != repository.Remote {
		t.Errorf("Class %v %v", c, ok)
	}
	if topo.Name(idOf(topo, "npm-b")) != qr("npm-b") || topo.Name(999) != (qname.Repo{}) {
		t.Error("Name must give the name of a known ID and the zero name for an unknown one")
	}
}

func namesOf(keys ...string) []qname.Repo {
	var out []qname.Repo
	for _, k := range keys {
		out = append(out, qr(k))
	}
	return out
}

// TestR_API_029_HoldersShowOnlyReadableVirtualRepositories: a version shows in the
// virtual repositories that contain its repository and that the caller may read, local
// repositories first.
func TestR_API_029_HoldersShowOnlyReadableVirtualRepositories(t *testing.T) {
	a, c := repoOf("npm-a", repository.Local), repoOf("npm-remote", repository.Remote)
	inner := repository.Assemble(repoOf("npm-all", repository.Virtual), []repository.Repository{a, c}, nil, 0)
	hidden := repository.Assemble(repoOf("npm-secret", repository.Virtual), []repository.Repository{a}, nil, 0)
	topo := NewTopology([]repository.Repository{a, c, inner, hidden})
	sc := Scope{topo: topo, virtualReadable: map[int64]bool{idOf(topo, "npm-all"): true}}
	if got := sc.holders(idsOf(topo, "npm-remote", "npm-a")); !slices.Equal(got, idsOf(topo, "npm-a", "npm-remote", "npm-all")) {
		t.Fatalf("%v", got)
	}
	if got := sc.holders(idsOf(topo, "npm-a")); slices.Contains(got, idOf(topo, "npm-secret")) {
		t.Fatalf("a virtual repository the caller may not read is shown: %v", got)
	}
}

// fakeAdapter orders versions as dotted integers and calls a "-pre" suffix a pre-release; a string of other characters (latest) is no
// version: it sorts before every version and by its bytes, its key of the class versionkey.NotVersion.
type fakeAdapter struct{}

func (fakeAdapter) Compare(a, b string) int {
	switch va, vb := isFakeVersion(a), isFakeVersion(b); {
	case !va && !vb:
		return strings.Compare(a, b)
	case !va:
		return -1
	case !vb:
		return 1
	}
	pa, pb := strings.TrimSuffix(a, "-pre"), strings.TrimSuffix(b, "-pre")
	return strings.Compare(pad(pa), pad(pb))
}

func isFakeVersion(v string) bool {
	v = strings.TrimSuffix(v, "-pre")
	return v != "" && strings.Trim(v, "0123456789.") == ""
}

func pad(v string) string {
	parts := strings.Split(v, ".")
	for i, p := range parts {
		parts[i] = strings.Repeat("0", 4-len(p)) + p
	}
	return strings.Join(parts, ".")
}
func (fakeAdapter) Prerelease(v string) bool { return strings.HasSuffix(v, "-pre") }
func (fakeAdapter) SortKey(v string) []byte {
	if !isFakeVersion(v) {
		return versionkey.Of(nil, false, v)
	}
	return versionkey.Of([]byte(pad(strings.TrimSuffix(v, "-pre"))), true, v)
}

func (fakeAdapter) Describe(context.Context, []Version, Fields, func(string) bool) (map[int64]Data, error) {
	return nil, nil
}
func (fakeAdapter) Find(context.Context, Find, int) ([]int64, error) { return nil, nil }
func (fakeAdapter) Withheld(context.Context, []string) ([]int64, error) {
	return nil, nil
}
func (fakeAdapter) Deprecated(context.Context, []string, int) ([]int64, error) { return nil, nil }

func (fakeAdapter) Fetch() FetchRule { return FetchLeadFile }

// digestAdapter is a format whose version metadata has a `digest` member (an OCI tag), for all versions but the one named none.
type digestAdapter struct {
	fakeAdapter
	asked *[]Fields
}

func (d digestAdapter) Describe(_ context.Context, vs []Version, want Fields, _ func(string) bool) (map[int64]Data, error) {
	*d.asked = append(*d.asked, want)
	out := map[int64]Data{}
	for _, v := range vs {
		if v.Version != "none" {
			out[v.ID] = Data{Metadata: map[string]any{"digest": "sha256:" + v.Version, "state": "in_use"}}
		}
		if v.Version == "old" {
			out[v.ID] = Data{Deprecation: &Deprecation{Message: "use 2"}, Unlisted: true}
		}
	}
	return out, nil
}

// TestR_API_085_TheVersionListMarksDeprecatedAndUnlistedVersionsInOneSummaryRead: every page of the version list carries each
// version's deprecation and unlisted mark from the format's summary, read once for the page and without the details nobody asked for.
func TestR_API_085_TheVersionListMarksDeprecatedAndUnlistedVersionsInOneSummaryRead(t *testing.T) {
	var asked []Fields
	a := digestAdapter{asked: &asked}
	repo := repoOf("nuget-a", repository.Local)
	s := &Service{}
	sc := Scope{topo: NewTopology([]repository.Repository{repo})}
	rows := rowsOf("old", "new")
	groupOf := map[string]versionGroup{
		"old": {version: "old", rows: rows[:1]},
		"new": {version: "new", rows: rows[1:2]},
	}
	page := []VersionSummary{{Version: "old"}, {Version: "new"}}
	if err := s.fillFromMetadata(context.Background(), sc, a, "nuget", groupOf, page, VersionFilter{}); err != nil {
		t.Fatal(err)
	}
	if d := page[0].Deprecation; d == nil || d.Message != "use 2" || !page[0].Unlisted || page[1].Deprecation != nil || page[1].Unlisted {
		t.Errorf("marks = %+v", page)
	}
	if page[1].Digest != "" {
		t.Errorf("a digest nobody asked for: %+v", page[1])
	}
	if len(asked) != 1 || asked[0] != FieldSummary {
		t.Errorf("the adapter was asked %v, want the summary once", asked)
	}
}

// TestR_UI_052_DigestsComeFromTheFormatsMetadataInOneRead: the list of an OCI tag's versions shows the digest of the format's
// metadata, read once for the page; a format without the member has none, and the core never parses a format's metadata beyond it.
func TestR_UI_052_DigestsComeFromTheFormatsMetadataInOneRead(t *testing.T) {
	var asked []Fields
	a := digestAdapter{asked: &asked}
	repo := repoOf("npm-a", repository.Local)
	s := &Service{}
	sc := Scope{topo: NewTopology([]repository.Repository{repo})}
	rows := rowsOf("1.0.0", "none")
	groupOf := map[string]versionGroup{
		"1.0.0": {version: "1.0.0", rows: rows[:1]},
		"none":  {version: "none", rows: rows[1:2]},
	}
	page := []VersionSummary{{Version: "1.0.0"}, {Version: "none"}}
	if err := s.fillFromMetadata(context.Background(), sc, a, "oci", groupOf, page, VersionFilter{WithDigest: true}); err != nil {
		t.Fatal(err)
	}
	if page[0].Digest != "sha256:1.0.0" || page[1].Digest != "" || page[0].State != "" {
		t.Errorf("digests = %+v", page)
	}
	if len(asked) != 1 || asked[0] != FieldSummary|FieldDetails {
		t.Errorf("the adapter was asked %v, want the summary and the details once", asked)
	}
}

// TestR_UI_194_StatesComeFromTheFormatsMetadataInOneRead: include=state reads the member `state` of the format's metadata (the ClamAV
// definitions' in_use, previous, superseded, files_removed) in the same one read as the digest; a version without it has none.
func TestR_UI_194_StatesComeFromTheFormatsMetadataInOneRead(t *testing.T) {
	var asked []Fields
	a := digestAdapter{asked: &asked}
	repo := repoOf("clamav", repository.Local)
	s := &Service{}
	sc := Scope{topo: NewTopology([]repository.Repository{repo})}
	rows := rowsOf("28190", "none")
	groupOf := map[string]versionGroup{
		"28190": {version: "28190", rows: rows[:1]},
		"none":  {version: "none", rows: rows[1:2]},
	}
	page := []VersionSummary{{Version: "28190"}, {Version: "none"}}
	if err := s.fillFromMetadata(context.Background(), sc, a, "clamav", groupOf, page, VersionFilter{WithState: true}); err != nil {
		t.Fatal(err)
	}
	if page[0].State != "in_use" || page[1].State != "" || page[0].Digest != "" {
		t.Errorf("states = %+v", page)
	}
	if len(asked) != 1 {
		t.Errorf("the adapter was asked %v, want one read", asked)
	}
}

func rowsOf(versions ...string) []q.PackageViewVersionsRow {
	out := make([]q.PackageViewVersionsRow, len(versions))
	for i, v := range versions {
		out[i] = q.PackageViewVersionsRow{VersionID: int64(i + 1), Version: v}
	}
	return out
}

// R-API-065: a version held by several repositories was last fetched when the latest of its lead files was; a version none of whose
// rows was fetched has no time.
func TestR_API_065_LastDownloadIsTheLatestOfTheRows(t *testing.T) {
	early, late := time.Date(2026, 11, 1, 8, 0, 0, 0, time.UTC), time.Date(2026, 11, 17, 16, 40, 2, 0, time.UTC)
	rows := rowsOf("1.0.0", "1.0.0", "1.0.0")
	rows[0].LastDownloadAt = pgtype.Timestamptz{Time: early, Valid: true}
	rows[2].LastDownloadAt = pgtype.Timestamptz{Time: late, Valid: true}
	if got := (versionGroup{version: "1.0.0", rows: rows}).lastDownload(); got == nil || !got.Equal(late) {
		t.Errorf("three rows: %v, want %v", got, late)
	}
	if got := (versionGroup{version: "2.0.0", rows: rowsOf("2.0.0")}).lastDownload(); got != nil {
		t.Errorf("never fetched: %v", got)
	}
}

func TestR_API_029_LatestIsTheHighestStableVersion(t *testing.T) {
	for _, tc := range []struct {
		name     string
		versions []string
		want     string
	}{
		{"the highest of several", []string{"1.2.0", "1.10.0", "1.9.0"}, "1.10.0"},
		{"a pre-release above the stable one does not count", []string{"1.0.0", "2.0.0-pre", "1.5.0"}, "1.5.0"},
		{"only pre-releases: the highest of them", []string{"1.0.0-pre", "3.0.0-pre", "2.0.0-pre"}, "3.0.0-pre"},
		{"a single version", []string{"0.1.0"}, "0.1.0"},
		{"the pre-release comes first in the rows", []string{"9.0.0-pre", "1.0.0"}, "1.0.0"},
	} {
		got, ok := latestOf(fakeAdapter{}, rowsOf(tc.versions...))
		if !ok || got.Version != tc.want {
			t.Errorf("%s: %q %v, want %q", tc.name, got.Version, ok, tc.want)
		}
	}
	if _, ok := latestOf(fakeAdapter{}, nil); ok {
		t.Error("a package without a visible version has a latest version")
	}
}

// R-API-029 (Q-API-431): a version the format does not rank (an image tag such as latest) is the latest only while no ranked version is
// visible, and then the newest of them by its time wins, the name breaking a tie.
func TestR_API_029_LatestOfAPackageWithoutARankedVersionIsItsNewest(t *testing.T) {
	at := func(v string, minutes int) q.PackageViewVersionsRow {
		return q.PackageViewVersionsRow{Version: v, CreatedAt: pgtype.Timestamptz{Time: time.Date(2026, 11, 25, 7, minutes, 0, 0, time.UTC), Valid: true}}
	}
	for _, tc := range []struct {
		name string
		rows []q.PackageViewVersionsRow
		want string
	}{
		{"a ranked version beats a newer unranked one", []q.PackageViewVersionsRow{at("latest", 9), at("1.0", 1), at("main", 8)}, "1.0"},
		{"a pre-release beats an unranked one", []q.PackageViewVersionsRow{at("latest", 9), at("2.0-pre", 1)}, "2.0-pre"},
		{"no ranked version: the newest by time", []q.PackageViewVersionsRow{at("main", 3), at("sha-f00cf6c", 7), at("latest", 5)}, "sha-f00cf6c"},
		{"one time: the higher name", []q.PackageViewVersionsRow{at("latest", 5), at("main", 5)}, "main"},
	} {
		if got, ok := latestOf(fakeAdapter{}, tc.rows); !ok || got.Version != tc.want {
			t.Errorf("%s: %q %v, want %q", tc.name, got.Version, ok, tc.want)
		}
	}
	at5 := time.Date(2026, 11, 25, 7, 5, 0, 0, time.UTC)
	o := VersionOrder{Sort: "version", Desc: true}
	if got := o.VersionKey(VersionSummary{Version: "1.0"}); !slices.Equal(got, []string{"1.0"}) {
		t.Errorf("the cursor of a ranked version: %v", got)
	}
	if got := o.VersionKey(VersionSummary{Version: "latest", PublishedAt: at5, Unranked: true}); !slices.Equal(got, []string{at5.Format(time.RFC3339Nano), "latest"}) {
		t.Errorf("the cursor of an unranked version: %v", got)
	}
}

func TestR_API_029_CursorKeysFollowTheSort(t *testing.T) {
	at := time.Date(2026, 10, 20, 9, 12, 44, 512000000, time.UTC)
	p := Package{Name: "@Team-A/UI", Key: "@team-a/ui", Format: "npm", CreatedAt: at, UpdatedAt: at.Add(time.Hour)}
	for _, tc := range []struct {
		order Order
		want  []string
	}{
		{Order{Sort: "name"}, []string{"@team-a/ui", "npm"}},
		{Order{Sort: "name", Desc: true}, []string{"@team-a/ui", "npm"}},
		{Order{Sort: "createdAt"}, []string{"2026-10-20T09:12:44.512Z", "@team-a/ui", "npm"}},
		{Order{Sort: "updatedAt", Desc: true}, []string{"2026-10-20T10:12:44.512Z", "@team-a/ui", "npm"}},
	} {
		key := tc.order.Key(p)
		if !slices.Equal(key, tc.want) {
			t.Errorf("%v: %v, want %v", tc.order, key, tc.want)
		}
		after, err := tc.order.ParseAfter(key)
		if err != nil || after.Name != "@team-a/ui" || after.Format != "npm" {
			t.Errorf("%v: %+v %v", tc.order, after, err)
		}
		if tc.order.Sort != "name" && !after.Time.Equal(map[string]time.Time{"createdAt": p.CreatedAt, "updatedAt": p.UpdatedAt}[tc.order.Sort]) {
			t.Errorf("%v: time %v", tc.order, after.Time)
		}
	}
	for _, bad := range [][]string{nil, {"a"}, {"not a time", "n", "f"}, {"2026-10-20T09:12:44Z", "n"}} {
		if _, err := (Order{Sort: "createdAt"}).ParseAfter(bad); !IsBadCursor(err) {
			t.Errorf("createdAt accepts %v", bad)
		}
	}
	if _, err := (Order{Sort: "name"}).ParseAfter([]string{"a", "b", "c"}); !IsBadCursor(err) {
		t.Error("name accepts three values")
	}
}

func TestR_API_029_NilSlicesAreSentAsEmptyArrays(t *testing.T) {
	if keys(nil) == nil || ids(nil) == nil {
		t.Fatal("a nil slice reaches the driver as NULL, and NOT (x = ANY(NULL)) drops every row")
	}
}

// TestR_API_029_ProjectNarrowsToThePhysicalRepositoriesOfTheProject: the project filter of the list (Q-UI-129) becomes
// a repository filter; it intersects with the asked repositories, and a virtual repository stands for its members.
func TestR_API_029_ProjectNarrowsToThePhysicalRepositoriesOfTheProject(t *testing.T) {
	a := repository.Repository{ID: "1", Name: qname.Repo{Project: "it", Key: "npm-a"}, Class: repository.Local, Format: "npm"}
	b := repository.Repository{ID: "2", Name: qname.Repo{Project: "other", Key: "npm-b"}, Class: repository.Local, Format: "npm"}
	v := repository.Assemble(repository.Repository{ID: "3", Name: qname.Repo{Project: "it", Key: "npm-all"}, Class: repository.Virtual, Format: "npm"},
		[]repository.Repository{a, b}, nil, 0)
	topo := NewTopology([]repository.Repository{a, b, v})
	for _, tc := range []struct {
		name string
		in   Filter
		want []qname.Repo
	}{
		{"project only", Filter{Project: "it"}, []qname.Repo{a.Name}},
		{"another project", Filter{Project: "other"}, []qname.Repo{b.Name}},
		{"unknown project lists nothing", Filter{Project: "ghost"}, nil},
		{"a virtual repository stands for its members, cut to the project", Filter{Project: "it", HasRepositories: true, Repositories: []qname.Repo{v.Name}}, []qname.Repo{a.Name}},
		{"excluded by the repository filter", Filter{Project: "it", HasRepositories: true, Repositories: []qname.Repo{b.Name}}, nil},
	} {
		got := topo.InProject(tc.in)
		if !got.HasRepositories || got.Project != "" || !slices.Equal(got.Repositories, tc.want) {
			t.Errorf("%s: %+v, want %v", tc.name, got, tc.want)
		}
	}
	if got := topo.InProject(Filter{Q: "x"}); got.HasRepositories || got.Q != "x" {
		t.Errorf("no project must leave the filter alone: %+v", got)
	}
}

// TestR_VULN_028_TopologyKeysRepositoriesByIDNotByParsedNames: a repository is found by its database ID whatever its
// qualified text looks like, so that no row is parsed back into a name (R-VULN-028). Two repositories whose texts differ
// only at the separator, and a repository that moved to another project, keep their own IDs and names.
func TestR_VULN_028_TopologyKeysRepositoriesByIDNotByParsedNames(t *testing.T) {
	dash := repository.Repository{ID: "10", Name: qname.Repo{Project: "a-b", Key: "x1"}, Class: repository.Local, Format: "npm"}
	slash := repository.Repository{ID: "11", Name: qname.Repo{Project: "a", Key: "x1"}, Class: repository.Local, Format: "npm"}
	moved := repository.Repository{ID: "12", Name: qname.Repo{Project: "zeta", Key: "x1"}, Class: repository.Remote, Format: "npm"}
	topo := NewTopology([]repository.Repository{moved, slash, dash})

	for _, r := range []repository.Repository{dash, slash, moved} {
		id := r.NumericID()
		if topo.Name(id) != r.Name || topo.ID(r.Name) != id {
			t.Errorf("%v: ID %d, name %v", r.Name, topo.ID(r.Name), topo.Name(id))
		}
	}
	// "a-b/x1" sorts before "a/x1" as text ('-' is below '/'), whatever the tuple (project, key) would say.
	if got, want := topo.Physical(), []int64{10, 11, 12}; !slices.Equal(got, want) {
		t.Errorf("Physical %v, want the order of the qualified texts %v", got, want)
	}
	if !topo.Less(10, 11) || topo.Less(11, 10) {
		t.Error("Less must follow the qualified texts")
	}
	sc := Scope{topo: topo, whole: []int64{12}, rowRepos: []int64{10, 12}}
	if got := sc.Repos(); !slices.Equal(got, []int64{10, 12}) {
		t.Errorf("Repos %v", got)
	}
}

// R-API-029 (Q-API-432): the list by content takes each (version, content) pair of a batch once, a version that names two contents (two
// holders) as two pairs and one without a stored content as one; the order the lead check compares is the order of the versions (a ranked
// version before an unranked one, the ranked by key, the unranked by time and name); a page is cut after a version's last entry, never
// between two entries of one version; and the cursor of a position is the version, or for an unranked one its time and the version.
// Microseconds, pure functions.
func TestR_API_029_TheListByContentGroupsAndCutsAtAVersion(t *testing.T) {
	a, b := []byte("a"), []byte("b")
	t0 := time.Date(2026, 11, 25, 7, 0, 0, 0, time.UTC)
	k := func(v string, ds ...[]byte) pageKey {
		p := pageKey{version: v, key: fakeAdapter{}.SortKey(v), published: t0}
		for _, d := range ds {
			p.withDigest(d)
		}
		p.withDigest(a) // a second time: once
		return p
	}
	cands := candidatesOf([]pageKey{k("2.0", b), k("1.0"), {version: "latest", unranked: true, published: t0}})
	var got []string
	for _, c := range cands {
		got = append(got, c.key.version+":"+string(c.digest))
	}
	if !slices.Equal(got, []string{"2.0:b", "2.0:a", "1.0:a", "latest:"}) {
		t.Errorf("the pairs of a batch: %v", got)
	}

	ranked1, ranked2 := pageKey{version: "1.0", key: fakeAdapter{}.SortKey("1.0")}, pageKey{version: "2.0", key: fakeAdapter{}.SortKey("2.0")}
	older := pageKey{version: "main", unranked: true, published: t0}
	newer := pageKey{version: "latest", unranked: true, published: t0.Add(time.Minute)}
	sameTime := pageKey{version: "sha-1", unranked: true, published: t0}
	for _, tc := range []struct {
		x, y pageKey
		want bool
	}{
		{ranked2, ranked1, true},
		{ranked1, ranked2, false},
		{ranked1, newer, true},
		{newer, ranked1, false},
		{newer, older, true},
		{older, newer, false},
		{sameTime, older, true},
		{older, sameTime, false},
	} {
		if got := earlier(tc.x, tc.y); got != tc.want {
			t.Errorf("earlier(%s, %s) = %v", tc.x.version, tc.y.version, got)
		}
	}

	entries := []imageCandidate{{key: k("3.0")}, {key: k("2.0"), digest: a}, {key: k("2.0"), digest: b}, {key: k("1.0")}}
	for limit, want := range map[int]int{1: 1, 2: 1, 3: 3, 4: 4} {
		if got := len(cutAtVersion(entries, limit)); got != want {
			t.Errorf("limit %d: %d entries, want %d", limit, got, want)
		}
	}
	if got := len(cutAtVersion(entries[1:], 1)); got != 2 {
		t.Errorf("a first version with two entries and limit 1: %d entries, want the version whole", got)
	}
	if got := positionOf(older); !slices.Equal(got, []string{t0.Format(time.RFC3339Nano), "main"}) {
		t.Errorf("the position of an unranked version: %v", got)
	}
	if got := positionOf(ranked1); !slices.Equal(got, []string{"1.0"}) {
		t.Errorf("the position of a ranked version: %v", got)
	}
}

// TestR_API_029_TheListReadsItsPageFirstOnlyInTheOrderOfTheName: the list reads the names of its page before their versions (the walk
// of PackageViewList, H-1683) in the order of the name, either direction, with the filters of the format, the repository and the
// project; a sort by time or relevance and every filter that a package's versions decide, or a name prefix, aggregate every version.
func TestR_API_029_TheListReadsItsPageFirstOnlyInTheOrderOfTheName(t *testing.T) {
	now := time.Now()
	plain := Filter{Formats: []string{"npm"}, Repositories: []qname.Repo{{Project: "p", Key: "r"}}, HasRepositories: true, Project: "p"}
	for _, c := range []struct {
		name string
		f    Filter
		o    Order
		want bool
	}{
		{"the name", plain, Order{Sort: "name"}, true},
		{"the name, highest first", plain, Order{Sort: "name", Desc: true}, true},
		{"the creation", plain, Order{Sort: "createdAt"}, false},
		{"the update", plain, Order{Sort: "updatedAt", Desc: true}, false},
		{"the relevance", plain, Order{Sort: "relevance"}, false},
		{"a text", Filter{Q: "ui"}, Order{Sort: "name"}, false},
		{"a keyword", Filter{Keyword: "ui"}, Order{Sort: "name"}, false},
		{"a name prefix", Filter{NamePrefix: "@acme/"}, Order{Sort: "name"}, false},
		{"deprecated", Filter{Deprecated: true}, Order{Sort: "name"}, false},
		{"signed", Filter{Signed: true}, Order{Sort: "name"}, false},
		{"dependsOn", Filter{DependsOn: "lib"}, Order{Sort: "name"}, false},
		{"createdFrom", Filter{CreatedFrom: &now}, Order{Sort: "name"}, false},
		{"createdTo", Filter{CreatedTo: &now}, Order{Sort: "name"}, false},
	} {
		if got := walks(c.f, c.o); got != c.want {
			t.Errorf("R-API-029 %s: walks %v, want %v", c.name, got, c.want)
		}
	}
}
