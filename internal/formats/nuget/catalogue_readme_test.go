package nuget

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/mirror/discover"
)

// readmeUpstream is a lent upstream whose service index names the README on a host it chooses; it refuses every host but its own as
// the catalogue's upstream does (foreign_host_denied, nothing sent) and answers the README address with a status.
type readmeUpstream struct {
	index  string
	status int
	asked  []string
}

func (u *readmeUpstream) Base() *url.URL {
	return &url.URL{Scheme: "https", Host: "feed.example", Path: "/v3/index.json"}
}
func (u *readmeUpstream) Settings() json.RawMessage { return nil }

func (u *readmeUpstream) Get(_ context.Context, target *url.URL, _ string, _ int64) (*discover.Response, error) {
	if target.Hostname() != "feed.example" {
		return nil, errs.E(errs.UpstreamFailed, errs.CodeForeignHostDenied, "the host "+target.Hostname()+" is none of the repository's")
	}
	u.asked = append(u.asked, target.String())
	return &discover.Response{Status: u.status, Header: http.Header{}, Body: []byte("# Acme\n")}, nil
}

func (u *readmeUpstream) Document(context.Context, string, string) ([]byte, bool, error) {
	return []byte(`{"version":"3.0.0","resources":[` +
		`{"@id":"https://feed.example/flat/","@type":"PackageBaseAddress/3.0.0"},` +
		`{"@id":"https://feed.example/reg/","@type":"RegistrationsBaseUrl/3.6.0"},` +
		`{"@id":"` + u.index + `","@type":"ReadmeUriTemplate/6.13.0"}]}`), true, nil
}

func (u *readmeUpstream) Artifact(context.Context, string, int64) ([]byte, bool, error) {
	return nil, false, nil
}

// TestR_CUR_028_TheReadmeOfANuGetPackageIsAskedWhereTheTemplateNamesIt: the README is asked at the address the template names, with
// the ID and the version lower-cased; a 404 is a package without one; an address on a host the repository does not reach is not asked
// and answers the host; a template that is no http or https address names none. Microseconds.
func TestR_CUR_028_TheReadmeOfANuGetPackageIsAskedWhereTheTemplateNamesIt(t *testing.T) {
	ctx := context.Background()
	u := &readmeUpstream{index: "https://feed.example/flat/{lower_id}/{lower_version}/readme", status: http.StatusOK}
	r, err := remoteAdapter{}.Readme(ctx, u, "Acme.Tools", "2.0.0-Beta")
	if err != nil || !r.Found || r.Text != "# Acme\n" || r.Version != "2.0.0-beta" ||
		len(u.asked) != 1 || u.asked[0] != "https://feed.example/flat/acme.tools/2.0.0-beta/readme" {
		t.Fatalf("R-CUR-028 the README: %+v %v, asked %v", r, err, u.asked)
	}
	u.status = http.StatusNotFound
	if r, err := (remoteAdapter{}).Readme(ctx, u, "acme.tools", "2.0.0"); err != nil || r.Found || r.Unreachable != "" {
		t.Fatalf("R-CUR-028 a version without a README: %+v %v", r, err)
	}
	far := &readmeUpstream{index: "https://readmes.example/{lower_id}/{lower_version}", status: http.StatusOK}
	if r, err := (remoteAdapter{}).Readme(ctx, far, "acme.tools", "2.0.0"); err != nil || r.Found || r.Unreachable != "readmes.example" || len(far.asked) != 0 {
		t.Fatalf("R-CUR-028 a README on a host the repository does not reach: %+v %v, asked %v", r, err, far.asked)
	}
	odd := &readmeUpstream{index: "file:///etc/{lower_id}", status: http.StatusOK}
	if r, err := (remoteAdapter{}).Readme(ctx, odd, "acme.tools", "2.0.0"); err != nil || r.Found || r.Unreachable != "" || len(odd.asked) != 0 {
		t.Fatalf("R-CUR-028 a template that is no web address: %+v %v", r, err)
	}
}
