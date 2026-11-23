// Package npmsign is npm's registry signature of the versions of local npm repositories (signatures.md R-SIG-048 to R-SIG-050,
// Q-SIG-58, Q-SIG-59, Q-SIG-156 to Q-SIG-163): the operation the job signing runs for a row of the kind npm, the discovery of the
// versions that carry no served signature, and the revision raise when what the documents serve changes with the identities' states.
//
// A version's signature is ECDSA P-256 over SHA-256 of "<name>@<version>:<integrity>" with the current identity's key, the entry
// {"keyid", "sig"} of its dist.signatures (R-SIG-050). It is stored as a signature of the store of stage 1, scheme npm-registry and role
// registry, with the identity that made it, attached issued to the version's tarball (R-SIG-054), through the signature store; the
// package documents of the local repository render the signatures whose identity is listed (internal/formatdata/npm). The package
// never holds a key: the job hands it the session's signer, in a transaction that holds the current identity's row (R-SIG-048).
package npmsign

import (
	"context"
	"crypto"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/guided-traffic/quader/internal/formats/catalog"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
	"github.com/guided-traffic/quader/internal/packages"
	"github.com/guided-traffic/quader/internal/signatures"
	sigstore "github.com/guided-traffic/quader/internal/signatures/store"
	"github.com/guided-traffic/quader/internal/signing"
	"github.com/guided-traffic/quader/internal/signing/store"
)

// The working bounds (R-SIG-048, R-SIG-050, Q-SIG-159).
const (
	// maxVersions is the most versions of one package one operation signs: the package's other due rows join its own, so its revision
	// is raised once for all of them.
	maxVersions = 1000
	// maxPackages is the most packages one transaction of the revision raise of R-SIG-048 raises.
	maxPackages = 1000
	// mediaType is the media type of a stored entry.
	mediaType = "application/json"
)

// Signatures is what the package needs of the signature store; *sigstore.Store implements it.
type Signatures interface {
	RecordIssued(ctx context.Context, tx meta.Tx, in sigstore.Issued) (int64, error)
	DetachRetiredIssued(ctx context.Context, tx meta.Tx, itemID int64, scheme string) (int64, error)
}

var _ Signatures = (*sigstore.Store)(nil)

// Work is what the package needs of the signing store; *store.Store implements it.
type Work interface {
	Enqueue(ctx context.Context, tx meta.Tx, kind, reason string, priority int, items []int64, delay time.Duration) error
	Served(ctx context.Context, kind string) ([]int64, bool, error)
	SetServed(ctx context.Context, kind string, ids []int64) error
}

var _ Work = (*store.Store)(nil)

// Signaler attaches a signal to a transaction, published after the commit; *cluster.Signals implements it.
type Signaler interface {
	Notify(tx meta.Tx, area, payload string)
}

// Options configure [New].
type Options struct {
	DB         meta.Runner
	Signatures Signatures
	Work       Work
	// Signals carries the signal that evicts merged metadata early after a raised revision (R-REPO-020); nil sends none.
	Signals Signaler
	Log     *slog.Logger
}

// Signer is npm's half of the job signing.
type Signer struct{ o Options }

// New returns the signer.
func New(o Options) (*Signer, error) {
	if o.DB == nil || o.Signatures == nil || o.Work == nil {
		return nil, errors.New("npm signing: the database, the signature store and the signing store are required")
	}
	if o.Log == nil {
		o.Log = slog.New(slog.DiscardHandler)
	}
	return &Signer{o: o}, nil
}

var errForeignTx = errors.New("npm signing: not a PostgreSQL transaction")

func queries(tx meta.Tx) (*q.Queries, error) {
	ptx, ok := postgres.Unwrap(tx)
	if !ok {
		return nil, errForeignTx
	}
	return q.New(ptx), nil
}

// Sign is the operation of a row of the kind npm (R-SIG-049, R-SIG-050), in the job's transaction that holds the row and the current
// identity's row: it locks the version's package, takes the package's other due rows that no other operation holds, signs every one of
// those versions that a document holds and that carries no served signature, replaces the attachments of a retired identity's
// signature of each, and raises the package's revision and time.modified once when it signed something. A row whose item is no
// version's tarball of a local npm repository (any more) is done without a signature.
func (n *Signer) Sign(ctx context.Context, tx meta.Tx, id signing.Identity, signer crypto.Signer, w store.Work) error {
	qs, err := queries(tx)
	if err != nil {
		return err
	}
	pkgs, err := qs.SigningNPMLockPackage(ctx, w.ItemID)
	if err != nil || len(pkgs) == 0 {
		return err
	}
	pkg := pkgs[0]
	others, err := qs.SigningNPMLockPackageWork(ctx, q.SigningNPMLockPackageWorkParams{PackageID: pkg.ID, ItemID: w.ItemID, MaxRows: maxVersions - 1})
	if err != nil {
		return err
	}
	versions, err := qs.SigningNPMUnsigned(ctx, q.SigningNPMUnsignedParams{PackageID: pkg.ID, ItemIds: append([]int64{w.ItemID}, others...)})
	if err != nil {
		return err
	}
	for _, v := range versions {
		if err := n.signVersion(ctx, tx, id, signer, pkg, v); err != nil {
			return err
		}
	}
	if len(others) > 0 {
		if err := qs.SigningNPMDoneWork(ctx, others); err != nil {
			return err
		}
	}
	if len(versions) == 0 {
		return nil
	}
	return n.raise(ctx, tx, qs, packages.Ref{ID: pkg.ID, RepoID: pkg.RepoID, Name: pkg.Name})
}

// signVersion signs one version and records the signature (R-SIG-050, R-SIG-054).
func (n *Signer) signVersion(ctx context.Context, tx meta.Tx, id signing.Identity, signer crypto.Signer, pkg q.SigningNPMLockPackageRow, v q.SigningNPMUnsignedRow) error {
	msg := signing.NPMMessage(pkg.Name, v.Version, v.Integrity)
	sig, err := signing.SignNPM(signer, id.NPMKeyID, msg)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(msg)
	if _, err := n.o.Signatures.DetachRetiredIssued(ctx, tx, v.ItemID, signatures.SchemeNPMRegistry); err != nil {
		return err
	}
	_, err = n.o.Signatures.RecordIssued(ctx, tx, sigstore.Issued{
		Scheme: signatures.SchemeNPMRegistry, Role: signatures.RoleRegistry, Raw: sig.Entry(), MediaType: mediaType,
		Subject: digest, SubjectName: pkg.Name + "@" + v.Version, SignedDigest: digest[:], DigestAlg: "sha256",
		KeyIDs: []string{id.NPMKeyID}, IdentityID: id.ID, RepoID: pkg.RepoID, ItemID: v.ItemID, Location: v.Path, Name: pkg.Name,
	})
	return err
}

// raise raises a package's revision and time.modified like a dist-tag change (R-NPM-009, R-NPM-012), with the signal that evicts
// merged metadata early (R-REPO-020): every replica's rendition and every virtual repository's merge follows.
func (n *Signer) raise(ctx context.Context, tx meta.Tx, qs *q.Queries, pkg packages.Ref) error {
	pw, err := packages.NewWriter(tx, "", packages.WithRaised(n.raised))
	if err != nil {
		return err
	}
	_, at, err := pw.BumpRevision(ctx, pkg)
	if err != nil {
		return err
	}
	return qs.SigningNPMTouch(ctx, q.SigningNPMTouchParams{PackageID: pkg.ID, At: at.UTC().Format(time.RFC3339Nano)})
}

func (n *Signer) raised(_ context.Context, tx meta.Tx, pkg packages.Ref) error {
	if n.o.Signals != nil {
		n.o.Signals.Notify(tx, catalog.MetadataChangedArea, catalog.PackageChanged(pkg.RepoID, pkg.Name))
	}
	return nil
}

// Discover is discovery's step of the kind npm (R-SIG-049): the tarballs of the versions of local npm repositories a document holds,
// after the cursor, that carry no served signature and have no row of work get work of the reason discovery and priority 3: what was
// published before an identity existed, what a restore loaded, what a retirement left. A row that waits or failed stays the job's: queued
// again, a failed row would start its attempts anew in every run. It answers the last item it looked at, or after when it reached the end.
func (n *Signer) Discover(ctx context.Context, _ signing.Identity, after int64, limit int) (int64, error) {
	last := after
	err := n.o.DB.InTx(ctx, func(tx meta.Tx) error {
		last = after
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		rows, err := qs.SigningNPMDiscover(ctx, q.SigningNPMDiscoverParams{After: after, RowLimit: int32(min(limit, 1<<20))}) //nolint:gosec // bounded
		if err != nil {
			return err
		}
		var unsigned []int64
		for _, r := range rows {
			if !r.Signed && !r.Queued {
				unsigned = append(unsigned, r.ID)
			}
		}
		if len(rows) == limit {
			last = rows[len(rows)-1].ID
		}
		return n.o.Work.Enqueue(ctx, tx, signing.WorkNPM, signing.ReasonDiscovery, signing.PriorityDiscovery, unsigned, 0)
	})
	return last, err
}

// Served follows a change of what the documents serve (R-SIG-048, Q-SIG-160): the identities whose signatures they render are every
// current and previous one while one is current (signing.NPMServed). When that set differs from the one stored at the last look, the
// revision of every package of a local repository whose versions carry a signature of an identity that joined or left it is raised, in
// transactions of maxPackages packages, and the versions of a retired identity get work of the reason retired and priority 2; then the
// set is stored. A run cut short raises again at the next one, which costs a revision and nothing else.
func (n *Signer) Served(ctx context.Context, ids []signing.Identity) error {
	now := signing.NPMServed(ids)
	stored, found, err := n.o.Work.Served(ctx, signing.WorkNPM)
	if err != nil {
		return err
	}
	changed := symmetricDifference(stored, now)
	if len(changed) == 0 {
		if !found {
			return n.o.Work.SetServed(ctx, signing.WorkNPM, now)
		}
		return nil
	}
	retired := []int64{}
	for _, i := range ids {
		if i.State == signing.StateRetired && slices.Contains(changed, i.ID) {
			retired = append(retired, i.ID)
		}
	}
	raised := 0
	for after := int64(0); ; {
		next, n0, err := n.raiseBatch(ctx, changed, retired, after)
		if err != nil {
			return err
		}
		raised += n0
		if next == 0 {
			break
		}
		after = next
	}
	n.o.Log.Info("the identities whose npm signatures the documents serve changed", "served", now, "changed", changed, "packages", raised)
	return n.o.Work.SetServed(ctx, signing.WorkNPM, now)
}

// raiseBatch raises the packages of one batch after the package id; next is the last package of a full batch, 0 at the end.
func (n *Signer) raiseBatch(ctx context.Context, changed, retired []int64, after int64) (next int64, raised int, err error) {
	err = n.o.DB.InTx(ctx, func(tx meta.Tx) error {
		next, raised = 0, 0
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		pkgs, err := qs.SigningNPMPackagesOf(ctx, q.SigningNPMPackagesOfParams{IdentityIds: changed, After: after, MaxRows: maxPackages})
		if err != nil || len(pkgs) == 0 {
			return err
		}
		ids := make([]int64, 0, len(pkgs))
		for _, p := range pkgs {
			if err := n.raise(ctx, tx, qs, packages.Ref{ID: p.ID, RepoID: p.RepoID, Name: p.Name}); err != nil {
				return fmt.Errorf("raise npm package %d: %w", p.ID, err)
			}
			ids = append(ids, p.ID)
		}
		raised = len(pkgs)
		if len(pkgs) == maxPackages {
			next = pkgs[len(pkgs)-1].ID
		}
		if len(retired) == 0 {
			return nil
		}
		items, err := qs.SigningNPMItemsOf(ctx, q.SigningNPMItemsOfParams{IdentityIds: retired, PackageIds: ids})
		if err != nil {
			return err
		}
		return n.o.Work.Enqueue(ctx, tx, signing.WorkNPM, signing.ReasonRetired, signing.PriorityFill, items, 0)
	})
	return next, raised, err
}

// symmetricDifference is the ids in exactly one of a and b, ascending.
func symmetricDifference(a, b []int64) []int64 {
	out := []int64{}
	for _, x := range a {
		if !slices.Contains(b, x) {
			out = append(out, x)
		}
	}
	for _, x := range b {
		if !slices.Contains(a, x) {
			out = append(out, x)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}
