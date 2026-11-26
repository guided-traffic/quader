package dispatch

import (
	"net/http"
	"strings"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/httpserver"
	"github.com/guided-traffic/quader/internal/qname"
	"github.com/guided-traffic/quader/internal/repository"
)

// Handoff serves a request of the management API by a route of a format, as if the request had reached the route below
// /api/<format>/<project>/<key>/ (R-ANS-061, Q-ANS-140): the screens' upload of an Ansible Galaxy collection runs the handler of the
// client's publish with the session's principal. method and rest are the method and the escaped path below the repository that the
// format's own Match reads; the request's body, headers and query reach the handler unchanged. Every check of the protocol path runs:
// the coarse decision of the route's action, the read-only maintenance mode, the repository's lifecycle, state and format, a write to a
// virtual repository handed to its deployment target, and then the format's own checks in its handler.
//
// What the handler writes on success reaches w; an error, whether a stage of the dispatcher or the handler returned it, is returned and
// not written, so that the management API answers it in its own shape. The request is not labelled with the route's name: it is the
// management operation's.
func (d *Dispatcher) Handoff(w http.ResponseWriter, r *http.Request, format string, key qname.Repo, method, rest string) error {
	f := d.formats[format]
	if f == nil {
		return errs.E(errs.MethodNotAllowed, errs.CodeNotApplicable, "No format "+format+" serves routes.")
	}
	r = formats.WithRefusal(r, formats.Refusal{
		Format: f.Descriptor().ID, Name: key, BaseURL: strings.TrimSuffix(httpserver.InfoFrom(r.Context()).BaseURL, "/"),
	})
	cleaned, err := repository.CleanPath(cleanable(rest))
	if err != nil {
		return err
	}
	route, params, err := f.Match(method, rest)
	if err != nil {
		return err
	}
	c := &caught{Format: f}
	d.serveRoute(w, r, c, key, false, routed{route: route, params: params, escaped: rest, cleaned: cleaned, handoff: true})
	return c.err
}

// caught is a format whose errors are kept instead of written: the stages of serveRoute and formats.ServeRoute write a refusal through
// WriteError, which the handoff returns to the management API.
type caught struct {
	formats.Format
	err error
}

// WriteError implements formats.Format.
func (c *caught) WriteError(_ http.ResponseWriter, _ *http.Request, err error) {
	if c.err == nil {
		c.err = err
	}
}
