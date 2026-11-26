package v1

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"unicode/utf8"

	"github.com/guided-traffic/quader/internal/api/v1/gen"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/httpserver"
	"github.com/guided-traffic/quader/internal/packages/view"
)

// The files a version's package embeds, for the screens (formats/nuget.md R-NUGET-038): NuGet's icon and licence file, read out of the
// stored package through the format's ViewEmbedder. Both are untrusted content of an uploaded or fetched package: an icon is served
// only as a PNG or a JPEG by its bytes, a licence only as UTF-8 text, each with the headers of a response the browser shows as that
// and nothing else. The cache policy keeps the answer to the reader and makes the browser ask again, so that the read check decides
// every view (one reader's permission never serves another, R-AUTHZ-034's reasoning for the artifacts level).

// embeddedCache is how a browser keeps an embedded file: for this reader only, and asked for again each time it is shown.
const embeddedCache = "private, no-cache"

// GetPackageVersionIcon implements GET /api/v1/packages/{packageId}/versions/{version}/icon (R-NUGET-038).
func (s *Server) GetPackageVersionIcon(w http.ResponseWriter, r *http.Request, id gen.PackageId, version gen.PackageVersionPath, _ gen.GetPackageVersionIconParams) {
	s.embedded(w, r, id, version, formats.EmbeddedIcon, "The version has no icon.", iconType)
}

// GetPackageVersionLicense implements GET /api/v1/packages/{packageId}/versions/{version}/license (R-NUGET-038).
func (s *Server) GetPackageVersionLicense(w http.ResponseWriter, r *http.Request, id gen.PackageId, version gen.PackageVersionPath, _ gen.GetPackageVersionLicenseParams) {
	s.embedded(w, r, id, version, formats.EmbeddedLicense, "The version has no licence file.", licenseType)
}

// iconType is the media type of an icon by its bytes: PNG or JPEG, "" for anything else (an SVG, which can carry script, first).
func iconType(b []byte) string {
	switch {
	case bytes.HasPrefix(b, []byte("\x89PNG\r\n\x1a\n")):
		return "image/png"
	case bytes.HasPrefix(b, []byte{0xFF, 0xD8, 0xFF}):
		return "image/jpeg"
	}
	return ""
}

// licenseType is the media type of a licence file: text when it is UTF-8 without a NUL, "" otherwise.
func licenseType(b []byte) string {
	if !utf8.Valid(b) || bytes.IndexByte(b, 0) >= 0 {
		return ""
	}
	return "text/plain; charset=utf-8"
}

// embedded answers one embedded file of a visible version. One answer for a version that is not visible, one that embeds no such
// file and one whose file is not of the type served: the difference would tell a caller that a hidden version exists.
func (s *Server) embedded(w http.ResponseWriter, r *http.Request, id gen.PackageId, version, file, none string, typeOf func([]byte) string) {
	ps, err := s.packageServices()
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	missing := errs.E(errs.NotFound, errs.CodeNotFound, none)
	format, name, ok := view.ParseID(id)
	if !ok {
		httpserver.Problem(w, r, missing)
		return
	}
	ctx, sc, err := s.packageScope(r, ps, view.ScopeRequest{Formats: []string{format}, ExactName: name})
	var b []byte
	found := false
	if err == nil {
		b, found, err = ps.View.EmbeddedOf(ctx, sc, format, name, version, file)
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	contentType := ""
	if found {
		contentType = typeOf(b)
	}
	if contentType == "" {
		httpserver.Problem(w, r, missing)
		return
	}
	sum := sha256.Sum256(b)
	tag := `"` + hex.EncodeToString(sum[:]) + `"`
	h := w.Header()
	h.Set("ETag", tag)
	h.Set("Cache-Control", embeddedCache)
	if IfNoneMatchHits(r, tag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	// Not a JSON answer: the headers of R-API-017 give way to those of a response the browser shows as a picture or as text and
	// nothing else.
	h.Set("Content-Type", contentType)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	h.Set("Content-Disposition", "inline; filename=\""+file+"\"")
	h.Set("Cross-Origin-Resource-Policy", "same-origin")
	h.Set("Content-Length", strconv.Itoa(len(b)))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(b) //nolint:gosec // G705: a PNG or JPEG by its bytes, or UTF-8 text, each with nosniff and a sandboxing CSP
	}
}
