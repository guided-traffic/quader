// Package scanlisten is the malware scanning's part of the scan listener of packages (malware-scanning.md R-MAL-006, architecture.md
// R-ARCH-037): the route the malware scanner reads one blob through with a blob token, and the routes it loads the signature database
// through with its certificate. The stage of the scan listener (internal/auth, scan.go) has admitted the request before a handler
// runs; a handler checks again that the principal is the one the route admits, and decides the read itself (Q-AUTHZ-340) through
// internal/malware/scanread, which holds the route's statement and the source of the database.
//
//	GET, HEAD  /scan/v1/blobs/sha256:<64 hex>                 the blob, while the token's repository holds it
//	GET        /scan/v1/malware-database                      the version in use and its files
//	GET        /scan/v1/malware-database/<version>/<file>     one file of a version the database row names
//
// No route fills a remote repository, asks an upstream, asks a gate (neither the vulnerability block nor the malware gate), counts a
// download or moves lastDownloadedAt: they read the blob store and the database, never a format's serve path. The blob route's bytes
// are recorded in the usage store by the chain's stage 7, under the subject system and the token's repository (Q-LIM-80).
package scanlisten

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/guided-traffic/quader/internal/auth/registrytoken"
	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/httpserver"
	"github.com/guided-traffic/quader/internal/limits"
	"github.com/guided-traffic/quader/internal/malware"
	"github.com/guided-traffic/quader/internal/malware/scanread"
)

// The patterns of the routes, registered on the scan listener's router only.
const (
	BlobPattern         = "GET " + registrytoken.BlobPathPrefix + "{digest}"
	DatabasePattern     = "GET " + registrytoken.DatabasePath
	DatabaseFilePattern = "GET " + registrytoken.DatabasePath + "/{version}/{file}"
)

// Database is the listing of the database route (R-MAL-006): the version in use, "" before the first one, and its files.
type Database struct {
	Version string                 `json:"version"`
	Files   []malware.DatabaseFile `json:"files"`
}

// Options configure [Register].
type Options struct {
	Holdings scanread.Holdings
	Blobs    scanread.BlobOpener
	// Database is what the database routes serve; nil answers them 503 database_unavailable.
	Database malware.DatabaseSource
	Log      *slog.Logger
}

// Register mounts the three routes on the scan listener's router.
func Register(mux interface{ Handle(string, http.Handler) }, o Options) {
	if o.Log == nil {
		o.Log = slog.New(slog.DiscardHandler)
	}
	h := &handler{o: o}
	mux.Handle(BlobPattern, http.HandlerFunc(h.blob))
	mux.Handle(DatabasePattern, http.HandlerFunc(h.listing))
	mux.Handle(DatabaseFilePattern, http.HandlerFunc(h.file))
}

type handler struct{ o Options }

// notFound is the answer for a blob or a file the route does not serve; it names nothing of why.
func notFound(w http.ResponseWriter, r *http.Request) {
	httpserver.Problem(w, r, errs.E(errs.NotFound, errs.CodeNotFound, "Not found."))
}

// malwareScanner reports whether the request's principal is the one the malware scanning's routes admit: the system subject
// malware-scanner-clamav, which the stage yields for a blob token on the blob route and for the malware scanner's certificate on the database
// routes. A route reached otherwise is a defect of the wiring, answered as nothing found.
func malwareScanner(p httpserver.Principal) bool {
	return p.Kind == httpserver.KindSystem && p.Name == registrytoken.BlobSubject
}

// databaseReader reports whether the request's principal may read the database routes: the malware scanner, or the updater for its
// seed (registrytoken.UpdaterSubject, R-MAL-032), which the stage yields for their certificates.
func databaseReader(p httpserver.Principal) bool {
	return malwareScanner(p) || (p.Kind == httpserver.KindSystem && p.Name == registrytoken.UpdaterSubject)
}

// blob serves one blob to the malware scanner (R-MAL-006): the token's digest, read through the token's repository while it holds the
// blob, whole (no Range), with Content-Length, Repr-Digest and application/octet-stream.
func (h *handler) blob(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p := httpserver.PrincipalFrom(ctx)
	b := p.Credential.ScanBlob
	digest := r.PathValue("digest")
	if !malwareScanner(p) || b == nil || b.Digest != digest {
		notFound(w, r)
		return
	}
	hexPart, _ := strings.CutPrefix(digest, "sha256:")
	d, ok := blob.ParseDigest(hexPart)
	if !ok {
		notFound(w, r)
		return
	}
	held, ok, err := h.o.Holdings.Held(ctx, b.Repo, d)
	if err != nil {
		h.o.Log.ErrorContext(ctx, "the scan listener could not read whether a repository holds a blob", "repository", b.Repo.String(),
			"digest", digest, "scan_id", p.Credential.ScanID, "error", err)
		httpserver.Problem(w, r, errs.E(errs.Unavailable, errs.CodeUnavailable, "The blob cannot be read now."))
		return
	}
	if !ok {
		notFound(w, r)
		return
	}
	size := held.Size
	// The usage store records the read under the token's repository, as the format host records a download under the repository that
	// served it (Q-LIM-80); the subject is system and nothing limits it (Q-LIM-66).
	limits.FromContext(ctx).Served(held.RepoID)
	hd := w.Header()
	hd.Set("Content-Type", "application/octet-stream")
	hd.Set("Content-Length", strconv.FormatInt(size, 10))
	hd.Set("Repr-Digest", "sha-256=:"+base64.StdEncoding.EncodeToString(d[:])+":")
	hd.Set("Accept-Ranges", "none")
	hd.Set("Cache-Control", "no-store")
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	body, err := h.o.Blobs.Open(ctx, d, size, blob.OpenOptions{})
	if err != nil {
		for _, k := range []string{"Content-Type", "Content-Length", "Repr-Digest", "Accept-Ranges"} {
			hd.Del(k)
		}
		if errors.Is(err, blob.ErrNotFound) {
			notFound(w, r) // the object is missing: the blob store marked it (R-STOR-027)
			return
		}
		h.o.Log.ErrorContext(ctx, "the scan listener could not open a blob", "digest", digest, "scan_id", p.Credential.ScanID, "error", err)
		httpserver.Problem(w, r, errs.E(errs.Unavailable, errs.CodeUnavailable, "The blob cannot be read now."))
		return
	}
	defer func() { _ = body.Close() }()
	h.copy(w, r, body, digest)
}

// copy writes a body after the status line. A body that fails in the middle aborts the response, so the reader sees a truncated
// transfer and never a complete wrong one (R-STOR-013); the malware scanner hashes what it reads and ends the scan digest_mismatch or
// registry_unavailable.
func (h *handler) copy(w http.ResponseWriter, r *http.Request, body io.Reader, name string) {
	cw := httpserver.ContentWriterFor(w, r, httpserver.ContentFile, name)
	cw.WriteHeader(http.StatusOK)
	if _, err := io.Copy(cw, body); err != nil {
		if r.Context().Err() != nil {
			return // the reader went away
		}
		h.o.Log.WarnContext(r.Context(), "a read of the scan listener ended in the middle of its body", "path", r.URL.Path, "error", err)
		panic(http.ErrAbortHandler)
	}
}

// listing answers the version in use and its files (R-MAL-006, Q-MAL-27).
func (h *handler) listing(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !databaseReader(httpserver.PrincipalFrom(ctx)) {
		notFound(w, r)
		return
	}
	if h.o.Database == nil {
		httpserver.Problem(w, r, errs.E(errs.Unavailable, "database_unavailable", "This replica serves no signature database."))
		return
	}
	v, err := h.o.Database.InUse(ctx)
	if err != nil {
		h.o.Log.ErrorContext(ctx, "the scan listener could not read the signature database in use", "error", err)
		httpserver.Problem(w, r, errs.E(errs.Unavailable, "database_unavailable", "The signature database cannot be read now."))
		return
	}
	out := Database{Version: v.Version, Files: v.Files}
	if out.Files == nil {
		out.Files = []malware.DatabaseFile{}
	}
	body, err := json.Marshal(out)
	if err != nil {
		httpserver.Problem(w, r, errs.Wrap(err, "scanlisten.listing"))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// file serves one file of a version the database row names (R-MAL-006), with its SHA-256 in Repr-Digest and its size, which the
// scanner checks the bytes against (R-MAL-007).
func (h *handler) file(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !databaseReader(httpserver.PrincipalFrom(ctx)) {
		notFound(w, r)
		return
	}
	if h.o.Database == nil {
		httpserver.Problem(w, r, errs.E(errs.Unavailable, "database_unavailable", "This replica serves no signature database."))
		return
	}
	f, body, err := h.o.Database.Open(ctx, r.PathValue("version"), r.PathValue("file"))
	switch {
	case errors.Is(err, malware.ErrNoDatabaseFile):
		notFound(w, r)
		return
	case err != nil:
		h.o.Log.ErrorContext(ctx, "the scan listener could not open a file of the signature database", "version", r.PathValue("version"),
			"file", r.PathValue("file"), "error", err)
		httpserver.Problem(w, r, errs.E(errs.Unavailable, "database_unavailable", "The signature database cannot be read now."))
		return
	}
	defer func() { _ = body.Close() }()
	hd := w.Header()
	hd.Set("Content-Type", "application/octet-stream")
	hd.Set("Content-Length", strconv.FormatInt(f.Size, 10))
	if raw, ok := blob.ParseDigest(f.SHA256); ok {
		hd.Set("Repr-Digest", "sha-256=:"+base64.StdEncoding.EncodeToString(raw[:])+":")
	}
	hd.Set("Cache-Control", "no-store")
	h.copy(w, r, body, f.Name)
}
