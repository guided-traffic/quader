package v1

import (
	"context"
	"net/http"
	"strings"
	"unicode"

	"github.com/guided-traffic/quader/internal/api/v1/gen"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats/deb/rows"
	"github.com/guided-traffic/quader/internal/httpserver"
	"github.com/guided-traffic/quader/internal/packages/view"
	"github.com/guided-traffic/quader/internal/qname"
	"github.com/guided-traffic/quader/internal/repository"
)

// DebContentsStore is the part of the Debian data port the operation of a version's contents reads (formats/debian.md R-DEB-039): rows
// only, never a blob or an upstream.
type DebContentsStore interface {
	VersionContents(ctx context.Context, repoID int64, pkg, version string) ([]rows.ContentsHead, error)
	ContentsFiles(ctx context.Context, itemID int64, needle string, offset, limit int) (rows.ContentsPage, error)
}

// The bounds of the operation (R-DEB-039).
const (
	contentsDefaultLimit = 200
	contentsMaxLimit     = 1000
	contentsMaxOffset    = 10_000
	contentsMaxQuery     = 256
)

// GetPackageVersionContents implements GET /api/v1/packages/{packageId}/versions/{version}/contents (R-DEB-039).
func (s *Server) GetPackageVersionContents(w http.ResponseWriter, r *http.Request, id gen.PackageId, version gen.PackageVersionPath,
	params gen.GetPackageVersionContentsParams,
) {
	out, err := s.versionContents(r, id, version, params)
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	writeTagged(w, r, out)
}

func (s *Server) versionContents(r *http.Request, id gen.PackageId, version gen.PackageVersionPath,
	params gen.GetPackageVersionContentsParams,
) (gen.PackageVersionContents, error) {
	var out gen.PackageVersionContents
	ps, err := s.packageServices()
	if err != nil {
		return out, err
	}
	st := s.o.DebContents
	if st == nil {
		return out, unavailable("The contents of Debian package files")
	}
	format, name, ok := view.ParseID(id)
	if !ok || format != "deb" {
		return out, docsNotFound()
	}
	needle, offset, limit, err := contentsQuery(params)
	if err != nil {
		return out, err
	}
	ctx, sc, err := s.packageScope(r, ps, view.ScopeRequest{Formats: []string{format}, ExactName: name})
	if err != nil {
		return out, err
	}
	d, found, err := ps.View.Version(ctx, sc, format, name, version)
	if err != nil {
		return out, err
	}
	if !found {
		return out, docsNotFound()
	}
	held := map[qname.Repo]string{}
	for _, e := range d.Repositories {
		held[e.Repo] = e.Class
	}
	cands, err := docsCandidates(ctx, ps, held, params.Repository)
	if err != nil {
		return out, err
	}
	for i := range cands {
		heads, err := st.VersionContents(ctx, cands[i].NumericID(), name, version)
		if err != nil {
			return out, err
		}
		if len(heads) == 0 {
			continue
		}
		return s.contentsOf(ctx, st, sc, cands[i], heads, name, version, params.Architecture, needle, offset, limit)
	}
	return out, docsNotFound()
}

// contentsQuery validates the filter and the page: control characters are dropped and the text is trimmed; a page past the kept list or
// over the bounds is 422.
func contentsQuery(p gen.GetPackageVersionContentsParams) (needle string, offset, limit int, err error) {
	limit = contentsDefaultLimit
	if p.Limit != nil {
		limit = int(*p.Limit)
	}
	if p.Offset != nil {
		offset = int(*p.Offset)
	}
	if limit < 1 || limit > contentsMaxLimit || offset < 0 || offset > contentsMaxOffset {
		return "", 0, 0, errs.E(errs.Unprocessable, errs.CodeValidation, "offset is 0 to 10000 and limit 1 to 1000.")
	}
	if p.Q != nil {
		needle = strings.TrimSpace(strings.Map(func(r rune) rune {
			if unicode.IsControl(r) {
				return -1
			}
			return r
		}, *p.Q))
		if len(needle) > contentsMaxQuery {
			return "", 0, 0, errs.E(errs.Unprocessable, errs.CodeValidation, "q is at most 256 characters.")
		}
	}
	return needle, offset, limit, nil
}

// contentsOf answers the package file of the architecture asked for (the first one without), with the wiki rule of the repository for
// the changelog and the copyright and the reason of a refusal for a reader of its configuration only.
func (s *Server) contentsOf(ctx context.Context, st DebContentsStore, sc view.Scope, repo repository.Repository, heads []rows.ContentsHead,
	name, version string, arch *string, needle string, offset, limit int,
) (gen.PackageVersionContents, error) {
	h := heads[0]
	archs := make([]string, 0, len(heads))
	for _, x := range heads {
		archs = append(archs, x.Architecture)
		if arch != nil && x.Architecture == *arch {
			h = x
		}
	}
	if arch != nil && h.Architecture != *arch {
		return gen.PackageVersionContents{}, docsNotFound()
	}
	out := gen.PackageVersionContents{Repository: repoName(repo), Architecture: h.Architecture, Architectures: archs}
	switch h.State {
	case rows.ContentsPending:
		out.State = gen.PackageVersionContentsStatePending
		return out, nil
	case rows.ContentsRefused:
		out.State = gen.PackageVersionContentsStateUnavailable
		cfg, err := s.readsConfig(ctx, repo.Name.Project)
		if err != nil {
			return out, err
		}
		if cfg {
			reason := gen.PackageVersionContentsReason(h.Reason)
			out.Reason, out.Detail = &reason, &h.Detail
		}
		out.ReadAt = &h.ReadAt
		return out, nil
	}
	out.State = gen.PackageVersionContentsStateAvailable
	out.ReadAt = &h.ReadAt
	// The changelog and the copyright are the uploader's or the upstream's text: wiki content (R-AUTHZ-034).
	if sc.MayReadWiki(repo.Name, name, version) {
		if h.Changelog != "" {
			out.Changelog = &gen.PackageChangelog{
				File: gen.PackageChangelogFile(h.ChangelogFile), Text: h.Changelog, Truncated: h.ChangelogTruncated,
			}
		}
		if h.Copyright != "" {
			licenses := h.Licenses
			if licenses == nil {
				licenses = []string{}
			}
			out.Copyright = &gen.PackageCopyright{Text: h.Copyright, Truncated: h.CopyrightTruncated, Licenses: licenses}
		}
	}
	if h.DocLink != "" {
		out.DocLink = &h.DocLink
	}
	page, err := st.ContentsFiles(ctx, h.ItemID, needle, offset, limit)
	if err != nil {
		return out, errs.Wrap(err, "api.getPackageVersionContents")
	}
	matched := page.Matched
	if needle == "" {
		matched = h.FilesKept
	}
	items := make([]gen.PackageFileEntry, 0, len(page.Files))
	for _, f := range page.Files {
		e := gen.PackageFileEntry{Path: f.Path, Kind: gen.PackageFileEntryKindFile}
		switch f.Kind {
		case "link", "hardlink", "special":
			e.Kind = gen.PackageFileEntryKind(f.Kind)
		default:
			size := f.Size
			e.Size = &size
		}
		if f.Target != "" {
			target := f.Target
			e.Target = &target
		}
		items = append(items, e)
	}
	out.Files = &gen.PackageFileList{
		Total: int32(min(h.FileCount, 1<<30)), Kept: int32(min(h.FilesKept, 1<<30)), //nolint:gosec // bounded by the reader
		Matched: int32(min(matched, 1<<30)), Offset: int32(offset), Items: items, //nolint:gosec // bounded above
	}
	return out, nil
}
