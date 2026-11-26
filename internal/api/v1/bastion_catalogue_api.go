package v1

import (
	"bytes"
	"net/http"
	"strconv"

	"github.com/guided-traffic/quader/internal/api/v1/gen"
	"github.com/guided-traffic/quader/internal/bastion/cataloguebounds"
	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastionmain"
	"github.com/guided-traffic/quader/internal/bastionmain/catalogue"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/httpserver"
)

// The catalogue routes of the Bastion interface (R-BAS-101 to R-BAS-103). The Bastion token was checked by the recognition stage;
// the shared authorization step admits the routes on that proof alone (the declaration {bastion: true}), so no other credential
// reads them. They answer by the registration's switches: 404 not_found while gui is off, and no documentation path while docs is
// off. What they serve is the catalogue the builder keeps (R-BAS-104 to R-BAS-108, internal/bastionmain/catalogue): a read renders
// nothing, it reads the change list and the compressed documents the builder wrote.

// catalogueSwitches answers the recognized registration and its switches, or writes the refusal and reports false: 503 without the
// interface's services or the catalogue's reader, the configuration's error, and 404 not_found while the web interface is off.
func (s *Server) catalogueSwitches(w http.ResponseWriter, r *http.Request) (bastionmain.Recognition, iface.Switches, bool) {
	b := s.o.Bastion
	rec, ok := bastionmain.RecognitionFrom(r.Context())
	if !ok || b.Feed == nil || b.Catalogue == nil {
		httpserver.Problem(w, r, unavailable("The Bastion interface"))
		return rec, iface.Switches{}, false
	}
	c, err := b.configuration(r, rec)
	if err != nil {
		httpserver.Problem(w, r, err)
		return rec, iface.Switches{}, false
	}
	if !c.Switches.GUI {
		httpserver.Problem(w, r, errs.E(errs.NotFound, errs.CodeNotFound, "The web interface of this Bastion is off: it has no catalogue."))
		return rec, iface.Switches{}, false
	}
	return rec, c.Switches, true
}

// BastionCatalogue implements GET /api/v1/bastion/catalogue (R-BAS-101): the changes after the cursor, at most limit, clamped to
// cataloguebounds.ChangesPageMax; reset for a cursor the catalogue does not continue.
func (s *Server) BastionCatalogue(w http.ResponseWriter, r *http.Request, p gen.BastionCatalogueParams) {
	if p.After < 0 || (p.Limit != nil && *p.Limit < 1) {
		httpserver.Problem(w, r, errs.E(errs.Invalid, errs.CodeInvalid, "after is a seq not below 0 and limit at least 1.", errs.WithBadRequest()))
		return
	}
	rec, _, ok := s.catalogueSwitches(w, r)
	if !ok {
		return
	}
	ans, err := s.o.Bastion.Catalogue.Changes(r.Context(), rec.BastionID, p.After, catalogue.ClampLimit(p.Limit))
	if err != nil {
		httpserver.Problem(w, r, errs.Wrap(err, "api.BastionCatalogue"))
		return
	}
	writeInterface(w, r, http.StatusOK, ans)
}

// BastionCatalogueDocument implements GET /api/v1/bastion/catalogue/document (R-BAS-101, R-BAS-103): one document,
// gzip-compressed, with the SHA-256 of its raw bytes as the ETag. A path outside the grammar answers 400; a documentation path while
// docs is off, and a path the catalogue does not hold, 404 not_found.
func (s *Server) BastionCatalogueDocument(w http.ResponseWriter, r *http.Request, p gen.BastionCatalogueDocumentParams) {
	path, err := iface.ParseCataloguePath(p.Path)
	if err != nil {
		httpserver.Problem(w, r, errs.E(errs.Invalid, errs.CodeInvalid, "path is no document path of the catalogue (R-BAS-103).", errs.WithBadRequest(), errs.WithCause(err)))
		return
	}
	rec, sw, ok := s.catalogueSwitches(w, r)
	if !ok {
		return
	}
	if path.Documentation() && !sw.Docs {
		httpserver.Problem(w, r, errs.E(errs.NotFound, errs.CodeNotFound, "The documentation of this Bastion's web interface is off."))
		return
	}
	docs, err := s.o.Bastion.Catalogue.Documents(r.Context(), rec.BastionID, []string{path.String()})
	if err != nil {
		httpserver.Problem(w, r, errs.Wrap(err, "api.BastionCatalogueDocument"))
		return
	}
	d, found := docs[path.String()]
	if !found {
		httpserver.Problem(w, r, errs.E(errs.NotFound, errs.CodeNotFound, "The catalogue holds no document "+path.String()+"."))
		return
	}
	h := w.Header()
	h.Set("Content-Type", iface.CatalogueDocumentMediaType)
	h.Set("Cache-Control", "no-store")
	h.Set("ETag", `"`+d.SHA256+`"`)
	h.Set(iface.HeaderInterface, strconv.Itoa(iface.InterfaceVersion))
	h.Set("Content-Length", strconv.Itoa(len(d.Gzip)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(d.Gzip)
}

// BastionCatalogueDocuments implements POST /api/v1/bastion/catalogue/documents (R-BAS-101): the batch read, one frame per path in
// the request's order (iface.WriteCatalogueFrame), up to the bound of compressed bytes and at least one frame. A documentation path
// while docs is off, and a path the catalogue does not hold, is a missing frame.
func (s *Server) BastionCatalogueDocuments(w http.ResponseWriter, r *http.Request, _ gen.BastionCatalogueDocumentsParams) {
	body, err := ReadBody(w, r, MediaJSON, int64(cataloguebounds.BatchMaxPaths)*(iface.MaxCataloguePathBytes+8)+(1<<10))
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	var req iface.CatalogueBatch
	if err := StrictUnmarshal(body, &req); err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	if err := req.Validate(cataloguebounds.BatchMaxPaths); err != nil {
		httpserver.Problem(w, r, errs.E(errs.Invalid, errs.CodeInvalid, "paths names 1 to "+strconv.Itoa(cataloguebounds.BatchMaxPaths)+
			" document paths of the catalogue, none twice (R-BAS-103).", errs.WithBadRequest(), errs.WithCause(err)))
		return
	}
	rec, sw, ok := s.catalogueSwitches(w, r)
	if !ok {
		return
	}
	docs, err := s.o.Bastion.Catalogue.Documents(r.Context(), rec.BastionID, req.Paths)
	if err != nil {
		httpserver.Problem(w, r, errs.Wrap(err, "api.BastionCatalogueDocuments"))
		return
	}
	out, err := catalogueFrames(req.Paths, docs, sw.Docs, cataloguebounds.BatchMaxBytes)
	if err != nil {
		httpserver.Problem(w, r, errs.Wrap(err, "api.BastionCatalogueDocuments"))
		return
	}
	h := w.Header()
	h.Set("Content-Type", iface.CatalogueBatchMediaType)
	h.Set("Cache-Control", "no-store")
	h.Set(iface.HeaderInterface, strconv.Itoa(iface.InterfaceVersion))
	h.Set("Content-Length", strconv.Itoa(out.Len()))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(out.Bytes())
}

// catalogueFrames writes the frames of a batch: one per path in order, a missing frame for a path not held or a documentation
// path while docs is off, stopping before the frame that would take the compressed bytes above maxBytes, but never before the
// first.
func catalogueFrames(paths []string, docs map[string]catalogue.Stored, docsOn bool, maxBytes int) (*bytes.Buffer, error) {
	var out bytes.Buffer
	sum := 0
	for i, p := range paths {
		d, found := docs[p]
		if found && !docsOn {
			if path, err := iface.ParseCataloguePath(p); err == nil && path.Documentation() {
				found = false
			}
		}
		if !found {
			if err := iface.WriteCatalogueFrame(&out, iface.CatalogueFrame{Path: p, Missing: true}, nil); err != nil {
				return nil, err
			}
			continue
		}
		if i > 0 && sum+len(d.Gzip) > maxBytes {
			break
		}
		sum += len(d.Gzip)
		if err := iface.WriteCatalogueFrame(&out, iface.CatalogueFrame{Path: p, SHA256: d.SHA256, Size: d.Size}, d.Gzip); err != nil {
			return nil, err
		}
	}
	return &out, nil
}

// BastionCataloguePublic implements GET /api/v1/bastion/catalogue/public (R-BAS-101): the public set of the catalogue's last build,
// empty before the first.
func (s *Server) BastionCataloguePublic(w http.ResponseWriter, r *http.Request, _ gen.BastionCataloguePublicParams) {
	rec, _, ok := s.catalogueSwitches(w, r)
	if !ok {
		return
	}
	pub, err := s.o.Bastion.Catalogue.Public(r.Context(), rec.BastionID)
	if err != nil {
		httpserver.Problem(w, r, errs.Wrap(err, "api.BastionCataloguePublic"))
		return
	}
	writeInterface(w, r, http.StatusOK, pub)
}
