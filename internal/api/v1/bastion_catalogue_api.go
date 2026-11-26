package v1

import (
	"bytes"
	"net/http"
	"strconv"

	"github.com/guided-traffic/quader/internal/api/v1/gen"
	"github.com/guided-traffic/quader/internal/bastion/cataloguebounds"
	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastionmain"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/httpserver"
)

// The catalogue routes of the Bastion interface (R-BAS-101 to R-BAS-103, W-35's package P5). The Bastion token was checked by
// the recognition stage; the shared authorization step admits the routes on that proof alone (the declaration {bastion: true}),
// so no other credential reads them. They answer by the registration's switches: 404 not_found while gui is off, and no
// documentation path while docs is off. What they serve is the catalogue W-35's package P6 builds; until it exists the change
// list and the public set are empty and every document is missing.

// catalogueSwitches answers the recognized registration's switches, or writes the refusal and reports false: 503 without the
// interface's services, the configuration's error, and 404 not_found while the web interface is off.
func (s *Server) catalogueSwitches(w http.ResponseWriter, r *http.Request) (iface.Switches, bool) {
	b := s.o.Bastion
	rec, ok := bastionmain.RecognitionFrom(r.Context())
	if !ok || b.Feed == nil {
		httpserver.Problem(w, r, unavailable("The Bastion interface"))
		return iface.Switches{}, false
	}
	c, err := b.configuration(r, rec)
	if err != nil {
		httpserver.Problem(w, r, err)
		return iface.Switches{}, false
	}
	if !c.Switches.GUI {
		httpserver.Problem(w, r, errs.E(errs.NotFound, errs.CodeNotFound, "The web interface of this Bastion is off: it has no catalogue."))
		return iface.Switches{}, false
	}
	return c.Switches, true
}

// BastionCatalogue implements GET /api/v1/bastion/catalogue (R-BAS-101): the changes after the cursor, which the builder (P6)
// answers at most limit of, clamped to cataloguebounds.ChangesPageMax. Until it is built the list is empty, the cursor the request's.
func (s *Server) BastionCatalogue(w http.ResponseWriter, r *http.Request, p gen.BastionCatalogueParams) {
	if p.After < 0 || (p.Limit != nil && *p.Limit < 1) {
		httpserver.Problem(w, r, errs.E(errs.Invalid, errs.CodeInvalid, "after is a seq not below 0 and limit at least 1.", errs.WithBadRequest()))
		return
	}
	if _, ok := s.catalogueSwitches(w, r); !ok {
		return
	}
	writeInterface(w, r, http.StatusOK, iface.CatalogueChanges{Cursor: p.After})
}

// BastionCatalogueDocument implements GET /api/v1/bastion/catalogue/document (R-BAS-101, R-BAS-103): one document,
// gzip-compressed. A path outside the grammar answers 400; a documentation path while docs is off, and until the catalogue is
// built every document, 404 not_found.
func (s *Server) BastionCatalogueDocument(w http.ResponseWriter, r *http.Request, p gen.BastionCatalogueDocumentParams) {
	path, err := iface.ParseCataloguePath(p.Path)
	if err != nil {
		httpserver.Problem(w, r, errs.E(errs.Invalid, errs.CodeInvalid, "path is no document path of the catalogue (R-BAS-103).", errs.WithBadRequest(), errs.WithCause(err)))
		return
	}
	sw, ok := s.catalogueSwitches(w, r)
	if !ok {
		return
	}
	if path.Documentation() && !sw.Docs {
		httpserver.Problem(w, r, errs.E(errs.NotFound, errs.CodeNotFound, "The documentation of this Bastion's web interface is off."))
		return
	}
	httpserver.Problem(w, r, errs.E(errs.NotFound, errs.CodeNotFound, "The catalogue holds no document "+path.String()+"."))
}

// BastionCatalogueDocuments implements POST /api/v1/bastion/catalogue/documents (R-BAS-101): the batch read, one frame per
// path in the request's order (iface.WriteCatalogueFrame). A documentation path while docs is off, and until the catalogue is
// built every path, is a missing frame.
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
	if _, ok := s.catalogueSwitches(w, r); !ok {
		return
	}
	var out bytes.Buffer
	for _, p := range req.Paths {
		// No catalogue holds a document yet; the builder (P6) answers a documentation path missing while docs is off.
		if err := iface.WriteCatalogueFrame(&out, iface.CatalogueFrame{Path: p, Missing: true}, nil); err != nil {
			httpserver.Problem(w, r, errs.Wrap(err, "api.BastionCatalogueDocuments"))
			return
		}
	}
	h := w.Header()
	h.Set("Content-Type", iface.CatalogueBatchMediaType)
	h.Set("Cache-Control", "no-store")
	h.Set(iface.HeaderInterface, strconv.Itoa(iface.InterfaceVersion))
	h.Set("Content-Length", strconv.Itoa(out.Len()))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(out.Bytes())
}

// BastionCataloguePublic implements GET /api/v1/bastion/catalogue/public (R-BAS-101): the public set, empty until the catalogue
// is built.
func (s *Server) BastionCataloguePublic(w http.ResponseWriter, r *http.Request, _ gen.BastionCataloguePublicParams) {
	if _, ok := s.catalogueSwitches(w, r); !ok {
		return
	}
	writeInterface(w, r, http.StatusOK, iface.CataloguePublic{})
}
