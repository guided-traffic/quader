package clamav

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/clamav/cvd"
	"github.com/guided-traffic/quader/internal/formats/clamav/rows"
)

// View is the format's part of the package screens (R-CLAMAV-007): the versions order as integers, a version shows every piece of
// its metadata (R-CLAMAV-003), and the history rows whose files the cleanup removed follow the stored versions.
func (Format) View() formats.ViewAdapter { return viewAdapter{} }

type viewAdapter struct{}

var _ formats.ViewAdapter = viewAdapter{}

// Compare orders versions as integers; a string that is no version sorts before every version.
func (viewAdapter) Compare(a, b string) int {
	va, ea := strconv.Atoi(a)
	vb, eb := strconv.Atoi(b)
	switch {
	case ea != nil && eb != nil:
		return strings.Compare(a, b)
	case ea != nil:
		return -1
	case eb != nil:
		return 1
	case va < vb:
		return -1
	case va > vb:
		return 1
	}
	return strings.Compare(a, b)
}

// Stored lists the versions of a database the repository holds: the stored ones newest first, then the history rows without files.
func (viewAdapter) Stored(ctx context.Context, ports formats.DataPorts, q formats.StoredQuery) (formats.Stored, error) {
	if q.Repo.Class != formats.Local || !cvd.IsDB(q.Package) || q.Limit <= 0 {
		return formats.Stored{}, nil
	}
	p, err := hostPort(ports)
	if err != nil {
		return formats.Stored{}, err
	}
	vs, err := p.Stored(ctx, q.Repo.ID, q.Package, q.Limit)
	if err != nil {
		return formats.Stored{}, err
	}
	out := formats.Stored{Versions: make([]string, 0, len(vs))}
	for _, v := range vs {
		out.Versions = append(out.Versions, strconv.Itoa(v.Version))
	}
	return out, nil
}

// Withheld names none: every stored version is served by its path (R-CLAMAV-007).
func (viewAdapter) Withheld(context.Context, formats.DataPorts, []string) ([]formats.VersionID, error) {
	return nil, nil
}

// Find has nothing to search beyond the three names, which the generic rows answer: a version has no description and no keywords.
func (viewAdapter) Find(context.Context, formats.DataPorts, formats.ViewFind, int) ([]formats.VersionID, error) {
	return nil, nil
}

// Describe returns the metadata of R-CLAMAV-003 for each version with a side row; the description names the database and the build
// time. A version has no README and no member that names other versions.
func (viewAdapter) Describe(ctx context.Context, ports formats.DataPorts, vs []formats.ViewVersion, want formats.ViewFields, _ func(string) bool) (map[formats.VersionID]formats.ViewData, error) {
	p, err := hostPort(ports)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, len(vs))
	for i, v := range vs {
		ids[i] = int64(v.ID)
	}
	rs, err := p.Versions(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make(map[formats.VersionID]formats.ViewData, len(rs))
	for _, r := range rs {
		d := formats.ViewData{Description: describeLine(r)}
		if want.Has(formats.ViewDetails) {
			d.Metadata = Metadata(r)
		}
		out[formats.VersionID(r.VersionID)] = d
	}
	return out, nil
}

// describeLine is the one line a list shows of a version: "ClamAV daily signatures, built 2026-11-21 08:29 UTC".
func describeLine(r rows.Version) string {
	s := "ClamAV " + r.DB + " signatures"
	if !r.Container.BuiltAt.IsZero() {
		s += ", built " + r.Container.BuiltAt.UTC().Format("2006-01-02 15:04 UTC")
	}
	return s
}

// Metadata is the object of the version details (R-CLAMAV-003, api-and-ui.md R-UI-194). Every text of a header is a string the screens
// show as text.
func Metadata(r rows.Version) map[string]any {
	c := r.Container
	md := map[string]any{
		"database": r.DB,
		"version":  r.Version,
		"state":    string(r.State()),
		"storedAt": r.StoredAt.UTC().Format(time.RFC3339),
	}
	if r.File != "" {
		md["file"] = r.File
	}
	if r.FilesRemovedAt != nil {
		md["filesRemovedAt"] = r.FilesRemovedAt.UTC().Format(time.RFC3339)
	}
	if !c.BuiltAt.IsZero() {
		lvl := cvd.LevelOf(c.FunctionalityLevel)
		md["builtAt"] = c.BuiltAt.UTC().Format(time.RFC3339)
		md["buildTimeText"] = c.BuildTimeText
		md["signatures"] = c.Signatures
		md["functionalityLevel"] = c.FunctionalityLevel
		md["functionalityLevelText"] = lvl.Text()
		md["builder"] = c.Builder
		md["verification"] = map[string]any{
			"outcome": string(c.Verification), "headerMd5": c.HeaderMD5, "computedMd5": c.ComputedMD5, "source": c.Source,
		}
		md["membersRead"] = c.MembersRead
		members := make([]map[string]any, 0, len(c.Members))
		for _, m := range c.Members {
			members = append(members, map[string]any{"name": m.Name, "size": m.Size, "sha256": m.SHA256, "lines": m.Lines})
		}
		md["members"] = members
	}
	sig := map[string]any{"present": r.SignName != ""}
	if r.SignName != "" {
		sig["name"], sig["size"], sig["signatures"] = r.SignName, r.SignSize, r.SignSignatures
	}
	md["detachedSignature"] = sig
	return md
}
