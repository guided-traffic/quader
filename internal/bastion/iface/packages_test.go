package iface_test

import (
	"crypto/sha256"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/auth/registrytoken"
	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/qname"
)

// The interface's part of the read shapes per package (bastion.md R-BAS-082, R-BAS-083, R-BAS-087): the two marking headers,
// the check's packages and the unverified reading of a registry token's grant and expiry.

// markedWith returns the headers of a marking of a shared document of web/npm-local with the given package headers.
func markedWith(pkg []string, read []string) http.Header {
	h := http.Header{}
	iface.Marking{Class: iface.ClassShared, Repository: "web/npm-local", Read: iface.ReadPartial, Subject: iface.Subject{Kind: iface.SubjectAnonymous}, Interface: iface.InterfaceVersion}.Set(h)
	if pkg != nil {
		h[iface.HeaderPackage] = pkg
	}
	if read != nil {
		h[iface.HeaderPackageRead] = read
	}
	return h
}

// TestR_BAS_082_TheMarkingCarriesThePackage: a package travels percent-encoded with its shape and comes back decoded; npm's
// scoped name is sent as it is, Debian's g++ as g%2B%2B; a marking without a package, or with a name outside the bounds, sets
// neither header; a package without a shape is written partial; the strip removes both.
func TestR_BAS_082_TheMarkingCarriesThePackage(t *testing.T) {
	for _, c := range []struct {
		pkg, wire string
		read      iface.Read
	}{
		{"library/nginx", "library/nginx", iface.ReadFull},
		{"@acme/sdk", "@acme/sdk", iface.ReadPartial},
		{"g++", "g%2B%2B", iface.ReadFull},
		{"Ab-9._~@/%: é", "Ab-9._~@/%25%3A%20%C3%A9", iface.ReadPartial},
		{strings.Repeat("x", iface.MaxPackageBytes), strings.Repeat("x", iface.MaxPackageBytes), iface.ReadFull},
	} {
		h := http.Header{}
		iface.Marking{Class: iface.ClassArtifact, Subject: iface.Subject{Kind: iface.SubjectAnonymous}, Interface: iface.InterfaceVersion, Package: c.pkg, PackageRead: c.read}.Set(h)
		if got := h.Get(iface.HeaderPackage); got != c.wire {
			t.Errorf("%q is written %q, want %q", c.pkg, got, c.wire)
		}
		m, ok, err := iface.ParseMarking(h)
		if err != nil || !ok || m.Package != c.pkg || m.PackageRead != c.read {
			t.Errorf("%q comes back as %+v, ok=%v, err=%v", c.pkg, m, ok, err)
		}
	}
	for name, mk := range map[string]iface.Marking{
		"no package":            {Class: iface.ClassArtifact, PackageRead: iface.ReadFull},
		"a name of 513 bytes":   {Class: iface.ClassArtifact, Package: strings.Repeat("x", iface.MaxPackageBytes+1), PackageRead: iface.ReadFull},
		"a control character":   {Class: iface.ClassArtifact, Package: "a\nb", PackageRead: iface.ReadFull},
		"a name that is no UTF": {Class: iface.ClassArtifact, Package: "a\xffb", PackageRead: iface.ReadFull},
	} {
		h := http.Header{}
		mk.Interface = iface.InterfaceVersion
		mk.Set(h)
		if h.Get(iface.HeaderPackage) != "" || h.Get(iface.HeaderPackageRead) != "" {
			t.Errorf("a marking with %s sets the package headers: %v", name, h)
		}
		if m, _, err := iface.ParseMarking(h); err != nil || m.Package != "" || m.PackageRead != "" {
			t.Errorf("a marking with %s reads %+v, %v", name, m, err)
		}
	}
	for _, read := range []iface.Read{"", iface.ReadNone, "most"} {
		h := http.Header{}
		iface.Marking{Class: iface.ClassArtifact, Package: "app", PackageRead: read}.Set(h)
		if got := h.Get(iface.HeaderPackageRead); got != string(iface.ReadPartial) {
			t.Errorf("a package read %q is written %q, want partial", read, got)
		}
	}
	h := markedWith([]string{"app"}, []string{"full"})
	iface.StripMarking(h)
	if len(h) != 0 {
		t.Errorf("the strip leaves %v", h)
	}
}

// TestR_BAS_082_ParseMarkingRefusesAMalformedPackage: each of the five malformed cases of R-BAS-082 is ErrMarking.
func TestR_BAS_082_ParseMarkingRefusesAMalformedPackage(t *testing.T) {
	cases := map[string]struct{ pkg, read []string }{
		// One header without the other.
		"a package without its shape": {[]string{"app"}, nil},
		"a shape without its package": {nil, []string{"full"}},
		// More than one value of either.
		"two packages": {[]string{"app", "other"}, []string{"full"}},
		"two shapes":   {[]string{"app"}, []string{"full", "full"}},
		// Any other value of the shape.
		"the shape none":       {[]string{"app"}, []string{"none"}},
		"the shape FULL":       {[]string{"app"}, []string{"FULL"}},
		"an empty shape":       {[]string{"app"}, []string{""}},
		"a list in one header": {[]string{"app"}, []string{"full, partial"}},
		// A value that does not decode strictly.
		"a plus sent as itself":       {[]string{"g++"}, []string{"full"}},
		"lower-case hexadecimal":      {[]string{"g%2b%2b"}, []string{"full"}},
		"a cut escape":                {[]string{"g%2"}, []string{"full"}},
		"a percent sign alone":        {[]string{"%"}, []string{"full"}},
		"an escaped verbatim byte":    {[]string{"%41pp"}, []string{"full"}},
		"a space":                     {[]string{"a b"}, []string{"full"}},
		"two names joined by a comma": {[]string{"app,other"}, []string{"full"}},
		// A decoded name outside the bounds.
		"an empty name":              {[]string{""}, []string{"full"}},
		"a name of 513 bytes":        {[]string{strings.Repeat("x", iface.MaxPackageBytes+1)}, []string{"full"}},
		"a control character":        {[]string{"a%0Ab"}, []string{"full"}},
		"the delete character":       {[]string{"a%7F"}, []string{"full"}},
		"a C1 control character":     {[]string{"a%C2%85"}, []string{"full"}},
		"bytes that are not UTF-8":   {[]string{"a%C3%28"}, []string{"full"}},
		"513 bytes after decoding":   {[]string{strings.Repeat("%20", iface.MaxPackageBytes+1)}, []string{"full"}},
		"a zero byte after decoding": {[]string{"%00"}, []string{"full"}},
	}
	for name, c := range cases {
		m, ok, err := iface.ParseMarking(markedWith(c.pkg, c.read))
		if !ok || !errors.Is(err, iface.ErrMarking) {
			t.Errorf("%s: %+v ok=%v err=%v, want ErrMarking", name, m, ok, err)
		}
	}
}

// TestR_BAS_083_TheCheckCarriesPackages: both members are always present, [] when empty, and travel through the strict
// decoder in request order.
func TestR_BAS_083_TheCheckCarriesPackages(t *testing.T) {
	req, _ := iface.Marshal(iface.CheckRequest{ClientAddress: "203.0.113.7", Repositories: []string{"web/docker"}})
	ans, _ := iface.Marshal(iface.CheckAnswer{Subject: iface.Subject{Kind: iface.SubjectInvalid}})
	if !strings.Contains(string(req), `"packages":[]`) || !strings.Contains(string(ans), `"packages":[]`) {
		t.Fatalf("an empty member is not written as []: %s %s", req, ans)
	}
	want := iface.CheckAnswer{
		Subject:      iface.Subject{Kind: iface.SubjectAnonymous},
		Repositories: map[string]iface.RepositoryRead{"web/docker": {Read: iface.ReadPartial}},
		Packages:     iface.PackageReads{{Repository: "web/docker", Package: "library/nginx", Read: iface.ReadFull}, {Repository: "web/docker", Package: "a&b", Read: iface.ReadPartial}},
	}
	b, err := iface.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"packages":[{"repository":"web/docker","package":"library/nginx","read":"full"},{"repository":"web/docker","package":"a&b","read":"partial"}]`) {
		t.Errorf("the answer is written %s", b)
	}
	var back iface.CheckAnswer
	if err := iface.DecodeStrict(strings.NewReader(string(b)), &back); err != nil || len(back.Packages) != 2 || back.Packages[0] != want.Packages[0] || back.Packages[1] != want.Packages[1] {
		t.Fatalf("decode: %+v %v", back, err)
	}
	r := iface.CheckRequest{ClientAddress: "203.0.113.7", Repositories: []string{"web/docker"}, Packages: iface.CheckPackages{{Repository: "web/docker", Package: "library/nginx"}}}
	b, _ = iface.Marshal(r)
	var rback iface.CheckRequest
	if err := iface.DecodeStrict(strings.NewReader(string(b)), &rback); err != nil || len(rback.Packages) != 1 || rback.Packages[0] != r.Packages[0] {
		t.Fatalf("decode: %s -> %+v %v", b, rback, err)
	}
}

// TestR_BAS_083_ACheckRequestsPackagesAreValidated: the rules of R-BAS-083 name the first element that breaks one.
func TestR_BAS_083_ACheckRequestsPackagesAreValidated(t *testing.T) {
	pair := func(repo, pkg string) iface.CheckPackage { return iface.CheckPackage{Repository: repo, Package: pkg} }
	repos := []string{"web/docker", "web/npm-local"}
	ok := iface.CheckRequest{Repositories: repos, Packages: iface.CheckPackages{pair("web/docker", "app"), pair("web/npm-local", "app"), pair("web/docker", "other")}}
	if p := ok.PackagesProblem(3); p != nil {
		t.Fatalf("a valid request: %+v", p)
	}
	if p := (iface.CheckRequest{Repositories: repos}).PackagesProblem(3); p != nil {
		t.Fatalf("no packages: %+v", p)
	}
	for name, c := range map[string]struct {
		pkgs  iface.CheckPackages
		index int
		code  string
	}{
		"more than the bound":          {iface.CheckPackages{pair("web/docker", "a"), pair("web/docker", "b"), pair("web/docker", "c"), pair("web/docker", "d")}, 3, "count"},
		"an empty name":                {iface.CheckPackages{pair("web/docker", "a"), pair("web/docker", "")}, 1, "invalid_package"},
		"a name of 513 bytes":          {iface.CheckPackages{pair("web/docker", strings.Repeat("x", iface.MaxPackageBytes+1))}, 0, "invalid_package"},
		"a control character":          {iface.CheckPackages{pair("web/docker", "a\x00")}, 0, "invalid_package"},
		"a repository not named":       {iface.CheckPackages{pair("web/docker", "a"), pair("web/other", "a")}, 1, "unknown_repository"},
		"a repository in another case": {iface.CheckPackages{pair("WEB/docker", "a")}, 0, "unknown_repository"},
		"a pair twice":                 {iface.CheckPackages{pair("web/docker", "a"), pair("web/npm-local", "a"), pair("web/docker", "a")}, 2, "duplicate"},
	} {
		p := (iface.CheckRequest{Repositories: repos, Packages: c.pkgs}).PackagesProblem(3)
		if p == nil || p.Index != c.index || p.Code != c.code || p.Detail == "" {
			t.Errorf("%s: %+v, want index %d code %s", name, p, c.index, c.code)
		}
	}
}

// issueRegistryToken issues a registry token with the real token service's encoder, as the main installation does.
func issueRegistryToken(t *testing.T, s *registrytoken.Signer, exp time.Time, entries ...registrytoken.Entry) string {
	t.Helper()
	tok, err := s.Issue(registrytoken.Claims{
		PrincipalKind: "user", PrincipalID: "0192f0c4-7a7e-7b51-9b1e-2f0d6c9e1a10",
		Binding:  registrytoken.Binding{TokenID: "0192f0c4-0000-7000-8000-000000000001", TokenType: "pat", SecretSHA256: sha256.Sum256([]byte("qdr_pat_x"))},
		Endpoint: "default", IssuedAt: exp.Add(-5 * time.Minute), ExpiresAt: exp, Grant: registrytoken.Grant{Entries: entries},
	})
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

// TestR_BAS_087_TheGrantIsReadAsTheTokenServiceWritesIt: RegistryTokenGrant reads the grant of a token from the real encoder,
// entry by entry as the verifier decodes it, and Pulls compares repository and image as opaque strings with the pull bit; a
// token whose claims do not decode, and one of a system subject, yield no grant (Q-BAS-246).
func TestR_BAS_087_TheGrantIsReadAsTheTokenServiceWritesIt(t *testing.T) {
	if iface.RegistryGrantPull != int(registrytoken.Pull) {
		t.Fatalf("the pull bit of the interface %d is not the token service's %d", iface.RegistryGrantPull, registrytoken.Pull)
	}
	s := issuer(t)
	docker, other := qname.Repo{Project: "web", Key: "docker"}, qname.Repo{Project: "web", Key: "other"}
	exp := time.Now().Add(time.Minute).Truncate(time.Second)
	tok := issueRegistryToken(t, s, exp,
		registrytoken.Entry{Repo: docker, Name: "library/nginx", Actions: registrytoken.Pull},
		registrytoken.Entry{Repo: docker, Name: "app", Actions: registrytoken.Push | registrytoken.Delete},
		registrytoken.Entry{Repo: other, Name: "library/nginx", Actions: registrytoken.All},
	)
	claims, err := s.Verify(tok, "default", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	g, ok := iface.RegistryTokenGrant(tok)
	if !ok || len(g) != len(claims.Grant.Entries) {
		t.Fatalf("grant = %+v, ok=%v; the verifier reads %+v", g, ok, claims.Grant.Entries)
	}
	for i, e := range claims.Grant.Entries {
		if g[i] != (iface.RegistryGrantEntry{Repository: e.Repo.String(), Name: e.Name, Actions: int(e.Actions)}) {
			t.Errorf("entry %d = %+v, the verifier reads %+v", i, g[i], e)
		}
		if g.Pulls(e.Repo.String(), e.Name) != claims.Grant.Allows(e.Repo, e.Name, registrytoken.Pull) {
			t.Errorf("entry %d: Pulls disagrees with Grant.Allows", i)
		}
	}
	for _, c := range []struct {
		repo, pkg string
		want      bool
	}{
		{"web/docker", "library/nginx", true},
		{"web/other", "library/nginx", true},
		{"web/docker", "app", false},
		{"web/docker", "library/redis", false},
		{"web/docker-local", "library/nginx", false},
		{"web/docker", "", false},
	} {
		if got := g.Pulls(c.repo, c.pkg); got != c.want {
			t.Errorf("Pulls(%s, %q) = %v, want %v", c.repo, c.pkg, got, c.want)
		}
	}
	if g, ok := iface.RegistryTokenGrant(issueRegistryToken(t, s, exp)); !ok || len(g) != 0 || g.Pulls("web/docker", "app") {
		t.Errorf("a token without a grant reads %+v, ok=%v", g, ok)
	}
	for name, tok := range map[string]string{
		"claims that are no JSON":         forged(`1:k1:g=web/docker`),
		"a payload of another version":    forged(`2:k1:{"g":[{"r":"web/docker","n":"app","a":1}]}`),
		"actions that are no number":      forged(`1:k1:{"g":[{"r":"web/docker","n":"app","a":"pull"}]}`),
		"a grant that is no list":         forged(`1:k1:{"g":{"r":"web/docker"}}`),
		"a scan token's system subject":   forged(`1:k1:{"s":"scan","g":[{"r":"web/docker","n":"app","a":1}]}`),
		"a value outside the form":        iface.RegistryTokenPrefix + "!!",
		"a payload that is no token form": forged("garbage"),
	} {
		if g, ok := iface.RegistryTokenGrant(tok); ok || g.Pulls("web/docker", "app") {
			t.Errorf("%s yields the grant %+v", name, g)
		}
	}
	if g, ok := iface.RegistryTokenGrant(forged(`1:k1:{"g":[{"r":"web/docker","n":"app","a":1}],"new":1}`)); !ok || !g.Pulls("web/docker", "app") {
		t.Error("a member the interface does not know hides the grant")
	}
	if g, ok := iface.RegistryTokenGrant(forged(`1:k1:{"g":[{"r":"web/docker","n":"","a":1}]}`)); !ok || g.Pulls("web/docker", "") {
		t.Error("an entry without a name pulls the empty package")
	}
}

// TestR_BAS_087_TheExpiryIsReadAsTheTokenServiceWritesIt: RegistryTokenExpiry reads the expiry of a token from the real encoder;
// claims without one, or with one that is not positive, yield none.
func TestR_BAS_087_TheExpiryIsReadAsTheTokenServiceWritesIt(t *testing.T) {
	s := issuer(t)
	exp := time.Now().Add(2 * time.Minute).Truncate(time.Second)
	tok := issueRegistryToken(t, s, exp)
	claims, err := s.Verify(tok, "default", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	got, ok := iface.RegistryTokenExpiry(tok)
	if !ok || !got.Equal(claims.ExpiresAt) || !got.Equal(exp) || got.Location() != time.UTC {
		t.Fatalf("expiry = %v ok=%v, the verifier reads %v", got, ok, claims.ExpiresAt)
	}
	for name, tok := range map[string]string{
		"no expiry":                    forged(`1:k1:{"e":"default"}`),
		"an expiry of zero":            forged(`1:k1:{"exp":0}`),
		"a negative expiry":            forged(`1:k1:{"exp":-5}`),
		"an expiry that is no number":  forged(`1:k1:{"exp":"soon"}`),
		"a payload of another version": forged(`2:k1:{"exp":1893456000}`),
		"a value outside the form":     iface.RegistryTokenPrefix + "!!",
	} {
		if got, ok := iface.RegistryTokenExpiry(tok); ok {
			t.Errorf("%s yields the expiry %v", name, got)
		}
	}
}
