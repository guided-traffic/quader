package formathost

import (
	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/items"
)

// downloadUnit is what the download event of the item at path names of its unit (operations-and-observability.md R-OPS-054), for the
// request's format and with the version the request was allowed to read ([UnitOf]). It reads nothing but the format's code and the
// request's state.
func (h *Host) downloadUnit(rq *formats.Request, path string) items.DownloadUnit {
	if rq == nil || rq.Repo == nil {
		return items.DownloadUnit{}
	}
	f, ok := h.o.lookup(rq.Repo.Format)
	if !ok {
		return items.DownloadUnit{}
	}
	read := ""
	if st := stateOf(rq); st != nil {
		read = st.readVersion()
	}
	return UnitOf(f, path, read)
}

// UnitOf is the unit a download of the item at path of format f names (R-OPS-054): the package and the version f classifies the path
// in, read where the path names no version (the version the request was allowed to read: an image's tag on a remote, whose fetch is
// counted on the manifest's digest), and the unit's key by f's declaration (formats.Descriptor.DownloadUnit, R-OPS-053): an image's
// name, a version's "<name>@<version>", none for a format whose unit is a file or for an item of no package. Files says that f's unit
// is a file: a change of such a file names its folder (R-OPS-056).
func UnitOf(f formats.Format, path, read string) items.DownloadUnit {
	unit := f.Descriptor().DownloadUnit
	files := unit != formats.DownloadUnitImage && unit != formats.DownloadUnitVersion
	info := f.Classify(formats.Path{Path: path})
	u := items.DownloadUnit{Package: info.Package, Version: info.Version}
	if u.Package == "" || files {
		return items.DownloadUnit{Files: files}
	}
	if u.Version == "" {
		u.Version = read
	}
	switch f.Descriptor().DownloadUnit {
	case formats.DownloadUnitImage:
		u.Key = u.Package
	case formats.DownloadUnitVersion:
		if u.Version != "" {
			u.Key = u.Package + "@" + u.Version
		}
	}
	return u
}
