// Package attest is Quader's receipt attestation of the artifacts its repositories receive (signatures.md R-SIG-051 to R-SIG-054): the
// operation of the kind attestation of the job signing, its discovery, and what the storage hooks and the malware scanning tell it.
//
// An attestation is made once the item's checks have their first verdicts (the malware scan, the vulnerability evaluation, stage 1's
// checks of the signatures attached at the commit) or after signing.attestations.wait_for_checks with what there is; it is made again when
// the malware state changes, when the identity changes and when the identity that made it is retired; it goes with its item. The format
// says which items are subjects (formats.SignatureAdapter.AttestationSubjects) when the job signs, never at the commit. The statement
// (internal/signing/statement) is signed as a Sigstore bundle (internal/signing/bundle) and stored in stage 1's store: a signature of the
// role attestation with identity_id set, attached with the source issued in its repository to the item it attests, with the job's index
// row in signing_attestations. An image's attestation also stands among its referrers where that hides nothing (R-SIG-054, Q-SIG-66).
//
// The package opens no key: the composition root hands Sign the identity and its crypto.Signer from the job's session (R-SIG-049).
package attest

import (
	"context"
	"crypto"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
	"github.com/guided-traffic/quader/internal/signatures"
	sigstore "github.com/guided-traffic/quader/internal/signatures/store"
	"github.com/guided-traffic/quader/internal/signing"
	"github.com/guided-traffic/quader/internal/signing/bundle"
	"github.com/guided-traffic/quader/internal/signing/statement"
	"github.com/guided-traffic/quader/internal/signing/store"
)

// Where an attestation is served (R-SIG-054).
const (
	ServedReferrer = "referrer"
	ServedAPI      = "api"
)

// The bounds of the work.
const (
	// minPoll and maxPoll bound how often a new attestation that waits for its checks looks again: a quarter of the item's age, so the
	// first verdicts of a fresh upload are met within seconds and a slow scan is not polled every second for an hour. The malware
	// verdict wakes it at once (SigningWakeVerdict).
	minPoll = 5 * time.Second
	maxPoll = 5 * time.Minute
)

// Repo is the repository of an attested item as the ports name it.
type Repo struct {
	ID                          int64
	Project, Key, Format, Class string
}

// Item is a stored item the format is asked about.
type Item struct {
	Path string
	Blob [32]byte
	Size int64
}

// Subject is what the format says of an item (formats.AttestationSubject).
type Subject struct {
	Attested                 bool
	Name, Version, MediaType string
	Scanned                  [][32]byte
}

// Placement is where an image's attestation may stand now (formats.AttestationReferrer.ReferrerPlacement): a format without referrers
// answers neither Refer nor a reason.
type Placement struct {
	Refer  bool
	Reason string
}

// Formats is what the job asks the item's format through the format host: its subjects, and for a format whose clients read referrers,
// where an attestation may stand, the referrer's write and its removal.
type Formats interface {
	Subjects(ctx context.Context, repo Repo, items []Item) ([]Subject, error)
	Placement(ctx context.Context, repo Repo, item Item) (Placement, error)
	// PutReferrer commits the referrer of the bundle in transactions of its own and answers its manifest's digest.
	PutReferrer(ctx context.Context, repo Repo, sub Subject, item Item, bundle []byte, issuedAt time.Time) ([32]byte, error)
	// RemoveReferrer removes the referrer of a manifest digest that carries the bundle of a digest, in tx.
	RemoveReferrer(ctx context.Context, tx meta.Tx, repo Repo, item Item, manifest, bundle [32]byte) error
}

// Vulnerabilities reads a version's evaluation as of its advisory data (R-VULN-001, R-VULN-038): pending is true while it is
// pending_scan or pending_data, which a new attestation waits for.
type Vulnerabilities interface {
	Version(ctx context.Context, repo Repo, name, version string) (v statement.Vulnerabilities, pending bool, err error)
}

// Signatures writes a signature Quader made into the signature store (internal/signatures/store), in the job's transaction.
type Signatures interface {
	RecordIssued(ctx context.Context, tx meta.Tx, in sigstore.Issued) (int64, error)
}

// Options configure [New].
type Options struct {
	DB         meta.Runner
	Formats    Formats
	Signatures Signatures
	// Vulns is nil where the vulnerability scanning is not wired: every statement says not_covered.
	Vulns Vulnerabilities
	// Enabled is signing.attestations.enabled, Fills signing.attestations.fills, WaitForChecks signing.attestations.wait_for_checks
	// (R-SIG-058).
	Enabled       bool
	Fills         bool
	WaitForChecks time.Duration
	// MalwareScanning is malware.enabled: false makes every statement say not_scanned.
	MalwareScanning bool
	// BaseURL is server.base_url, the installation's URL in the predicate and the host of a subject's name.
	BaseURL string
	Log     *slog.Logger
	Now     func() time.Time
}

// Attester is the kind attestation of the job signing.
type Attester struct {
	o    Options
	host string
	once sync.Once
	id   string
}

// New returns the attester.
func New(o Options) (*Attester, error) {
	if o.DB == nil || o.Formats == nil || o.Signatures == nil {
		return nil, errors.New("attest: the database, the formats and the signature store are required")
	}
	if o.Log == nil {
		o.Log = slog.Default()
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	host := o.BaseURL
	if u, err := url.Parse(o.BaseURL); err == nil && u.Host != "" {
		host = u.Host
	}
	return &Attester{o: o, host: host}, nil
}

var errForeignTx = errors.New("attest: not a PostgreSQL transaction")

func queries(tx meta.Tx) (*q.Queries, error) {
	ptx, ok := postgres.Unwrap(tx)
	if !ok {
		return nil, errForeignTx
	}
	return q.New(ptx), nil
}

// item is what the job reads of an attested item.
type item struct {
	q.SigningAttestItemRow
	receivedAt time.Time
	blob       [32]byte
}

func (it item) repo() Repo {
	return Repo{ID: it.RepoID, Project: it.ProjectKey, Key: it.RepoKey, Format: it.Format, Class: it.Class}
}

func (it item) asItem() Item { return Item{Path: it.Path, Blob: it.blob, Size: it.Size} }

func (it item) fill() bool { return it.Class == "remote" || it.UpstreamUrl != "" }

// Sign is the operation of one row of the kind attestation (R-SIG-053), in the job's transaction that holds the current identity's
// row: it reconciles the item's attestation with what it should be. An item that is gone, generated, of a system repository, not
// attested by its format, or whose attestation is as it should be ends the row with signing.ErrNothing; a new attestation whose checks
// have not answered waits (signing.Later) until the bound of wait_for_checks.
func (a *Attester) Sign(ctx context.Context, tx meta.Tx, id signing.Identity, key crypto.Signer, w store.Work) error {
	qs, err := queries(tx)
	if err != nil {
		return err
	}
	it, ok, err := a.load(ctx, qs, w.ItemID)
	switch {
	case err != nil:
		return err
	case !ok || !it.Live || it.Generated || it.SystemRepo || !a.o.Enabled || (it.fill() && !a.o.Fills):
		return signing.ErrNothing
	}
	subs, err := a.o.Formats.Subjects(ctx, it.repo(), []Item{it.asItem()})
	if err != nil {
		return err
	}
	cur, attested, err := current(ctx, qs, w.ItemID)
	if err != nil {
		return err
	}
	if len(subs) != 1 || !subs[0].Attested {
		if attested {
			// No subject any more (an index now lists the manifest): its attestation goes.
			return a.drop(ctx, tx, qs, it, cur)
		}
		return signing.ErrNothing
	}
	sub := subs[0]
	ck, err := a.checks(ctx, qs, it, sub)
	if err != nil {
		return err
	}
	switch {
	case !attested:
		if ck.pending {
			if deadline := it.receivedAt.Add(a.o.WaitForChecks); a.o.Now().Before(deadline) {
				return &signing.Later{Until: a.poll(it.receivedAt, deadline)}
			}
		}
	case cur.IdentityID == id.ID && cur.MalwareState == ck.malware.State && cur.Released == ck.malware.Released:
		return a.place(ctx, tx, qs, it, sub, cur)
	}
	return a.issue(ctx, tx, qs, id, key, it, sub, ck, cur, attested)
}

// poll is when a new attestation that waits for its checks looks again: a quarter of the item's age, between minPoll and maxPoll, never
// after the deadline.
func (a *Attester) poll(received, deadline time.Time) time.Time {
	now := a.o.Now()
	next := now.Add(min(max(now.Sub(received)/4, minPoll), maxPoll))
	if next.After(deadline) {
		return deadline
	}
	return next
}

func (a *Attester) load(ctx context.Context, qs *q.Queries, itemID int64) (item, bool, error) {
	rows, err := qs.SigningAttestItem(ctx, itemID)
	if err != nil || len(rows) == 0 {
		return item{}, false, err
	}
	it := item{SigningAttestItemRow: rows[0], receivedAt: parseTime(rows[0].CreatedAt)}
	copy(it.blob[:], rows[0].BlobSha256)
	return it, true, nil
}

func current(ctx context.Context, qs *q.Queries, itemID int64) (q.SigningAttestationRow, bool, error) {
	rows, err := qs.SigningAttestation(ctx, itemID)
	if err != nil || len(rows) == 0 {
		return q.SigningAttestationRow{}, false, err
	}
	return rows[0], true, nil
}

// checked is what the item's checks said: the malware snapshot, the vulnerability snapshot, stage 1's signatures, and whether any of
// them has not given its first verdict.
type checked struct {
	malware    statement.Malware
	vulns      statement.Vulnerabilities
	signatures []statement.Signature
	pending    bool
}

func (a *Attester) checks(ctx context.Context, qs *q.Queries, it item, sub Subject) (checked, error) {
	var ck checked
	scanned := sub.Scanned
	if len(scanned) == 0 {
		scanned = [][32]byte{it.blob}
	}
	if a.o.MalwareScanning {
		blobs := make([][]byte, len(scanned))
		for i := range scanned {
			blobs[i] = scanned[i][:]
		}
		rows, err := qs.SigningAttestVerdicts(ctx, blobs)
		if err != nil {
			return ck, err
		}
		ck.malware = malwareOf(rows, len(scanned))
	} else {
		ck.malware = statement.Malware{State: statement.MalwareNotScanned}
	}
	if a.o.Vulns != nil && sub.Version != "" {
		v, pending, err := a.o.Vulns.Version(ctx, it.repo(), sub.Name, sub.Version)
		if err != nil {
			return ck, err
		}
		ck.vulns, ck.pending = v, pending
	}
	if a.o.Vulns != nil && it.Format == "oci" {
		v, pending, err := a.o.Vulns.Version(ctx, it.repo(), sub.Name, "sha256:"+hex.EncodeToString(it.blob[:]))
		if err != nil {
			return ck, err
		}
		ck.vulns, ck.pending = v, pending
	}
	sigs, err := qs.SigningAttestSignatures(ctx, q.SigningAttestSignaturesParams{Subject: it.blob[:], RepoID: it.RepoID})
	if err != nil {
		return ck, err
	}
	for _, s := range sigs {
		ck.signatures = append(ck.signatures, statement.Signature{
			Role: s.Role, Scheme: s.Scheme, Integrity: s.State, Identity: s.Identity, Issuer: s.Issuer, KeyIDs: s.KeyIds,
		})
		ck.pending = ck.pending || s.State == string(signatures.StatePending)
	}
	ck.pending = ck.pending || ck.malware.State == statement.MalwarePending
	return ck, nil
}

// malwareOf is the malware snapshot of the blobs a statement names (R-SIG-051): infected when one is, else pending when one has no
// verdict yet, else failed or unscannable, else clean; the database and the times of the verdict that decided it, and whether a release
// passes every blob that is not clean.
func malwareOf(rows []q.SigningAttestVerdictsRow, want int) statement.Malware {
	rank := map[string]int{"infected": 5, "pending": 4, "failed": 3, "unscannable": 2, "clean": 1}
	if len(rows) < want {
		return statement.Malware{State: statement.MalwarePending}
	}
	best, released := rows[0], true
	for _, r := range rows {
		if rank[r.State] > rank[best.State] || (rank[r.State] == rank[best.State] && r.ScannedAt > best.ScannedAt) {
			best = r
		}
		if r.State != "clean" && r.State != "pending" {
			released = released && r.Released
		}
	}
	if best.State == "pending" {
		return statement.Malware{State: statement.MalwarePending}
	}
	return statement.Malware{
		State: best.State, Database: best.DbVersion, DatabaseBuiltAt: parseTime(best.DbBuiltAt), ScannedAt: parseTime(best.ScannedAt),
		Released: best.State != "clean" && released,
	}
}

// issue makes the item's attestation (again) with the current identity: the statement, the bundle, its placement, and the rows that
// replace the item's earlier attestation in one transaction; an earlier referrer is removed after the new one is stored (R-SIG-053).
func (a *Attester) issue(ctx context.Context, tx meta.Tx, qs *q.Queries, id signing.Identity, key crypto.Signer, it item, sub Subject,
	ck checked, cur q.SigningAttestationRow, attested bool,
) error {
	installation, err := a.installation(ctx, qs)
	if err != nil {
		return err
	}
	now := a.o.Now().UTC()
	facts := statement.Facts{
		InstallationID: installation, BaseURL: a.o.BaseURL, Host: a.host,
		Project: it.ProjectKey, Repository: it.RepoKey, Format: it.Format, Class: it.Class,
		Path: it.Path, Name: sub.Name, Version: sub.Version, MediaType: sub.MediaType, Size: it.Size, SHA256: it.blob,
		ReceivedAt: it.receivedAt, Origin: originOf(it), Signatures: ck.signatures, Malware: ck.malware, Vulnerabilities: ck.vulns,
		IssuedAt: now,
	}
	if it.Format == "npm" {
		facts.SHA512 = it.Sha512
	}
	stmt, err := statement.Build(facts)
	if err != nil {
		return err
	}
	raw, err := bundle.Sign(stmt, key, id.Certificate)
	if err != nil {
		return err
	}
	pl, err := a.o.Formats.Placement(ctx, it.repo(), it.asItem())
	if err != nil {
		return err
	}
	servedAs, reason := ServedAPI, pl.Reason
	var referrer []byte
	if pl.Refer {
		d, err := a.o.Formats.PutReferrer(ctx, it.repo(), sub, it.asItem(), raw, now)
		if err != nil {
			return fmt.Errorf("the referrer of item %d: %w", it.ID, err)
		}
		servedAs, reason, referrer = ServedReferrer, "", d[:]
	}
	// The signature and its attachment are written as every signature Quader makes is (R-SIG-054); the item's earlier attestation loses
	// its attachment in the same transaction, whichever identity made it.
	sig, err := a.o.Signatures.RecordIssued(ctx, tx, sigstore.Issued{
		Scheme: signatures.SchemeSigstoreBundle, Role: signatures.RoleAttestation, Raw: raw, MediaType: bundle.MediaType,
		Subject: it.blob, IdentityID: id.ID, RepoID: it.RepoID, ItemID: it.ID, Location: it.Path, Name: sub.Name,
		PredicateType: statement.PredicateType, ClaimedAt: now, Replace: true,
		Signer: &signatures.Signer{
			Subject: id.Subject, Issuer: id.Issuer, Identity: firstName(id), AlternativeNames: id.Names, Algorithm: "ECDSA P-256",
			HashAlgorithm: "SHA-256", PredicateType: statement.PredicateType,
			StatementSubjects: []string{statement.SubjectName(a.host, it.ProjectKey, it.RepoKey, subjectPath(it, sub))},
		},
	})
	if err != nil {
		return err
	}
	scanned := sub.Scanned
	if len(scanned) == 0 {
		scanned = [][32]byte{it.blob}
	}
	if err := qs.SigningPutAttestation(ctx, q.SigningPutAttestationParams{
		ItemID: it.ID, RepoID: it.RepoID, SignatureID: sig, IdentityID: id.ID, MalwareState: ck.malware.State, Released: ck.malware.Released,
		Scanned: byteSlices(scanned), ServedAs: servedAs, Referrer: referrer, ApiReason: reason, IssuedAt: now.Format(time.RFC3339Nano),
	}); err != nil {
		return err
	}
	if attested && cur.ServedAs == ServedReferrer && len(cur.Referrer) == 32 && string(cur.Referrer) != string(referrer) {
		return a.o.Formats.RemoveReferrer(ctx, tx, it.repo(), it.asItem(), [32]byte(cur.Referrer), [32]byte(cur.ObjectSha256))
	}
	return nil
}

// place keeps an attestation that is as it should be and puts it where it should stand (R-SIG-054): a referrer that would now hide an
// older signature is removed and the attestation served through the API; a referrer that was deleted is made again from the same
// bundle, with the same digest; one that may stand now where none did is written.
func (a *Attester) place(ctx context.Context, tx meta.Tx, qs *q.Queries, it item, sub Subject, cur q.SigningAttestationRow) error {
	pl, err := a.o.Formats.Placement(ctx, it.repo(), it.asItem())
	if err != nil {
		return err
	}
	switch {
	case cur.ServedAs == ServedReferrer && !pl.Refer:
		if err := a.o.Formats.RemoveReferrer(ctx, tx, it.repo(), it.asItem(), [32]byte(cur.Referrer), [32]byte(cur.ObjectSha256)); err != nil {
			return err
		}
		_, err := qs.SigningSetAttestationPlacement(ctx, q.SigningSetAttestationPlacementParams{ItemID: it.ID, ServedAs: ServedAPI, ApiReason: pl.Reason})
		return err
	case !pl.Refer:
		if cur.ApiReason != pl.Reason {
			_, err := qs.SigningSetAttestationPlacement(ctx, q.SigningSetAttestationPlacementParams{ItemID: it.ID, ServedAs: ServedAPI, ApiReason: pl.Reason})
			return err
		}
		return signing.ErrNothing
	case cur.ServedAs == ServedReferrer:
		stored, err := qs.SigningReferrerStored(ctx, q.SigningReferrerStoredParams{RepoID: it.RepoID, Digest: cur.Referrer})
		if err != nil || stored {
			if err == nil {
				err = signing.ErrNothing
			}
			return err
		}
	}
	bundles, err := qs.SigningBundle(ctx, cur.ObjectSha256)
	if err != nil || len(bundles) == 0 {
		return err
	}
	d, err := a.o.Formats.PutReferrer(ctx, it.repo(), sub, it.asItem(), bundles[0], parseTime(cur.IssuedAt))
	if err != nil {
		return err
	}
	_, err = qs.SigningSetAttestationPlacement(ctx, q.SigningSetAttestationPlacementParams{ItemID: it.ID, ServedAs: ServedReferrer, Referrer: d[:]})
	return err
}

// drop removes the attestation of an item that is no subject any more, its referrer with it.
func (a *Attester) drop(ctx context.Context, tx meta.Tx, qs *q.Queries, it item, cur q.SigningAttestationRow) error {
	if cur.ServedAs == ServedReferrer && len(cur.Referrer) == 32 {
		if err := a.o.Formats.RemoveReferrer(ctx, tx, it.repo(), it.asItem(), [32]byte(cur.Referrer), [32]byte(cur.ObjectSha256)); err != nil {
			return err
		}
	}
	if _, err := qs.SigningDropAttestation(ctx, it.ID); err != nil {
		return err
	}
	return signing.ErrNothing
}

// installation reads the installation's ID once (R-BAK-022: a restore keeps it).
func (a *Attester) installation(ctx context.Context, qs *q.Queries) (string, error) {
	var err error
	a.once.Do(func() {
		a.id, err = qs.SigningInstallationID(ctx)
	})
	if err != nil {
		a.once = sync.Once{}
	}
	return a.id, err
}

// originOf is how the item arrived (R-SIG-010): a fill names the URL it was fetched from and when, an upload its principal and a CI
// job's claims.
func originOf(it item) statement.Origin {
	if it.fill() {
		return statement.Origin{Kind: statement.OriginFill, Upstream: it.UpstreamUrl, FetchedAt: it.receivedAt}
	}
	o := statement.Origin{Kind: statement.OriginUpload, PrincipalKind: it.PrincipalKind, PrincipalName: it.PrincipalName}
	if it.Ci != "" {
		var ci map[string]any
		if json.Unmarshal([]byte(it.Ci), &ci) == nil {
			o.CI = ci
		}
	}
	return o
}

// subjectPath is the part of a subject's name after the repository: the image for an image, the path for every other item.
func subjectPath(it item, sub Subject) string {
	if it.Format == "oci" && sub.Name != "" {
		return sub.Name
	}
	return it.Path
}

// firstName is the identity's first URI, what a verifier names as --certificate-identity (R-SIG-045).
func firstName(id signing.Identity) string {
	for _, n := range id.Names {
		if strings.Contains(n, "://") {
			return n
		}
	}
	if len(id.Names) > 0 {
		return id.Names[0]
	}
	return ""
}

func byteSlices(ds [][32]byte) [][]byte {
	out := make([][]byte, len(ds))
	for i := range ds {
		out[i] = ds[i][:]
	}
	return out
}

func parseTime(text string) time.Time {
	if text == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, text)
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}
