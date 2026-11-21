package v1

import (
	"context"
	"time"

	"github.com/guided-traffic/quader/internal/api/v1/gen"
	"github.com/guided-traffic/quader/internal/qname"
	"github.com/guided-traffic/quader/internal/signatures"
)

// The covering of a package by a signed index (signatures.md R-SIG-030): Debian signs the release document, which names the index files,
// which name each package's SHA-256; apt checks no signature of the package. The format answers which signed documents of a repository
// name a stored file (formats.SignatureCoverage); the answers list each with the signatures attached to it in the repository and their
// state, judged as a publisher's would be, so that a covering release signed by a trusted key reads verified.

// SignatureCoveringDocument is a signed document that names a stored file, as the format answers it.
type SignatureCoveringDocument struct {
	// Path is the canonical path of the signed document, SHA256 its digest (the subject of its signatures), Name its name in the format's
	// words (a release's suite), Date its own date (zero when it states none).
	Path   string
	SHA256 [32]byte
	Name   string
	Date   time.Time
}

// SignatureCoverage answers the signed documents of a repository that name a stored file (the format's coverage, nil for a format
// without one); the composition root binds it to formats.CapabilitiesFor(format).SignatureCoverage with the format's data port.
type SignatureCoverage func(ctx context.Context, repoID int64, repo qname.Repo, format, class, path string, sha256 [32]byte) ([]SignatureCoveringDocument, error)

// covering answers the covering documents of the subjects of one repository with their signatures there, in the scope of project.
func (s *Server) covering(ctx context.Context, sv SignatureServices, repoID int64, repo qname.Repo, format, class, project string, ss sigSubjects) ([]gen.SignatureCovering, error) {
	out := []gen.SignatureCovering{}
	if sv.Coverage == nil {
		return out, nil
	}
	seen := map[[32]byte]bool{}
	for _, sub := range ss.list {
		if sub.path == "" {
			continue
		}
		docs, err := sv.Coverage(ctx, repoID, repo, format, class, sub.path, sub.digest)
		if err != nil {
			return out, err
		}
		for _, d := range docs {
			if seen[d.SHA256] {
				continue
			}
			seen[d.SHA256] = true
			c, err := s.coveringEntry(ctx, sv, repoID, repo, project, d)
			if err != nil {
				return out, err
			}
			out = append(out, c)
		}
	}
	return out, nil
}

// coveringEntry is one covering document with the signatures attached to it in the repository. Its state reads the signatures as a
// publisher's (a release's signature has the role registry, which never decides a version's badge, R-SIG-009).
func (s *Server) coveringEntry(ctx context.Context, sv SignatureServices, repoID int64, repo qname.Repo, project string, d SignatureCoveringDocument) (gen.SignatureCovering, error) {
	c := gen.SignatureCovering{Release: d.Path, Suite: d.Name, Signatures: []gen.SignatureEntry{}, State: gen.SignatureBadgeState(signatures.BadgeNone)}
	if !d.Date.IsZero() {
		date := d.Date.UTC()
		c.Date = &date
	}
	views, err := sv.Store.Views(ctx, signatures.ViewQuery{RepoIDs: []int64{repoID}, Subjects: [][32]byte{d.SHA256}, ProjectID: project})
	if err != nil {
		return c, err
	}
	counted := countedOf(views)
	for i := range counted {
		counted[i].Role = signatures.RolePublisher
	}
	state, _ := signatures.Badge(counted)
	c.State = gen.SignatureBadgeState(state)
	var ss sigSubjects
	ss.add(d.SHA256, d.Path, "")
	sigs, atts := sigEntries(views, ss, func(int64) string { return repo.String() })
	c.Signatures = append(sigs, atts...)
	return c, nil
}
