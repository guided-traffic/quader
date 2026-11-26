package nuget

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/nuget/version"
	"github.com/guided-traffic/quader/internal/mirror/discover"
)

// What the tab Upstream shows of a NuGet package that is not stored (remote-curation.md R-CUR-028, formats/nuget.md R-NUGET-035): the
// facts of the newest registration leaf, which the lookup reads for the versions anyway, and the README the upstream announces with
// ReadmeUriTemplate/6.13.0 in its service index (nuget.org's flat container `…/{lower_id}/{lower_version}/readme`): one request, to an
// address the template names, which the lent upstream reaches only on the upstream's host or an extraHosts entry (R-REPO-043).

var _ discover.Documented = remoteAdapter{}

// newestLeaf is the newest version's catalog entry of a registration.
type newestLeaf struct {
	version, description string
	entry                json.RawMessage
}

// about reads the facts of the newest leaf: the licence expression (else the licence's address as a link), the project's address, the
// dependencies of every target framework and the deprecation.
func (l newestLeaf) about() discover.About {
	if l.version == "" {
		return discover.About{}
	}
	var e struct {
		LicenseExpression string `json:"licenseExpression"`
		LicenseURL        string `json:"licenseUrl"`
		ProjectURL        string `json:"projectUrl"`
		DependencyGroups  []struct {
			TargetFramework string `json:"targetFramework"`
			Dependencies    []struct {
				ID    string `json:"id"`
				Range string `json:"range"`
			} `json:"dependencies"`
		} `json:"dependencyGroups"`
		Deprecation *struct {
			Message          string   `json:"message"`
			Reasons          []string `json:"reasons"`
			AlternatePackage *struct {
				ID    string `json:"id"`
				Range string `json:"range"`
			} `json:"alternatePackage"`
		} `json:"deprecation"`
	}
	_ = json.Unmarshal(l.entry, &e) //nolint:errcheck // a leaf that does not read tells nothing more than its version
	a := discover.About{Version: l.version, License: strings.TrimSpace(e.LicenseExpression)}
	if e.ProjectURL != "" {
		a.Links = append(a.Links, discover.Link{Kind: discover.LinkHomepage, URL: e.ProjectURL})
	}
	if a.License == "" && e.LicenseURL != "" {
		a.Links = append(a.Links, discover.Link{Kind: discover.LinkLicense, URL: e.LicenseURL})
	}
	for _, g := range e.DependencyGroups {
		for _, d := range g.Dependencies {
			a.Dependencies = append(a.Dependencies, discover.Dependency{Name: d.ID, Range: d.Range, Group: g.TargetFramework})
		}
	}
	if d := e.Deprecation; d != nil {
		a.Deprecation = &discover.Deprecation{Message: d.Message, Reasons: d.Reasons}
		if alt := d.AlternatePackage; alt != nil {
			a.Deprecation.Alternate, a.Deprecation.AlternateRange = alt.ID, alt.Range
		}
	}
	return a
}

// keywords are the newest leaf's tags.
func (l newestLeaf) keywords() []string {
	var e struct {
		Tags json.RawMessage `json:"tags"`
	}
	if json.Unmarshal(l.entry, &e) != nil {
		return nil
	}
	return tagList(e.Tags) // the list a registration writes, or the string of words an older feed's leaf has (view.go)
}

// Readme implements [discover.Documented]: the README the service index's template names for the version, one request. An upstream
// that announces no template, or a version that has no README there (404), answers Found false; an address on a host the repository
// does not reach answers Unreachable with the host, and nothing is sent.
func (remoteAdapter) Readme(ctx context.Context, u discover.Upstream, name, ver string) (discover.Readme, error) {
	lower, ok := lowerID(name)
	v, err := version.Parse(ver)
	if !ok || err != nil {
		return discover.Readme{}, nil //nolint:nilerr // a name or a version the format does not read names no README
	}
	r := discover.Readme{Version: v.URLForm()}
	d, err := feedDiscovery(ctx, u)
	if err != nil || d == nil {
		return r, err
	}
	target, ok := d.ReadmeURL(lower, v.URLForm())
	if !ok {
		return r, nil
	}
	resp, err := u.Get(ctx, target, "text/markdown, text/plain, */*", discover.MaxReadmeBytes)
	if host, refused := formats.UnreachedHost(err, target); refused {
		r.Unreachable = host
		return r, nil
	}
	if err != nil {
		return discover.Readme{}, err
	}
	switch {
	case resp.Status == http.StatusNotFound || resp.Status == http.StatusGone:
		return r, nil
	case resp.Status != http.StatusOK:
		return discover.Readme{}, errs.E(errs.UpstreamFailed, errs.CodeUpstreamError,
			"The upstream answered "+http.StatusText(resp.Status)+" ("+strconv.Itoa(resp.Status)+") to the README of "+lower+" "+v.URLForm()+".")
	}
	r.Found, r.Text = true, string(resp.Body)
	return discover.CutReadme(r), nil
}
