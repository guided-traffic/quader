package ansible

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/qname"
)

// `ansible-galaxy role search` through a remote repository (R-ANS-056): the client's query passes through to the upstream's
// `v1/search/roles/`, as the engine passes every Volatile request (R-ARCH-013 step 6): through the repository's client under its
// egress rules, credential policy, fetch slots and circuit, kept for a short time per replica and never stored. Only the parameters the
// client sends pass, each bounded; the answer keeps the roles the caller may see, each with its addresses on the repository's routes.

// roleSearchPath is the repository path of the pass-through, the key the engine keeps an answer under with its parameters.
const roleSearchPath = dirRoles + "/search"

// roleSearchTTL is how long a replica keeps an upstream's answer to one search.
const roleSearchTTL = time.Minute

// The bounds of a search (R-ANS-056): a word of the query, the page and its size. ansible-galaxy asks for page_size 1000.
const (
	maxRoleSearchText = 128
	maxRoleSearchPage = 1000
	maxRoleSearchSize = 1000
)

// roleSearchWords are the parameters of `v1/search/roles/` that ansible-galaxy sends (api.py, search_roles): the text, the tags, the
// platforms and the author; anything else is dropped.
var roleSearchWords = []string{"autocomplete", "tags_autocomplete", "platforms_autocomplete", "username_autocomplete"}

// roleSearchParams reads the client's query: the words that pass, each at most maxRoleSearchText characters of printable text, and the
// page and its size as numbers in their bounds. A value outside them is 400 in the v1 shape.
func roleSearchParams(q url.Values) (url.Values, error) {
	out := url.Values{}
	for _, k := range roleSearchWords {
		v := strings.TrimSpace(q.Get(k))
		if v == "" {
			continue
		}
		if utf8.RuneCountInString(v) > maxRoleSearchText || !utf8.ValidString(v) || strings.ContainsFunc(v, isControl) {
			return nil, errs.E(errs.Invalid, errs.CodeAnsibleInvalidRequest,
				k+" is at most "+strconv.Itoa(maxRoleSearchText)+" characters of text.")
		}
		out.Set(k, v)
	}
	for k, bound := range map[string]int{"page": maxRoleSearchPage, "page_size": maxRoleSearchSize} {
		s := q.Get(k)
		if s == "" {
			continue
		}
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > bound {
			return nil, errs.E(errs.Invalid, errs.CodeAnsibleInvalidRequest, k+" is a number from 1 to "+strconv.Itoa(bound)+".")
		}
		out.Set(k, strconv.Itoa(n))
	}
	return out, nil
}

// UpstreamQuery maps the pass-through of a role search to the upstream request: the v1 API the API root announced, search/roles/, with
// the parameters the format cleaned (R-ANS-056). The address is built from the repository's URL and the announced prefix only.
func (remoteAdapter) UpstreamQuery(ctx context.Context, rc formats.RemoteContext, q formats.Query) (*http.Request, error) {
	if q.Path.Path != roleSearchPath {
		return nil, errs.E(errs.Internal, errs.CodeInternal, "The Ansible remote passes the role search through and nothing else.")
	}
	d, err := discoveryOf(rc)
	if err != nil {
		return nil, err
	}
	if d.v1 == "" {
		return nil, missing()
	}
	base := *rc.Upstream
	base.User, base.Fragment, base.RawFragment, base.RawQuery = nil, "", "", ""
	if !strings.HasSuffix(base.Path, "/") {
		base.Path += "/"
	}
	u, err := url.Parse(base.String() + d.v1 + "search/roles/")
	if err != nil {
		return nil, errs.E(errs.Internal, errs.CodeInternal, "The upstream address does not build.", errs.WithCause(err))
	}
	u.RawQuery = q.Params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, errs.E(errs.Internal, errs.CodeInternal, "", errs.WithCause(err))
	}
	req.Header.Set("Accept", "application/json")
	return req, nil
}

// remoteRoleSearch answers `ansible-galaxy role search` (R-ANS-056): the upstream's answer with the roles the caller may see.
func (f *Format) remoteRoleSearch(ctx context.Context, rq *formats.Request, w http.ResponseWriter) error {
	params, err := roleSearchParams(rq.URL.Query())
	if err != nil {
		return err
	}
	if err := rq.Host.Authorize(ctx, rq, readTarget("", "", roleSearchPath)); err != nil {
		return err
	}
	if err := requireV1(ctx, rq); err != nil {
		return err
	}
	res, err := rq.Host.Remote().Query(ctx, rq, formats.Query{Path: formats.Path{Path: roleSearchPath}, Params: params, TTL: roleSearchTTL})
	if errors.Is(err, formats.ErrOffline) {
		return errs.E(errs.Unavailable, errs.CodeUnavailable,
			"The repository may not ask its upstream now, and a role search is not kept: search again when it may.")
	}
	if err != nil {
		return err
	}
	keep, err := f.roleSearchKeep(ctx, rq)
	if err != nil {
		return err
	}
	body, err := renderRoleSearch(res.Body, rq.Host.Links(rq).Registry(servingName(rq)), params, keep)
	if err != nil {
		return err
	}
	h := w.Header()
	h.Set("Content-Type", "application/json")
	h.Set("Cache-Control", "private, no-store")
	h.Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(http.StatusOK)
	_, err = w.Write(body)
	return err
}

// roleSearchKeep decides a hit by its role's package (Q-ANS-36): the repository's include and exclude patterns, its delivery rules (a
// role they refuse whole is not listed, as a role install of it is refused), and the caller's read of the package.
func (f *Format) roleSearchKeep(ctx context.Context, rq *formats.Request) (func(pkg string) bool, error) {
	scope, err := rq.Host.Resolver().ReadScope(ctx, rq, []qname.Repo{rq.Repo.Name})
	if err != nil {
		return nil, err
	}
	set := formats.Delivery(rq)
	return func(pkg string) bool {
		if !rq.Repo.Admits(pkg, false) || !set.Package(pkg).Delivered {
			return false
		}
		switch scope.Shape(rq.Repo.Name) {
		case formats.ReadAll:
			return true
		case formats.ReadRows:
			return scope.MayRead(rq.Repo.Name, pkg, "")
		}
		return false
	}, nil
}

// renderRoleSearch renders the upstream's answer for the repository whose URL is repo: the roles keep decides on, each with its own
// addresses on the repository's routes and without a download_url, the count less the roles left out of this page, and the links of the
// page on the repository's search with the parameters that passed (R-ANS-056). Every other member passes unchanged.
func renderRoleSearch(doc []byte, repo string, params url.Values, keep func(pkg string) bool) ([]byte, error) {
	var page map[string]json.RawMessage
	if len(doc) > MaxDocumentBytes || json.Unmarshal(doc, &page) != nil || page == nil {
		return nil, invalidDocument("a role search", "it is not a JSON object")
	}
	var results []map[string]json.RawMessage
	if raw, ok := page["results"]; ok {
		if err := json.Unmarshal(raw, &results); err != nil {
			return nil, invalidDocument("a role search", "results is not a list of objects")
		}
	}
	kept := results[:0]
	for _, r := range results {
		var id int64
		if json.Unmarshal(r["id"], &id) != nil || id < 1 || !digits(strconv.FormatInt(id, 10), maxRoleIDDigits) {
			continue
		}
		sid := strconv.FormatInt(id, 10)
		if !keep(RolePackage(sid)) {
			continue
		}
		// A client is never sent to download from an address the repository did not write (R-ANS-020): a hit names none.
		delete(r, "download_url")
		r["url"] = rawString(repo + "v1/roles/" + sid + "/")
		if raw, ok := r["related"]; ok {
			var rel map[string]string
			if json.Unmarshal(raw, &rel) == nil {
				for k := range rel {
					rel[k] = repo + "v1/roles/" + sid + "/" + k + "/"
				}
				r["related"], _ = json.Marshal(rel) //nolint:errcheck // a map of strings
			}
		}
		kept = append(kept, r)
	}
	var count int
	if json.Unmarshal(page["count"], &count) == nil {
		page["count"] = json.RawMessage(strconv.Itoa(max(count-(len(results)-len(kept)), len(kept))))
	}
	extra := url.Values{}
	for k, v := range params {
		if k != "page" && k != "page_size" {
			extra[k] = v
		}
	}
	size := roleSearchDefaultSize
	if n, err := strconv.Atoi(params.Get("page_size")); err == nil {
		size = n
	}
	return renderRolePage(page, kept, repo, "v1/search/roles/", extra, size)
}

// roleSearchDefaultSize is the page size a link names when the client asked for none (the public Galaxy's default).
const roleSearchDefaultSize = 10
