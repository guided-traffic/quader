package nuget

import (
	"bytes"
	"context"
	"encoding/hex"
	"net/http"
	"strconv"
	"unicode/utf8"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/nuget/rows"
)

// The embedded files of a version (R-NUGET-024): GET and HEAD of v3/flatcontainer/{id}/{version}/icon, …/license and …/readme. A
// file is read out of the stored .nupkg when a client asks for it, through the one reader of a stored package (nupkg.Open, in the
// data port), and is never stored as an item of its own (Q-NUGET-135). What the .nuspec names comes from PostgreSQL, so a version
// that embeds none of the asked file is a 404 without a read of the object store. The file is content of the package it is read
// from: the read decides the three checks of a download of that .nupkg (the malware gate, the vulnerability block, the signature
// gate) and serves nothing they hold back (Q-NUGET-136). A remote repository serves the files of the packages its cache holds, and
// a virtual repository those of the first member that holds the package (Q-NUGET-137).

// maxEmbedded is R-NUGET-024's bound of an embedded file: 1 MiB, the icon, the license and the README alike.
const maxEmbedded = 1 << 20

// The media types of the embedded files (R-NUGET-024): an icon by its content, PNG or JPEG.
const (
	pngMediaType      = "image/png"
	jpegMediaType     = "image/jpeg"
	licenseMediaType  = "text/plain; charset=utf-8"
	markdownMediaType = "text/markdown; charset=utf-8"
)

// EmbeddedReader is the part of the data port the embedded files read (internal/formatdata/nuget/embedded.go). It is a separate
// interface because the contract's DataPort ([Store]) is the port of the protocol and the writes; the same bound value implements
// both, and the twin of nugettest does too.
type EmbeddedReader interface {
	// EmbeddedNames reads, in one statement, the package paths the .nuspec of a live version names for its icon, its license file
	// and its README; found is false for a number the repository holds no live version of.
	EmbeddedNames(ctx context.Context, repoID int64, lowerID, lowerVersion string) (names rows.Embedded, found bool, err error)
	// EmbeddedFile reads one file of the stored package with the SHA-256 digest and the size, at most max bytes, with ranged reads;
	// nil for a package that cannot be opened, a path it does not hold and a file above max.
	EmbeddedFile(ctx context.Context, sha256 []byte, size int64, path string, max int64) ([]byte, error)
}

func embeddedPortOf(ports formats.DataPorts) (EmbeddedReader, error) {
	if ports != nil {
		if p, ok := ports.FormatData(ID).(EmbeddedReader); ok {
			return p, nil
		}
	}
	return nil, errs.E(errs.Internal, errs.CodeInternal, "nuget: the data port reads no embedded files")
}

// EmbeddedPath is the canonical path of an embedded file of a version, the path its read is decided on (R-NUGET-024):
// "{lower_id}/{lower_version}/.embedded/{file}".
func EmbeddedPath(lowerID, lowerVersion, file string) string {
	return lowerID + "/" + lowerVersion + "/" + canonEmbedded + "/" + file
}

// handleEmbedded serves an embedded file of a version in every class. The read is decided on the file's canonical path; an endpoint
// with exposure rules that hides the version hides its files.
func (f *Format) handleEmbedded(ctx context.Context, rq *formats.Request, w http.ResponseWriter) error {
	lowerID, ver, file := rq.Params.Get("id"), rq.Params.Get("version"), rq.Params.Get("file")
	if err := rq.Host.Authorize(ctx, rq, formats.Target{Package: lowerID, Version: ver, Path: EmbeddedPath(lowerID, ver, file), Actions: []formats.Action{formats.ActionRead}}); err != nil {
		return err
	}
	if hiddenVersion(rq, lowerID, ver) {
		return errNoSuchResource()
	}
	if rq.Repo.Class == formats.Virtual {
		return f.virtualEmbedded(ctx, rq, w, lowerID, ver, file)
	}
	return f.embeddedIn(ctx, rq, w, lowerID, ver, file)
}

// virtualEmbedded walks the members that take part for the version's .nupkg, in resolution order, and answers with the first that
// holds the package: its file, its 404 of a file the package does not embed, or a check's refusal, which ends the walk (R-REPO-019,
// errs.IsMemberMiss). A member's miss goes on to the next member, and a member that could not be asked as well, whose failure is the
// answer when no member holds the package. A remote member answers from its cache: no fetch is started for an embedded file.
func (f *Format) virtualEmbedded(ctx context.Context, rq *formats.Request, w http.ResponseWriter, lowerID, ver, file string) error {
	p := formats.Path{Path: NupkgPath(lowerID, ver)}
	plan, err := rq.Host.Resolver().Plan(ctx, rq, p, f.Classify(p))
	if err != nil {
		return err
	}
	if !plan.Admitted {
		return errNoSuchResource()
	}
	var failed error
	for _, pm := range plan.Members {
		mrq, err := memberRequest(rq, pm.Member.Name)
		if err != nil {
			return err
		}
		err = f.embeddedIn(ctx, mrq, w, lowerID, ver, file)
		switch {
		case err == nil:
			return nil
		case errs.IsMemberMiss(err):
		case errs.IsMemberFailure(err):
			if failed == nil {
				failed = err
			}
		default:
			return err
		}
	}
	if failed != nil {
		return failed
	}
	return plan.Unknown(errNoSuchResource())
}

// embeddedIn answers the file from the repository of rq, local or remote, or a member of a virtual repository whose read the plan
// decided on the .nupkg's path. A version the caller's grants hide in the repository (its .nupkg's path is refused, Q-NUGET-120) is a
// missing one, as for the .nuspec.
func (f *Format) embeddedIn(ctx context.Context, rq *formats.Request, w http.ResponseWriter, lowerID, ver, file string) error {
	cv, err := formats.CallerVersions(ctx, rq, lowerID)
	if err != nil {
		return err
	}
	if !cv.Version(ver) {
		return errNoSuchResource()
	}
	port, err := embeddedPortOf(rq.Host)
	if err != nil {
		return err
	}
	names, found, err := port.EmbeddedNames(ctx, rq.RepoRef().ID, lowerID, ver)
	if err != nil {
		return err
	}
	entry := names.Path(file)
	if !found || entry == "" {
		return errNoSuchResource()
	}
	it, found, err := rq.Host.Items().Stat(ctx, rq.RepoRef(), formats.Path{Path: NupkgPath(lowerID, ver)})
	if err != nil {
		return err
	}
	if !found || it.Folder {
		return errNoSuchResource()
	}
	// The serve-time check of the .nupkg (a tombstoned or unknown version of a local repository), then the checks of its download.
	if err := f.ServeCheck(ctx, rq.Host, it.Ref.Repo, it.Ref, formats.ServeMayRevalidate); err != nil {
		return err
	}
	if err := rq.Host.Items().Check(ctx, rq, it); err != nil {
		return err
	}
	etag := `"` + hex.EncodeToString(it.Digests.SHA256) + "." + file + `"`
	if ifNoneMatch(rq.Header.Get("If-None-Match"), etag) {
		w.Header().Set("ETag", etag)
		w.WriteHeader(http.StatusNotModified)
		return nil
	}
	body, err := port.EmbeddedFile(ctx, it.Digests.SHA256, it.Size, entry, maxEmbedded)
	if err != nil {
		return err
	}
	mediaType, ok := embeddedMediaType(file, body)
	if !ok {
		return errNoSuchResource()
	}
	h := w.Header()
	h.Set("Content-Type", mediaType)
	h.Set("Content-Length", strconv.Itoa(len(body)))
	h.Set("ETag", etag)
	w.WriteHeader(http.StatusOK)
	if rq.Method == http.MethodHead {
		return nil
	}
	_, err = w.Write(body)
	return err
}

// embeddedMediaType is the media type an embedded file is served with, and false for a file outside R-NUGET-024's bounds: an icon is
// PNG or JPEG by its content, a license and a README are UTF-8 text. A file that is none of these is answered as one the package does
// not embed.
func embeddedMediaType(file string, body []byte) (string, bool) {
	if body == nil {
		return "", false
	}
	switch file {
	case "icon":
		switch {
		case bytes.HasPrefix(body, []byte("\x89PNG\r\n\x1a\n")):
			return pngMediaType, true
		case bytes.HasPrefix(body, []byte{0xff, 0xd8, 0xff}):
			return jpegMediaType, true
		}
		return "", false
	case "license":
		return licenseMediaType, utf8.Valid(body)
	case "readme":
		return markdownMediaType, utf8.Valid(body)
	}
	return "", false
}
