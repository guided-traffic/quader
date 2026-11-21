package scanlisten

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"hash"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/guided-traffic/quader/internal/auth/registrytoken"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/httpserver"
	"github.com/guided-traffic/quader/internal/vuln/imagescan"
)

// The image scanning's database routes of the scan listener (vulnerability-scanning.md R-VULN-049, Q-VULN-206, architecture.md
// R-ARCH-037): the loader of every pod of vuln-scanner-trivy reads the database in use here with the pod's certificate and no token. The
// stage of the listener (internal/auth, scan.go) admits the image scanner's certificate (vulnerabilities.images.registry.client_name) as
// the principal system:vuln-scanner-trivy and nothing else; a handler checks again that the principal is that one.
//
//	GET  /scan/v1/trivy-database                      the database in use: its digest, its layer's size and its build time
//	GET  /scan/v1/trivy-database/sha256:<64 hex>      the layer of that digest, while it is the one in use
//
// A read is no download: it moves no lastDownloadedAt, asks no gate, fills nothing and is recorded nowhere in the usage store, as the
// worker's read of the layer was (R-VULN-044). The previous database is not served: the worker names the one in use in every detection.

// The patterns of the routes, registered on the scan listener's router only.
const (
	TrivyDatabasePattern      = "GET " + registrytoken.TrivyDatabasePath
	TrivyDatabaseLayerPattern = "GET " + registrytoken.TrivyDatabasePath + "/{digest}"
)

// ErrNoTrivyDatabase is a layer the routes do not serve: another digest than the one in use, or one the repository no longer holds.
var ErrNoTrivyDatabase = errors.New("the database's repository does not serve this layer")

// TrivyDatabaseSource is what the routes serve: the source state of the database and the item store of its repository.
type TrivyDatabaseSource interface {
	// InUse names the database in use; an empty digest before the first.
	InUse(ctx context.Context) (imagescan.DatabaseListing, error)
	// Open opens the layer of digest and returns its size; ErrNoTrivyDatabase for a layer the routes do not serve.
	Open(ctx context.Context, digest string) (io.ReadCloser, int64, error)
}

// TrivyOptions configure [RegisterTrivyDatabase].
type TrivyOptions struct {
	Source TrivyDatabaseSource
	Log    *slog.Logger
}

// RegisterTrivyDatabase mounts the two routes on the scan listener's router.
func RegisterTrivyDatabase(mux interface{ Handle(string, http.Handler) }, o TrivyOptions) {
	if o.Log == nil {
		o.Log = slog.Default()
	}
	h := &trivyHandler{o: o}
	mux.Handle(TrivyDatabasePattern, http.HandlerFunc(h.listing))
	mux.Handle(TrivyDatabaseLayerPattern, http.HandlerFunc(h.layer))
}

type trivyHandler struct{ o TrivyOptions }

// imageScanner reports whether the request's principal is the one the routes admit: the system subject vuln-scanner-trivy with the
// client certificate as its credential, which the stage yields for the image scanner's certificate on these routes. A route reached
// otherwise is a defect of the wiring, answered as nothing found.
func imageScanner(p httpserver.Principal) bool {
	return p.Kind == httpserver.KindSystem && p.Name == registrytoken.ScanSubject && p.Credential.Registry == nil
}

func (h *trivyHandler) listing(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !imageScanner(httpserver.PrincipalFrom(ctx)) {
		notFound(w, r)
		return
	}
	l, err := h.o.Source.InUse(ctx)
	if err != nil {
		h.o.Log.ErrorContext(ctx, "the scan listener could not read the image scanner's database in use", "error", err)
		httpserver.Problem(w, r, errs.E(errs.Unavailable, "database_unavailable", "The vulnerability database cannot be read now."))
		return
	}
	body, err := json.Marshal(l)
	if err != nil {
		httpserver.Problem(w, r, errs.Wrap(err, "scanlisten.trivyListing"))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// layer serves the layer of the digest in use, whole, with its Content-Length and its SHA-256 in Repr-Digest; the loader hashes what it
// reads and keeps nothing whose hash is not the digest (R-VULN-049).
func (h *trivyHandler) layer(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	digest := r.PathValue("digest")
	if !imageScanner(httpserver.PrincipalFrom(ctx)) || !imagescan.ValidDigest(digest) {
		notFound(w, r)
		return
	}
	body, size, err := h.o.Source.Open(ctx, digest)
	switch {
	case errors.Is(err, ErrNoTrivyDatabase):
		notFound(w, r)
		return
	case err != nil:
		h.o.Log.ErrorContext(ctx, "the scan listener could not open the image scanner's database", "database", digest, "error", err)
		httpserver.Problem(w, r, errs.E(errs.Unavailable, "database_unavailable", "The vulnerability database cannot be read now."))
		return
	}
	defer func() { _ = body.Close() }()
	raw, _ := hex.DecodeString(strings.TrimPrefix(digest, "sha256:"))
	hd := w.Header()
	hd.Set("Content-Type", "application/octet-stream")
	hd.Set("Content-Length", strconv.FormatInt(size, 10))
	hd.Set("Repr-Digest", "sha-256=:"+base64.StdEncoding.EncodeToString(raw)+":")
	hd.Set("Cache-Control", "no-store")
	// The store's bytes are checked against the digest while they are sent: a body that does not end as the digest aborts the response,
	// so the loader sees a truncated transfer and never a complete wrong one (R-STOR-013).
	h.copy(w, r, &digestReader{r: body, h: sha256.New(), want: raw}, digest)
}

// copy writes a body after the status line; a body that fails in the middle aborts the response.
func (h *trivyHandler) copy(w http.ResponseWriter, r *http.Request, body io.Reader, name string) {
	cw := httpserver.ContentWriterFor(w, r, httpserver.ContentFile, name)
	cw.WriteHeader(http.StatusOK)
	if _, err := io.Copy(cw, body); err != nil {
		if r.Context().Err() != nil {
			return
		}
		h.o.Log.WarnContext(r.Context(), "a read of the image scanner's database ended in the middle of its body", "path", r.URL.Path, "error", err)
		panic(http.ErrAbortHandler)
	}
}

// digestReader hashes what it reads and fails at the end when the bytes are not the digest.
type digestReader struct {
	r    io.Reader
	h    hash.Hash
	want []byte
}

func (d *digestReader) Read(p []byte) (int, error) {
	n, err := d.r.Read(p)
	d.h.Write(p[:n])
	if errors.Is(err, io.EOF) && !strings.EqualFold(hex.EncodeToString(d.h.Sum(nil)), hex.EncodeToString(d.want)) {
		return n, errors.New("the stored layer is not its digest")
	}
	return n, err
}
