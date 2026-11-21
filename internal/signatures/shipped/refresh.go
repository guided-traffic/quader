package shipped

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/tuf"
	"github.com/theupdateframework/go-tuf/v2/metadata"
	"github.com/theupdateframework/go-tuf/v2/metadata/config"
	"github.com/theupdateframework/go-tuf/v2/metadata/updater"
)

// DefaultURL is the public Sigstore TUF repository, the default of signatures.trust_root.url (R-SIG-040).
const DefaultURL = tuf.DefaultMirror

// refresh runs the TUF client workflow from the root sigstore-go embeds (or o.Root) against o.URL: the root chain, the timestamp, the
// snapshot, the targets and the delegated role of registry.npmjs.org, each verified by its signatures, versions and expiry at o.Now; then
// it downloads the two targets and verifies their lengths and hashes against the targets metadata. sigstore-go's TUF client is a thin
// wrapper of the same go-tuf updater, but it reads the wall clock and offers no reference time, so the updater is driven here directly
// with sigstore-go's embedded root. Nothing is written to disk. The version returned is the snapshot's: npm's keys are a target of the
// delegated role registry.npmjs.org, whose change raises the snapshot's version and leaves the top-level targets' as it was.
func refresh(ctx context.Context, o RefreshOptions) (Refreshed, error) {
	if o.Client == nil {
		return Refreshed{}, errors.New("shipped: refresh needs an HTTP client")
	}
	base, trustRoot := o.URL, o.Root
	if base == "" {
		base = DefaultURL
	}
	if trustRoot == nil {
		trustRoot = tuf.DefaultRoot()
	}
	cfg, err := config.New(base, trustRoot)
	if err != nil {
		return Refreshed{}, fmt.Errorf("shipped: TUF client of %s: %w", base, err)
	}
	cfg.DisableLocalCache = true
	cfg.PrefixTargetsWithHash = true
	cfg.Fetcher = httpFetcher{ctx: ctx, client: o.Client}
	up, err := updater.New(cfg)
	if err != nil {
		return Refreshed{}, fmt.Errorf("shipped: TUF client of %s: %w", base, err)
	}
	if !o.Now.IsZero() {
		up.UnsafeSetRefTime(o.Now.UTC())
	}
	if err := up.Refresh(); err != nil {
		return Refreshed{}, fmt.Errorf("shipped: TUF metadata of %s: %w", base, err)
	}
	var out Refreshed
	if out.TrustedRoot, err = target(up, TargetTrustedRoot); err != nil {
		return Refreshed{}, err
	}
	if out.NPMKeys, err = target(up, TargetNPMKeys); err != nil {
		return Refreshed{}, err
	}
	if err := usable(out); err != nil {
		return Refreshed{}, err
	}
	if snap := up.GetTrustedMetadataSet().Snapshot; snap != nil {
		out.TUFVersion = snap.Signed.Version
	}
	return out, nil
}

// target downloads one target and verifies it against the trusted targets metadata.
func target(up *updater.Updater, name string) ([]byte, error) {
	info, err := up.GetTargetInfo(name)
	if err != nil {
		return nil, fmt.Errorf("shipped: TUF target %s: %w", name, err)
	}
	_, b, err := up.DownloadTarget(info, "", "")
	if err != nil {
		return nil, fmt.Errorf("shipped: TUF target %s: %w", name, err)
	}
	return b, nil
}

// usable refuses targets that are signed but cannot serve: a trusted root sigstore-go does not read, keys npm's shape does not hold.
func usable(r Refreshed) error {
	if _, err := root.NewTrustedRootFromJSON(r.TrustedRoot); err != nil {
		return fmt.Errorf("shipped: TUF target %s does not parse: %w", TargetTrustedRoot, err)
	}
	if _, err := npmKeys(r.NPMKeys); err != nil {
		return fmt.Errorf("shipped: TUF target %s: %w", TargetNPMKeys, err)
	}
	return nil
}

// httpFetcher fetches TUF metadata and targets through the given client (the outbound client and its egress rules), within ctx, at most
// the length the updater allows. A status other than 200 is go-tuf's ErrDownloadHTTP, by which the updater ends the root chain at a 404.
type httpFetcher struct {
	ctx    context.Context
	client *http.Client
}

// DownloadFile implements go-tuf's fetcher.Fetcher.
func (f httpFetcher) DownloadFile(urlPath string, maxLength int64, _ time.Duration) ([]byte, error) {
	req, err := http.NewRequestWithContext(f.ctx, http.MethodGet, urlPath, nil)
	if err != nil {
		return nil, fmt.Errorf("request %s: %w", urlPath, err)
	}
	res, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", urlPath, err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, &metadata.ErrDownloadHTTP{StatusCode: res.StatusCode, URL: urlPath}
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, maxLength+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", urlPath, err)
	}
	if int64(len(b)) > maxLength {
		return nil, &metadata.ErrDownloadLengthMismatch{Msg: fmt.Sprintf("%s is longer than %d bytes", urlPath, maxLength)}
	}
	return b, nil
}
