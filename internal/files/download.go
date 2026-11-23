package files

import (
	"context"
	"io"

	"github.com/guided-traffic/quader/internal/qname"

	"github.com/guided-traffic/quader/internal/auth/principal"
	"github.com/guided-traffic/quader/internal/authz"
	"github.com/guided-traffic/quader/internal/bastionmain"
	"github.com/guided-traffic/quader/internal/items"
	"github.com/guided-traffic/quader/internal/malware/gate"
	rc "github.com/guided-traffic/quader/internal/remotecuration"
	"github.com/guided-traffic/quader/internal/remotecuration/rules"
	rulestore "github.com/guided-traffic/quader/internal/remotecuration/rules/store"
	"github.com/guided-traffic/quader/internal/repository"
	sgate "github.com/guided-traffic/quader/internal/signatures/gate"
	"github.com/guided-traffic/quader/internal/vuln/enforce"
)

// Resolved is the answer of Stat: what the request path resolved to, from
// PostgreSQL alone (R-STOR-012: HEAD and conditional requests never reach the
// object store).
type Resolved struct {
	Repo repository.Repository
	// Item is the file, or the folder when IsFolder.
	Item     items.Item
	IsFolder bool
	// Decided reports whether every check of a stored file that applies passed at its first decision on a decided state
	// (sgate.Serve.RunDecided), the condition of a handover per blob (storage.md R-STOR-095 step 3); false for a folder.
	Decided bool
}

// Stat resolves a GET or HEAD of /files/ (R-ARCH-011 steps 4 to 6): `read` on the
// coordinate the format gives the path (R-EP-014) is decided before the item is
// looked up, a path that the repository's patterns do not admit is a missing path
// (R-GENERIC-007, R-REPO-005), and a blob whose object is missing or damaged is
// refused (R-STOR-012). A folder is returned as a folder; the caller renders its
// index. The root of the repository is a folder.
func (s *Service) Stat(ctx context.Context, c Caller, repoKey qname.Repo, p repository.Cleaned) (Resolved, error) {
	kind := authz.KindArtifact
	if p.Folder {
		kind = authz.KindMetadata
	}
	t := s.locate(ctx, repoKey, p.Path, p.Folder)
	if _, err := c.decide(ctx, repoKey, p.Path, t.class, kind, authz.Read); err != nil {
		return Resolved{}, err
	}
	repo, err := t.resolved(true)
	if err != nil {
		return Resolved{}, err
	}
	if !admits(repo, p.Path, p.Folder, t.class) {
		return Resolved{}, notFound()
	}
	if p.Path == "" {
		return Resolved{Repo: repo, Item: items.Root(repoKey, repo.CreatedAt, repo.UpdatedAt), IsFolder: true}, nil
	}
	ref := items.RepoOf(repo)
	ref.Vuln = s.enforce.PolicyOf(repo) // the block of the version is read by the lookup itself (R-VULN-015)
	if s.formats != nil {
		ref.Facts = s.formats.ServeFactsKey(repo, p.Path) // and the rows the format's serve check decides on (R-ARCH-027)
	}
	it, found, err := s.items.Live(ctx, ref, p.Path)
	if err != nil {
		return Resolved{}, err
	}
	if !found || (p.Folder && it.Kind != items.KindFolder) {
		if !found && repo.Class == repository.Local {
			// A local repository that holds nothing at the path holds nothing for any subject (R-BAS-013).
			bastionmain.NoteMissing(ctx)
		}
		return Resolved{}, notFound()
	}
	if it.Kind == items.KindFolder {
		return Resolved{Repo: repo, Item: it, IsFolder: true}, nil
	}
	// A blob marked missing is looked for again once per interval, and served when its object is back (Q-STOR-153).
	if it, err = s.items.Usable(ctx, it); err != nil {
		return Resolved{}, err
	}
	// A format keeps a say about its own items on this raw view too (R-ARCH-008
	// "Serve-time check"): npm's tombstoned tarball is a 404 here as on its route.
	if s.formats != nil {
		if err := s.formats.ServeCheck(ctx, repo, it); err != nil {
			return Resolved{}, err
		}
	}
	// The three checks of a stored item, after authorization and the format's own check, so that a version the caller may not read
	// stays a 404 and a tombstoned one the format's: the malware gate (R-MAL-013; every file of /files/ is a download), the block of the
	// vulnerability scanning (R-VULN-015) and the gate of signatures (R-SIG-064), decided together before one wait (R-SIG-069).
	srv := sgate.Serve{
		Enforce: s.enforce,
		VulnOf: enforce.Download{
			Addressed: repoKey.String(), Store: repo.Name.String(), RepoID: repo.ID, Format: repo.Format, Path: it.Path, VersionID: it.VersionID,
			Stored: true, Block: s.enforce.Decide(it.Policy, it.Block, it.VersionID), Actor: c.Actor, Source: c.Source,
			Endpoint: c.Subject.Endpoint, Ecosystem: it.Policy.Ecosystem,
		},
	}
	if s.malware.Enabled() {
		srv.Malware = s.malware
		srv.MalwareOf = gate.Download{
			Addressed: repoKey, HolderID: repo.NumericID(), Path: it.Path, Digest: it.Digest, Size: it.Size, Generated: it.Generated, System: repo.Upstream.System(),
			Verdict: it.Malware, Strict: s.malware.Strict(repo.MalwareOnlyChecked()), Arrived: c.Received,
			Actor: c.Actor, Source: c.Source, Endpoint: c.Subject.Endpoint,
		}
	}
	if s.sigs.Enabled() {
		gr := GateRepo(repo)
		srv.Signatures = s.sigs
		srv.SignaturesOf = sgate.Download{
			Addressed: gr, Holder: gr, Members: []sgate.Repo{gr}, Path: it.Path, Blob: it.Digest, Generated: it.Generated, Arrived: c.Received,
			Actor: c.Actor, Source: c.Source, Endpoint: c.Subject.Endpoint,
		}
	}
	srv.Rules = s.rulesRefusal(c, repo, it.Path, t.class)
	decided, err := srv.RunDecided(ctx)
	if err != nil {
		if rf, ok := rules.RefusalOf(err); ok {
			s.refusals.Refused(ctx, rf, rulestore.RefusalContext{Actor: c.Actor, Source: c.Source, Endpoint: c.Subject.Endpoint})
		}
		return Resolved{}, err
	}
	return Resolved{Repo: repo, Item: it, Decided: decided}, nil
}

// rulesRefusal is the fourth check of a file of /files/ (remote-curation.md R-CUR-003 point 2): the refusal of a remote repository's
// delivery rules for the package and version the format classifies the path in, from the replica's snapshot; nil for a repository
// without rules, a path of no package and a system read.
func (s *Service) rulesRefusal(c Caller, repo repository.Repository, path string, class PathClass) error {
	set := repo.Delivery()
	if set == nil || class.Package == "" || c.Subject.Kind == principal.KindSystem {
		return nil
	}
	var d rc.Decision
	if class.Version == "" {
		d = set.Package(class.Package)
	} else {
		d = set.Decide(class.Package, class.Version)
	}
	if d.Delivered {
		return nil
	}
	n, v := set.Fold(class.Package, class.Version)
	rf := &rules.Refusal{RepoID: repo.NumericID(), Repo: repo.Name, Name: n, Version: v, Reason: d.Reason, Kind: rules.KindArtifact, Path: path}
	return rf.Err(c.BaseURL)
}

// Open opens the content of a resolved file for streaming ([items.Store.Open]).
func (s *Service) Open(ctx context.Context, it items.Item, rng *items.Range) (io.ReadCloser, error) {
	return s.items.Open(ctx, it, rng)
}

// Served records a download that was answered with its content ([items.Store.Served]).
func (s *Service) Served(ctx context.Context, c Caller, res Resolved, countStats bool) {
	s.items.Served(ctx, items.ServedEvent{
		Item: res.Item, Actor: c.Actor, Source: c.Source, Endpoint: c.Subject.Endpoint, CountStats: countStats,
	})
}

// HandedOver records a download handed over to a Bastion ([items.Store.Served], storage.md R-STOR-095): e carries the item and
// the handover's delivery; the caller's actor, source and endpoint are added here, and countStats as for [Service.Served].
func (s *Service) HandedOver(ctx context.Context, c Caller, e items.ServedEvent, countStats bool) {
	e.Actor, e.Source, e.Endpoint, e.CountStats = c.Actor, c.Source, c.Subject.Endpoint, countStats
	s.items.Served(ctx, e)
}
