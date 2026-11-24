//go:build integration

package npm

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/test/integration/testenv"
	"github.com/guided-traffic/quader/test/npmstub/corpus"
)

// The delivery rules of an npm remote against the stack and the counting stub (remote-curation.md R-CUR-003, R-CUR-004): the case of
// each criterion of R2 that only the request path of a running replica shows. The rules' decision itself is the unit table of
// internal/remotecuration/rules, the documents' filter the kit's check 23; these cases add the stub's count (no upstream request for a
// refused package or version), the virtual walk and the record of a refusal. Data: the corpus's packages, a few kilobytes each.

// refusal checks the answer of R-CUR-004 in npm's shape: 403, the code, Cache-Control no-store, and the text that names the package, the
// version (or "any version") and the repository and says where to ask.
func refusal(t *testing.T, a testenv.Answer, repo, name, version string) {
	t.Helper()
	if a.Status != http.StatusForbidden || code(t, a) != "not_allowed_in_repository" || a.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("%d %q %s, want the rules' 403", a.Status, a.Header.Get("Cache-Control"), a.Body)
	}
	var b map[string]string
	_ = json.Unmarshal(a.Body, &b)
	if version == "" {
		version = "any version"
	}
	want := name + " " + version + " is not allowed in it/" + repo + ": "
	if !strings.HasPrefix(b["error"], want) || !strings.Contains(b["error"], "/ui/projects/it/repositories/"+repo+"/upstream?name=") {
		t.Errorf("the text %q, want it to start with %q and say where to ask", b["error"], want)
	}
}

// TestR_CUR_002_ABlockedPackageStaysListedAndIsMarkedNotDelivered: a rule does not hide what a remote stores, as an exclude pattern did:
// after a block, the version a client fetched before stays in the packages API, marked not available with the reason blocked, while the
// package protocol refuses the package without an upstream request (remote-curation.md R-CUR-002, R-CUR-014 "listed and not delivered").
// The stored version is a client's fetch before the rule, not an examination download (R-CUR-014's own case).
func TestR_CUR_002_ABlockedPackageStaysListedAndIsMarkedNotDelivered(t *testing.T) {
	r := newRemoteEnv(t, remoteOptions{})
	p := upstreamPackage(t, 2)
	v := p.Versions[0].Version
	rep := r.start(1)[0]
	if a := r.get(rep, rRemote, p.Name+"/-/"+corpus.TarballFile(p.Name, v), r.tokA); a.Status != http.StatusOK {
		t.Fatalf("the fetch before the rule: %d %.300s", a.Status, a.Body)
	}
	r.st.MustPutDeliveryRule(rep, r.opsPAT, rRemote, testenv.PackageRule(p.Name, "block"))
	r.st.WaitDelivered(t, rep, r.opsPAT, rRemote, p.Name, "", false)
	docs := r.stub.Counts().Documents(p.Name)

	t.Run("R-CUR-002 the package protocol refuses the blocked package without an upstream request", func(t *testing.T) {
		refusal(t, r.view(rep, rRemote, p.Name, r.tokA), rRemote, p.Name, "")
		if n := r.stub.Counts().Documents(p.Name); n != docs {
			t.Errorf("the upstream was asked %d times for the blocked package's document", n-docs)
		}
	})
	t.Run("R-CUR-002 R-CUR-014 the stored version is listed, not available, with the reason blocked", func(t *testing.T) {
		var id string
		testenv.WaitFor(t, "the stored package in the packages API", 10*time.Second, func() bool {
			a := r.st.Do(rep, http.MethodGet, "/api/v1/packages/lookup?format=npm&name="+p.Name, r.tokA, nil)
			if a.Status != http.StatusOK {
				return false
			}
			id, _ = a.JSON(t)["id"].(string)
			return id != ""
		})
		a := r.st.Do(rep, http.MethodGet, "/api/v1/packages/"+id+"/versions/"+v, r.tokA, nil)
		if a.Status != http.StatusOK {
			t.Fatalf("the version: %d %s", a.Status, a.Body)
		}
		var d struct {
			Repositories []struct {
				Key, UnavailableReason string
				Available              bool
			} `json:"repositories"`
		}
		if err := json.Unmarshal(a.Body, &d); err != nil {
			t.Fatal(err)
		}
		found := false
		for _, rp := range d.Repositories {
			if strings.HasSuffix(rp.Key, rRemote) {
				found = true
				if rp.Available || rp.UnavailableReason != "blocked" {
					t.Errorf("the remote's entry: available %v, reason %q; want not available, blocked", rp.Available, rp.UnavailableReason)
				}
			}
		}
		if !found {
			t.Errorf("the version does not list the remote: %s", a.Body)
		}
	})
}

// TestR_CUR_003_AnNpmRemoteInModeOnlyAllowedDeliversTheAllowedVersionOnly: with one version allowed, the package document lists that
// version only, its tarball is fetched and served, the other version's tarball and a package without a rule are refused before any
// upstream request (R-CUR-003 points 1 and 3, R-CUR-004).
func TestR_CUR_003_AnNpmRemoteInModeOnlyAllowedDeliversTheAllowedVersionOnly(t *testing.T) {
	r := newRemoteEnv(t, remoteOptions{})
	const only = "npm-only"
	p := upstreamPackage(t, 2)
	var other *corpus.Package
	for _, c := range corpus.Default().Packages {
		if c.Name != p.Name && !strings.HasPrefix(c.Name, "@") && len(c.Versions) < 30 {
			other = c
			break
		}
	}
	allowed, refused := p.Versions[0].Version, p.Versions[1].Version
	r.st.Repository(only, "remote", "npm", r.remoteConfig(3600, map[string]any{"delivery": testenv.OnlyAllowed()}, nil))
	r.st.DeliveryRule(only, testenv.VersionRule(p.Name, allowed, "allow"))
	r.st.Policy("only-readers", []string{"read"}, []string{only}, r.alice)
	rep := r.start(1)[0]

	t.Run("R-CUR-003 the package document lists the allowed version only", func(t *testing.T) {
		a := r.view(rep, only, p.Name, r.tokA)
		if a.Status != http.StatusOK {
			t.Fatalf("%d %s", a.Status, a.Body)
		}
		if got := versionsOf(t, a); !slices.Equal(got, []string{allowed}) {
			t.Errorf("versions %v, want [%s]", got, allowed)
		}
		if got := tagsOf(t, a)["latest"]; got != allowed {
			t.Errorf("latest %q, want the allowed %s", got, allowed)
		}
	})
	t.Run("R-CUR-003 the allowed version's tarball is fetched and served", func(t *testing.T) {
		file := corpus.TarballFile(p.Name, allowed)
		if a := r.get(rep, only, p.Name+"/-/"+file, r.tokA); a.Status != http.StatusOK {
			t.Fatalf("%d %.300s", a.Status, a.Body)
		}
		if n := r.stub.Counts().Tarball(p.Name, file); n != 1 {
			t.Errorf("the upstream was asked %d times for the allowed tarball, want once", n)
		}
	})
	t.Run("R-CUR-003 R-CUR-004 the other version's tarball is refused and the upstream is not asked", func(t *testing.T) {
		file := corpus.TarballFile(p.Name, refused)
		refusal(t, r.get(rep, only, p.Name+"/-/"+file, r.tokA), only, p.Name, refused)
		if n := r.stub.Counts().Tarball(p.Name, file); n != 0 {
			t.Errorf("the upstream was asked %d times for the refused tarball", n)
		}
	})
	t.Run("R-CUR-003 R-CUR-004 a package without a rule is refused at its document and the upstream is not asked", func(t *testing.T) {
		refusal(t, r.view(rep, only, other.Name, r.tokA), only, other.Name, "")
		if n := r.stub.Counts().Documents(other.Name); n != 0 {
			t.Errorf("the upstream was asked %d times for the document of a package without a rule", n)
		}
	})
}

// TestR_CUR_004_AVirtualRepositoryWalksPastARefusingMember: a remote's refusal is "not found" of that member, so a virtual repository
// serves the package from the next member that delivers it; with only the refusing member, the answer is that member's 403 and its text
// names the member (R-CUR-004, Q-CUR-8). Two refusals of one client within a minute write one remote.delivery.refused and count 2 in the
// request list's entry of the subject once the recorder's sums are written (every 5 s).
func TestR_CUR_004_AVirtualRepositoryWalksPastARefusingMember(t *testing.T) {
	r := newRemoteEnv(t, remoteOptions{})
	const refusing, both, alone = "npm-refusing", "npm-cur-both", "npm-cur-alone"
	p := upstreamPackage(t, 2)
	// Two versions blocked by version rules: the first for the walk, the second for the record, so the walk's refusal is not counted
	// with the two of the record.
	v, counted := p.Versions[0].Version, p.Versions[1].Version
	file := corpus.TarballFile(p.Name, v)
	r.st.Repository(refusing, "remote", "npm", r.remoteConfig(3600, nil, nil))
	r.st.DeliveryRule(refusing, testenv.VersionRule(p.Name, v, "block"))
	r.st.DeliveryRule(refusing, testenv.VersionRule(p.Name, counted, "block"))
	for key, members := range map[string][]string{both: {refusing, rRemote}, alone: {refusing}} {
		r.st.Repository(key, "virtual", "npm", `{}`)
		for i, m := range members {
			r.st.Exec(`INSERT INTO virtual_members (virtual_id, position, member_id)
				SELECT v.id, $2, m.id FROM repositories v, repositories m WHERE v.key = $1 AND m.key = $3`, key, i, m)
		}
	}
	r.st.Policy("cur-readers", []string{"read"}, []string{refusing, both, alone}, r.alice)
	rep := r.start(1)[0]

	t.Run("R-CUR-004 the second member serves what the first refuses", func(t *testing.T) {
		if a := r.get(rep, both, p.Name+"/-/"+file, r.tokA); a.Status != http.StatusOK {
			t.Fatalf("%d %.300s", a.Status, a.Body)
		}
		if n := r.itemCount(rRemote, p.Name+"/-/"+file); n != 1 {
			t.Errorf("the tarball is stored %d times in the delivering member, want once", n)
		}
		if n := r.itemCount(refusing, p.Name+"/-/"+file); n != 0 {
			t.Errorf("the refusing member stored the tarball")
		}
		if n := r.stub.Counts().Tarball(p.Name, file); n != 1 {
			t.Errorf("the upstream was asked %d times for the tarball, want once (by the delivering member)", n)
		}
	})
	t.Run("R-CUR-004 with only the refusing member the answer is its 403, naming it", func(t *testing.T) {
		refusal(t, r.get(rep, alone, p.Name+"/-/"+file, r.tokA), refusing, p.Name, v)
	})
	t.Run("R-CUR-004 two refusals within a minute write one event and count 2 in the request list", func(t *testing.T) {
		other := counted
		otherFile := corpus.TarballFile(p.Name, other)
		for range 2 {
			refusal(t, r.get(rep, refusing, p.Name+"/-/"+otherFile, r.tokA), refusing, p.Name, other)
		}
		count := func() int64 {
			return r.st.Number(`SELECT COALESCE(sum(d.refusals), 0) FROM remote_delivery_requests d JOIN repositories r ON r.id = d.repo_id
				WHERE r.key = $1 AND d.name = $2 AND d.version = $3`, refusing, p.Name, other)
		}
		testenv.WaitFor(t, "the request list's count", 10*time.Second, func() bool { return count() >= 2 })
		if n := count(); n != 2 {
			t.Errorf("the request list counts %d refusals, want 2", n)
		}
		events := func() int64 {
			return r.st.Number(`SELECT count(*) FROM audit_event WHERE action = 'remote.delivery.refused' AND repository = $1
				AND details->>'version' = $2`, "it/"+refusing, other)
		}
		testenv.WaitFor(t, "the refusal's event", 10*time.Second, func() bool { return events() >= 1 })
		if n := events(); n != 1 {
			t.Errorf("%d remote.delivery.refused events for two refusals within a minute, want 1", n)
		}
	})
}
