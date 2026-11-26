package clamav

import (
	"context"
	"net/http"
	"strconv"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/observability"
)

// container serves <db>.cvd: the container of the version of db that the set in use names (R-CLAMAV-004, Q-MAL-99), with the
// conditionals and Range of every stored file. A set in use without a .cvd of db (none yet, or a .cud of an installation's own
// repository) answers 404.
func (Format) container(ctx context.Context, rq *formats.Request, w http.ResponseWriter) error {
	db := rq.Params.Get(paramDB)
	port, err := hostPort(rq.Host)
	if err != nil {
		return err
	}
	served, ok, err := port.Served(ctx, rq.RepoRef().ID, db)
	if err != nil {
		return err
	}
	if !ok || served.File != db+".cvd" {
		return notFound("No signed container of " + db + " is in use.")
	}
	return serveStored(ctx, rq, w, db, served.Version, ContainerPath(db, served.Version, served.File))
}

// signature serves <db>-<version>.cvd.sign while it is stored; a version not stored answers 404, which freshclam takes as no error.
func (Format) signature(ctx context.Context, rq *formats.Request, w http.ResponseWriter) error {
	db := rq.Params.Get(paramDB)
	v, err := strconv.Atoi(rq.Params.Get(paramVersion))
	if err != nil {
		return notFound("No such detached signature.")
	}
	return serveStored(ctx, rq, w, db, v, SignaturePath(db, v))
}

// serveStored decides a read of the stored file at path for the database's version and serves it.
func serveStored(ctx context.Context, rq *formats.Request, w http.ResponseWriter, db string, version int, path string) error {
	if err := rq.Host.Authorize(ctx, rq, formats.Target{
		Package: db, Version: strconv.Itoa(version), Path: path, Actions: []formats.Action{formats.ActionRead},
	}); err != nil {
		return err
	}
	it, found, err := rq.Host.Items().Stat(ctx, rq.RepoRef(), formats.Path{Path: path})
	if err != nil {
		return err
	}
	if !found || it.Folder {
		return notFound("The file is not stored.")
	}
	return rq.Host.Items().Serve(ctx, rq, w, it, formats.ServeMayRevalidate)
}

func notFound(detail string) error { return errs.E(errs.NotFound, errs.CodeNotFound, detail) }

// challenge is the challenge of a 401 on the endpoint: freshclam sends the credentials of its URL as Basic (R-CLAMAV-006).
const challenge = `Basic realm="quader"`

// WriteError renders an error as a problem document whose status a stock freshclam can live with (R-CLAMAV-005, Q-CLAMAV-3): a
// refusal of the decision point or of any other stage (Forbidden) answers 404, because a 403 puts freshclam on a cool-down of 24
// hours; a refusal of a limit (RateLimited) answers 503 with the error's Retry-After, because a 429 puts freshclam on an older libcurl
// on a cool-down of 4 hours; a 401 carries the Basic challenge. Every other error keeps its status. The body names the error's code
// and detail, for a person who asks with curl; the chain's refusals on the prefix are rendered here too (R-ARCH-023).
func (Format) WriteError(w http.ResponseWriter, r *http.Request, err error) {
	errs.Write(w, r, Rendered(err), w.Header().Get("X-Request-Id"), observability.LoggerFrom(r.Context()))
}

// Rendered maps an error to the kind the endpoint answers it with (the table of R-CLAMAV-005).
func Rendered(err error) error {
	e, ok := errs.As(err)
	if !ok {
		return err
	}
	c := *e
	switch c.Kind {
	case errs.Forbidden:
		c.Kind = errs.NotFound
	case errs.RateLimited:
		c.Kind = errs.Unavailable
		if c.ForwardDetail == "" {
			// The refusal of a limit names the caller's own wait, which a caller of a Bastion reads too; as a 503 it would read the
			// neutral text of the status instead (bastion.md R-BAS-114, Q-BAS-386).
			c.ForwardDetail = c.Detail
		}
	case errs.Unauthenticated:
		c.Challenge = challenge
	}
	return &c
}
