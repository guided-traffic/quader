package mainclient

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"github.com/guided-traffic/quader/internal/bastion/cataloguebounds"
	"github.com/guided-traffic/quader/internal/bastion/iface"
)

// The public catalogue's reads (W-35, R-BAS-101, R-BAS-109): the change list, the public set and the batch read of documents.
// They take no slot of the budget, like the handshake and the feed: they are the Bastion's own background reads and never a
// caller's miss. Every body of them is read through the throughput's catalogue class (R-BAS-097, Q-BAS-458), below the callers'
// misses; nothing else reads a catalogue body.

// KindCatalogue is the kind of the catalogue's reads in quader_bastion_main_requests_total.
const KindCatalogue Kind = "catalogue"

// CataloguePacer paces a read of the public catalogue: throughput.Pacer implements it. A [Throughput] that does not leaves the
// catalogue's reads unpaced.
type CataloguePacer interface {
	CatalogueReader(ctx context.Context, body io.ReadCloser) PacedBody
}

// CatalogueSource is what the catalogue's sync reads of the main installation; [HTTPClient] implements it.
type CatalogueSource interface {
	// CatalogueChanges reads one page of the change list after the cursor; limit 0 asks for the main installation's page. The
	// answer is validated against after (iface.CatalogueChanges.Validate).
	CatalogueChanges(ctx context.Context, after int64, limit int) (iface.CatalogueChanges, error)
	// CataloguePublic reads the public set.
	CataloguePublic(ctx context.Context) (iface.CataloguePublic, error)
	// CatalogueDocuments reads the documents of paths in one batch and calls fn for each frame it received, in order, with the
	// compressed bytes of a document that is not missing. The answer may stop before the last path (the main installation's
	// bound); the caller asks again for the paths fn was not called for. An error of fn ends the read with that error.
	CatalogueDocuments(ctx context.Context, paths []string, fn func(f iface.CatalogueFrame, gz []byte) error) error
}

var _ CatalogueSource = (*HTTPClient)(nil)

// maxFrameGzip bounds the compressed bytes of one frame: a document is at most cataloguebounds.DocumentMaxBytes raw, and gzip
// adds a few bytes per 16 KiB block to bytes it cannot compress.
const maxFrameGzip = cataloguebounds.DocumentMaxBytes + cataloguebounds.DocumentMaxBytes/1024 + 1024

// CatalogueChanges implements [CatalogueSource].
func (c *HTTPClient) CatalogueChanges(ctx context.Context, after int64, limit int) (iface.CatalogueChanges, error) {
	q := url.Values{iface.QueryAfter: {strconv.FormatInt(after, 10)}}
	if limit > 0 {
		q.Set(iface.QueryLimit, strconv.Itoa(limit))
	}
	var ans iface.CatalogueChanges
	if err := c.interfaceCall(ctx, ifaceCall{kind: KindCatalogue, method: http.MethodGet, path: iface.PathCatalogue, query: q, out: &ans, catalogue: true}); err != nil {
		return iface.CatalogueChanges{}, err
	}
	if err := ans.Validate(after); err != nil {
		return iface.CatalogueChanges{}, &AnswerError{Status: http.StatusOK, Err: err}
	}
	return ans, nil
}

// CataloguePublic implements [CatalogueSource].
func (c *HTTPClient) CataloguePublic(ctx context.Context) (iface.CataloguePublic, error) {
	var ans iface.CataloguePublic
	if err := c.interfaceCall(ctx, ifaceCall{kind: KindCatalogue, method: http.MethodGet, path: iface.PathCataloguePublic, out: &ans, catalogue: true}); err != nil {
		return iface.CataloguePublic{}, err
	}
	return ans, nil
}

// CatalogueDocuments implements [CatalogueSource]. The answer streams: each frame is handed to fn as it arrives, so a batch of
// up to cataloguebounds.BatchMaxBytes is never held whole.
func (c *HTTPClient) CatalogueDocuments(ctx context.Context, paths []string, fn func(iface.CatalogueFrame, []byte) error) error {
	call := ifaceCall{kind: KindCatalogue, method: http.MethodPost, path: iface.PathCatalogueDocuments, in: iface.CatalogueBatch{Paths: paths}, catalogue: true}
	call.read = func(resp *http.Response) error {
		if ct := resp.Header.Get("Content-Type"); ct != iface.CatalogueBatchMediaType {
			return &AnswerError{Status: resp.StatusCode, Err: fmt.Errorf("the batch answered %q", ct)}
		}
		r := bufio.NewReaderSize(resp.Body, iface.MaxCatalogueFrameHeaderBytes+1)
		for i := 0; ; i++ {
			f, gz, err := iface.ReadCatalogueFrame(r, maxFrameGzip)
			switch {
			case errors.Is(err, io.EOF):
				return nil
			case err != nil:
				return &AnswerError{Status: resp.StatusCode, Err: err}
			case i >= len(paths) || f.Path != paths[i]:
				return &AnswerError{Status: resp.StatusCode, Err: fmt.Errorf("frame %d names %q, not the path asked in its place", i, f.Path)}
			}
			if err := fn(f, gz); err != nil {
				return err
			}
		}
	}
	return c.interfaceCall(ctx, call)
}

// pacedCatalogue reads a catalogue body through the throughput's catalogue class (R-BAS-097).
func (c *HTTPClient) pacedCatalogue(ctx context.Context, body io.ReadCloser) io.ReadCloser {
	if p, ok := c.o.Throughput.(CataloguePacer); ok && p != nil {
		return p.CatalogueReader(ctx, body)
	}
	return body
}

// readCatalogue decodes a catalogue answer of the interface: the version check, a 2xx with the call's reader or its JSON, and the
// problems of the other answers as decode does.
func (c *HTTPClient) readCatalogue(resp *http.Response, call ifaceCall) error {
	if resp.StatusCode != http.StatusOK || call.read == nil {
		return c.decode(resp, call)
	}
	if err := checkVersion(resp.Header); err != nil {
		return err
	}
	c.o.Metrics.MainTokenRefused.Set(0)
	return call.read(resp)
}
