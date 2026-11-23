package scanner

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/guided-traffic/quader/internal/outbound"
	"github.com/guided-traffic/quader/internal/qname"
	"github.com/guided-traffic/quader/internal/vuln/imagescan"
)

// The scanner's pull through the registry (R-VULN-042): the manifest by its digest, then the config and every layer the manifest
// names, by digest, from the scan listener of packages, with the call's scan token as Bearer, over the link's TLS. It addresses the
// repository the call names under its plain key, follows no redirect and reads no other address; the checks of R-VULN-036 run on
// what it pulls (imagescan.BuildLayout).

// ErrorCodeHeader is the header in which the scan listener names the quader code of a refusal (Q-VULN-180): the registry's error
// shape has no member for it, and the scanner tells an uncached layer of a remote (populate_not_permitted) from a refused token by it.
const ErrorCodeHeader = "Quader-Error-Code"

// populateNotPermitted is the code of a remote's refusal to fill for a reader who may not fill (R-AUTHZ-008); repositoryDisabled the
// code of a 404 for a repository the scan listener's replica holds for disabled (Q-VULN-226).
const (
	populateNotPermitted = "populate_not_permitted"
	repositoryDisabled   = "repository_disabled"
)

// The manifest media types the pull accepts: the image manifests the job scans (R-VULN-033).
var manifestAccept = strings.Join([]string{
	"application/vnd.oci.image.manifest.v1+json", "application/vnd.docker.distribution.manifest.v2+json",
}, ", ")

// errPullRefused and errRegistryUnavailable are the two answers of the pull that belong to the installation: the scan listener
// refused the token (401, or 403 with another code than populate_not_permitted), or it could not be reached or failed (5xx). The
// handler answers them 403 pull_refused and 502 registry_unavailable (R-VULN-041).
var (
	errPullRefused         = errors.New("the scan listener refused the scan token")
	errRegistryUnavailable = errors.New("the scan listener could not be reached")
	// errRepositoryDisabled is a 404 that names repository_disabled: the job read the repository as enabled, the scan listener's replica
	// has not followed the switch yet (Q-VULN-226). The handler answers it 409 repository_disabled, which the worker puts off.
	errRepositoryDisabled = errors.New("the scan listener holds the repository for disabled")
)

// Registry is the scan listener as the scanner reaches it: its origin and the link's TLS (nil: plain HTTP, Q-HA-322).
type Registry struct {
	// URL is vulnerabilities.images.registry.url, an origin.
	URL string
	// TLS returns the configuration of one connection (httpserver.LinkTLS.Config).
	TLS func() *tls.Config
}

// puller is the pull of one call: its repository, image name and token, over a client of its own whose connections end with the call.
type puller struct {
	base   string
	repo   qname.Repo
	name   string
	token  string
	client *http.Client
}

var _ imagescan.BlobReader = (*puller)(nil)

func (r Registry) puller(repo qname.Repo, name, token string) *puller {
	return &puller{
		base: strings.TrimSuffix(r.URL, "/"), repo: repo, name: name, token: token,
		// Connections kept for the blobs of the call and closed with it; the link's TLS for each connection; no redirect.
		client: outbound.LinkClient(outbound.LinkOptions{TLS: r.TLS, KeepAlive: true}),
	}
}

// close ends the connections of the call.
func (p *puller) close() { p.client.CloseIdleConnections() }

// path is the escaped path of a request of the image: /v2/<project>/<key>/<name>/<kind>/<digest>. The name's segments are escaped one
// by one, so no character of a call can change the path's structure.
func (p *puller) path(kind, digest string) string {
	segs := []string{"", "v2", url.PathEscape(p.repo.Project), url.PathEscape(p.repo.Key)}
	for _, s := range strings.Split(p.name, "/") {
		segs = append(segs, url.PathEscape(s))
	}
	return strings.Join(append(segs, kind, url.PathEscape(digest)), "/")
}

func (p *puller) get(ctx context.Context, kind, digest, accept string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.base+p.path(kind, digest), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+p.token)
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	res, err := p.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, context.Cause(ctx)
		}
		return nil, fmt.Errorf("%w: %w", errRegistryUnavailable, err)
	}
	if res.StatusCode == http.StatusOK {
		return res, nil
	}
	defer func() { _ = res.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 64<<10))
	switch {
	case res.StatusCode == http.StatusNotFound && res.Header.Get(ErrorCodeHeader) == repositoryDisabled:
		// Not the image's: the repository is enabled in the database and not yet on the listener's replica. Asked again without an
		// attempt (Q-VULN-226).
		return nil, fmt.Errorf("%w: %s", errRepositoryDisabled, p.repo.String())
	case res.StatusCode == http.StatusNotFound,
		res.StatusCode == http.StatusForbidden && res.Header.Get(ErrorCodeHeader) == populateNotPermitted:
		// The repository does not hold it: a layer no client brought into a remote's cache, or a manifest that went since the job read
		// its row. Tried again with the back-off (R-VULN-042, Q-VULN-165).
		return nil, &imagescan.ScanError{Code: imagescan.CodeLayerMissing, Detail: "the repository does not hold " + digest}
	case res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden:
		return nil, fmt.Errorf("%w: %d", errPullRefused, res.StatusCode)
	}
	return nil, fmt.Errorf("%w: it answered %d", errRegistryUnavailable, res.StatusCode)
}

// ReadManifest implements [imagescan.BlobReader].
func (p *puller) ReadManifest(ctx context.Context, ref imagescan.ImageRef, limit int64) ([]byte, error) {
	res, err := p.get(ctx, "manifests", ref.DigestString(), manifestAccept)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(res.Body, limit))
	if err != nil {
		return nil, err
	}
	// The manifest is a blob of the call's digest like every other (R-VULN-036): bytes that are not it would analyse another image
	// under this one's name.
	if sum := sha256.Sum256(raw); "sha256:"+hex.EncodeToString(sum[:]) != ref.DigestString() {
		return nil, &imagescan.ScanError{Code: imagescan.CodeDigestMismatch, Detail: "the manifest the scan listener answered does not have the call's digest", Terminal: true}
	}
	return raw, nil
}

// OpenBlob implements [imagescan.BlobReader].
func (p *puller) OpenBlob(ctx context.Context, _ imagescan.ImageRef, digest string) (io.ReadCloser, int64, bool, error) {
	res, err := p.get(ctx, "blobs", digest, "")
	var se *imagescan.ScanError
	if errors.As(err, &se) && se.Code == imagescan.CodeLayerMissing {
		return nil, 0, false, nil
	}
	if err != nil {
		return nil, 0, false, err
	}
	return res.Body, res.ContentLength, true, nil
}
