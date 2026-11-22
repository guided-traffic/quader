package blob

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// The link check of a connection (storage.md R-STOR-074, Q-STOR-294, Q-STOR-346): a probe object below <prefix>/_probe/, a link
// to it signed with the three overrides of R-STOR-072 against the connection's own endpoint, fetched with the HTTP client the
// store is reached with. It records its two parts apart, because a handover to a Bastion needs the first only (R-STOR-096): the
// link works (200 with the probe's bytes), and the store's answer honours the three overrides.

// The overrides the link check asks for. The type is one no store would set on its own, so a store that ignores the override
// answers with another.
const (
	linkProbeContentType = "text/x-quader-link-check; charset=utf-8"
	linkProbeDisposition = "attachment; filename*=UTF-8''quader-link-check.txt"
	linkProbeCache       = "private, no-store"
)

// linkProbeLife is the life of the probe's link: the check fetches it at once.
const linkProbeLife = time.Minute

// Fetcher fetches a signed link: the HTTP client a store is reached with (*http.Client), or a store that answers its own links (the
// in-memory store of blobtest).
type Fetcher interface {
	Do(req *http.Request) (*http.Response, error)
}

// FetcherOf is the fetcher of a store's links: what its Fetcher method gives (a [Guard] forwards its store's), the HTTP client of an
// S3 adapter, or the store itself when it answers its links; nil for none.
func FetcherOf(s ObjectStore) Fetcher {
	switch f := s.(type) {
	case interface{ Fetcher() Fetcher }:
		return f.Fetcher()
	case interface{ HTTPClient() *http.Client }:
		if hc := f.HTTPClient(); hc != nil {
			return hc
		}
		return nil
	case Fetcher:
		return f
	}
	return nil
}

// LinkCheck is the result of a connection's link check on one replica.
type LinkCheck struct {
	// Ran is true once a check answered; false before, and while the store could not be asked (a transient failure, an open
	// circuit), which counts as not passed.
	Ran bool
	// Works: the probe's link answered 200 with the probe's bytes (the first part, all a handover needs).
	Works bool
	// Overrides: that answer carried the three overridden headers (the second part, which a client's link needs as well).
	Overrides bool
	// Detail says what the store answered when a part failed, for the settings' reason link_check_failed; never the link.
	Detail string
	// At is when the check ran.
	At time.Time
}

// Passed reports whether both parts passed: the connection's links may go to clients (R-STOR-074).
func (r LinkCheck) Passed() bool { return r.Ran && r.Works && r.Overrides }

// CheckLink runs the link check against a store: it writes a probe object named after replica below the prefix's _probe/,
// signs a link to it with the three overrides against the store's own endpoint, fetches it with f, and deletes the probe. A
// failure to write or sign the probe, or no fetcher, leaves Ran false; what the link's answer shows sets the two parts.
func CheckLink(ctx context.Context, s ObjectStore, f Fetcher, prefix, replica string) LinkCheck {
	e := &ConformanceEnv{Store: s, Opts: ConformanceOptions{Prefix: prefix, Replica: replica}}
	defer e.cleanup(ctx)
	r, _ := linkCheck(ctx, e, f)
	return r
}

// linkCheck is the check's body, shared with the conformance check: the result, and an error that says why a part failed.
func linkCheck(ctx context.Context, e *ConformanceEnv, f Fetcher) (LinkCheck, error) {
	r := LinkCheck{At: time.Now()}
	if f == nil {
		return r, errors.New("no HTTP client was given to fetch the presigned URL")
	}
	key := e.Key("link")
	data := payload(2048)
	if err := putBytes(ctx, e.Store, key, data); err != nil {
		return r, fmt.Errorf("write the probe: %w", err)
	}
	link, err := e.Store.Presign(ctx, key, PresignOptions{
		Life: linkProbeLife, ContentType: linkProbeContentType, ContentDisposition: linkProbeDisposition, CacheControl: linkProbeCache,
	})
	if err != nil {
		return r, fmt.Errorf("sign the probe's link: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link.URL, nil)
	if err != nil {
		return r, err
	}
	resp, err := f.Do(req)
	if err != nil {
		r.Ran, r.Detail = true, "the link was not answered: "+redactURLError(err)
		return r, errors.New(r.Detail)
	}
	defer resp.Body.Close()
	got, err := io.ReadAll(io.LimitReader(resp.Body, int64(len(data))+1))
	r.Ran = true
	if err != nil || resp.StatusCode != http.StatusOK || !bytes.Equal(got, data) {
		r.Detail = fmt.Sprintf("the link answered %d with %d bytes, want 200 with the probe's %d", resp.StatusCode, len(got), len(data))
		return r, errors.New(r.Detail)
	}
	r.Works = true
	for _, h := range []struct{ name, want string }{
		{"Content-Type", linkProbeContentType}, {"Content-Disposition", linkProbeDisposition}, {"Cache-Control", linkProbeCache},
	} {
		if got := resp.Header.Get(h.name); got != h.want {
			r.Detail = fmt.Sprintf("the link's answer carries %s %q, want %q: the store does not honour response-%s", h.name, got, h.want, lowerHeader(h.name))
			return r, errors.New(r.Detail)
		}
	}
	r.Overrides = true
	return r, nil
}

// lowerHeader is a header's name in lower case, the spelling of its response- query parameter.
func lowerHeader(name string) string { return string(bytes.ToLower([]byte(name))) }

// redactURLError is the text of a client error without the URL it names: a link is a bearer for its life and goes into no
// error and no log (R-STOR-072).
func redactURLError(err error) string {
	var ue *url.Error
	if errors.As(err, &ue) && ue.Err != nil {
		return ue.Err.Error()
	}
	return "the request failed"
}
