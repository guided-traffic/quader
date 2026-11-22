// Package statement is Quader's receipt attestation as a statement (signatures.md R-SIG-051, Q-SIG-61): an in-toto statement v1 whose
// predicate, receipt v1, says that this installation received the artifact with this digest, when, from which uploader or upstream, and
// what its checks said at the time. Each fact is one that stays true once signed or carries its date: the trust state of a signature is
// left out (it follows the anchors of a scope), the vulnerability state is a snapshot named by the date of its advisory data, and no
// e-mail address and no secret is in it.
//
// The package builds the statement's bytes from the facts the job gathers (Facts); it reads nothing and signs nothing. Its output is
// deterministic for the same facts, so a golden file holds every case.
package statement

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// The names of the statement (R-SIG-051): the in-toto statement type, Quader's predicate type, and the DSSE payload type it is signed as.
const (
	// Type is the in-toto statement v1 (in-toto attestation framework, spec/v1/statement.md).
	Type = "https://in-toto.io/Statement/v1"
	// PredicateType is receipt v1: a fixed URI of this repository with its version, resolving to the section that defines it. A change
	// that removes or renames a field is v2; an added field keeps v1.
	PredicateType = "https://github.com/guided-traffic/quader/blob/main/docs/spec/signatures.md#receipt-attestation-v1"
	// PayloadType is the DSSE payload type of an in-toto statement (R-SIG-052).
	PayloadType = "application/vnd.in-toto+json"
)

// The bounds of a statement (R-SIG-051).
const (
	// MaxBytes is the most bytes a statement takes; the signatures that do not fit are left out and counted.
	MaxBytes = 16 << 10
	// MaxSignatures is the most signatures a statement names (R-SIG-013's bound per subject).
	MaxSignatures = 32
)

// The origins of an artifact (R-SIG-010).
const (
	OriginUpload = "upload"
	OriginFill   = "fill"
)

// The malware states a statement names beyond R-MAL-002's verdicts: a repository without the scanning.
const (
	MalwareNotScanned = "not_scanned"
	MalwarePending    = "pending"
)

// Facts are what the job read of one attested item (R-SIG-051); Build turns them into the statement.
type Facts struct {
	// Installation is the installation's ID and its base URL; Host is the registry host the subject's name begins with.
	InstallationID string
	BaseURL        string
	Host           string
	// The repository: its project, key, format and class.
	Project, Repository, Format, Class string
	// The artifact: its canonical path, the name and version its format gives it, its media type where the format names one (an image),
	// its size, and its digests (SHA-256 always, SHA-512 where the format's client checks it: npm's integrity).
	Path, Name, Version, MediaType string
	Size                           int64
	SHA256                         [32]byte
	SHA512                         []byte
	// ReceivedAt is when the item was committed; Origin how it arrived.
	ReceivedAt time.Time
	Origin     Origin
	// Signatures are stage 1's signatures attached to the subject in this repository at the issue, without trust states.
	Signatures []Signature
	Malware    Malware
	// Vulnerabilities is the version's evaluation as of its advisory data; the zero value is a format or a version the scanning does not
	// evaluate (not_covered).
	Vulnerabilities Vulnerabilities
	IssuedAt        time.Time
}

// Origin is how the artifact arrived (R-SIG-010): an upload by a principal, with a pipeline's claims for a CI upload, or a remote's fill.
type Origin struct {
	Kind string // OriginUpload or OriginFill
	// An upload: the principal's kind and name as the audit records them, and a CI upload's issuer and allowlisted claims (R-CI-020),
	// never user_email.
	PrincipalKind, PrincipalName string
	CI                           map[string]any
	// A fill: the remote's upstream and when it was fetched.
	Upstream  string
	FetchedAt time.Time
}

// Signature is one of stage 1's signatures of the subject (R-SIG-003): its role, scheme and integrity state, the signer's identity and
// issuer, the key IDs. The trust state is never part of it.
type Signature struct {
	Role, Scheme, Integrity string
	Identity, Issuer        string
	KeyIDs                  []string
}

// Malware is the verdict the attestation names (R-MAL-002): the state, the database version and its build time, and the scan time. A
// repository without the scanning is MalwareNotScanned; a verdict not given yet is MalwarePending. Released is a release of R-MAL-021.
type Malware struct {
	State           string
	Database        string
	DatabaseBuiltAt time.Time
	ScannedAt       time.Time
	Released        bool
}

// Vulnerabilities is the version's evaluation (R-VULN-001), its grade (R-VULN-038), the open findings per severity, and the date of the
// advisory data it was evaluated against.
type Vulnerabilities struct {
	State  string
	Grade  string
	Counts *Counts
	DataAt time.Time
}

// Counts are the open findings of a version per severity.
type Counts struct {
	Critical int `json:"critical"`
	High     int `json:"high"`
	Medium   int `json:"medium"`
	Low      int `json:"low"`
	Unknown  int `json:"unknown"`
}

// ---- the wire form

type statement struct {
	Type          string    `json:"_type"`
	Subject       []subject `json:"subject"`
	PredicateType string    `json:"predicateType"`
	Predicate     receipt   `json:"predicate"`
}

type subject struct {
	Name   string            `json:"name"`
	Digest map[string]string `json:"digest"`
}

type receipt struct {
	Installation      installation    `json:"installation"`
	Repository        repository      `json:"repository"`
	Artifact          artifact        `json:"artifact"`
	ReceivedAt        stamp           `json:"receivedAt"`
	Origin            origin          `json:"origin"`
	Signatures        []signature     `json:"signatures"`
	SignaturesOmitted int             `json:"signaturesOmitted,omitempty"`
	Malware           malware         `json:"malware"`
	Vulnerabilities   vulnerabilities `json:"vulnerabilities"`
	IssuedAt          stamp           `json:"issuedAt"`
}

type installation struct {
	ID  string `json:"id"`
	URL string `json:"url"`
}

type repository struct {
	Project string `json:"project"`
	Key     string `json:"key"`
	Format  string `json:"format"`
	Class   string `json:"class"`
}

type artifact struct {
	Path      string `json:"path"`
	Name      string `json:"name,omitempty"`
	Version   string `json:"version,omitempty"`
	MediaType string `json:"mediaType,omitempty"`
	Size      int64  `json:"size"`
}

type principal struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

type origin struct {
	Kind      string         `json:"kind"`
	Principal *principal     `json:"principal,omitempty"`
	CI        map[string]any `json:"ci,omitempty"`
	Upstream  string         `json:"upstream,omitempty"`
	FetchedAt *stamp         `json:"fetchedAt,omitempty"`
}

type signature struct {
	Role      string   `json:"role"`
	Scheme    string   `json:"scheme"`
	Integrity string   `json:"integrity"`
	Identity  string   `json:"identity,omitempty"`
	Issuer    string   `json:"issuer,omitempty"`
	KeyIDs    []string `json:"keyIds,omitempty"`
}

type malware struct {
	State           string `json:"state"`
	Database        string `json:"database,omitempty"`
	DatabaseBuiltAt *stamp `json:"databaseBuiltAt,omitempty"`
	ScannedAt       *stamp `json:"scannedAt,omitempty"`
	Released        bool   `json:"released,omitempty"`
}

type vulnerabilities struct {
	State  string  `json:"state"`
	Grade  string  `json:"grade,omitempty"`
	Counts *Counts `json:"counts,omitempty"`
	DataAt *stamp  `json:"dataAt,omitempty"`
}

// stamp is an instant as the statement writes it: UTC with milliseconds.
type stamp time.Time

func (s stamp) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Time(s).UTC().Format("2006-01-02T15:04:05.000Z"))
}

func (s *stamp) UnmarshalJSON(b []byte) error {
	var text string
	if err := json.Unmarshal(b, &text); err != nil {
		return err
	}
	t, err := time.Parse(time.RFC3339Nano, text)
	if err != nil {
		return err
	}
	*s = stamp(t.UTC())
	return nil
}

func timeOf(s *stamp) time.Time {
	if s == nil {
		return time.Time{}
	}
	return time.Time(*s)
}

func stampOf(t time.Time) *stamp {
	if t.IsZero() {
		return nil
	}
	s := stamp(t)
	return &s
}

// ErrTooLarge is a statement that does not fit MaxBytes even without its signatures: its CI claims are larger than R-SIG-010 lets in.
var ErrTooLarge = errors.New("statement: larger than 16 KiB without its signatures")

// SubjectName is the name of an attested item as the statement's subject names it (R-SIG-051): <host>/<project>/<repository>/<path> for
// a file, and <host>/<project>/<repository>/<image> for an image, whose subject is the manifest's digest.
func SubjectName(host, project, repo, nameOrPath string) string {
	return host + "/" + project + "/" + repo + "/" + nameOrPath
}

// Build renders the statement of the facts (R-SIG-051): at most MaxSignatures signatures, and fewer when the statement would exceed
// MaxBytes, with the number left out in signaturesOmitted.
func Build(f Facts) ([]byte, error) {
	if f.SHA256 == ([32]byte{}) {
		return nil, errors.New("statement: the subject has no SHA-256")
	}
	st := statement{Type: Type, PredicateType: PredicateType, Subject: []subject{subjectOf(f)}, Predicate: receiptOf(f)}
	sigs := signaturesOf(f.Signatures)
	omitted := 0
	if len(sigs) > MaxSignatures {
		omitted, sigs = len(sigs)-MaxSignatures, sigs[:MaxSignatures]
	}
	for {
		st.Predicate.Signatures, st.Predicate.SignaturesOmitted = sigs, omitted
		b, err := json.Marshal(st)
		if err != nil {
			return nil, err
		}
		if len(b) <= MaxBytes {
			return b, nil
		}
		if len(sigs) == 0 {
			return nil, fmt.Errorf("%w (%d bytes)", ErrTooLarge, len(b))
		}
		sigs, omitted = sigs[:len(sigs)-1], omitted+1
	}
}

func subjectOf(f Facts) subject {
	name := f.Path
	if f.Format == "oci" && f.Name != "" {
		name = f.Name
	}
	d := map[string]string{"sha256": hex.EncodeToString(f.SHA256[:])}
	if len(f.SHA512) == 64 {
		d["sha512"] = hex.EncodeToString(f.SHA512)
	}
	return subject{Name: SubjectName(f.Host, f.Project, f.Repository, name), Digest: d}
}

func receiptOf(f Facts) receipt {
	r := receipt{
		Installation: installation{ID: f.InstallationID, URL: f.BaseURL},
		Repository:   repository{Project: f.Project, Key: f.Repository, Format: f.Format, Class: f.Class},
		Artifact:     artifact{Path: f.Path, Name: f.Name, Version: f.Version, MediaType: f.MediaType, Size: f.Size},
		ReceivedAt:   stamp(f.ReceivedAt),
		Origin:       originOf(f.Origin),
		Malware:      malwareOf(f.Malware),
		IssuedAt:     stamp(f.IssuedAt),
	}
	v := f.Vulnerabilities
	if v.State == "" {
		v.State = "not_covered"
	}
	r.Vulnerabilities = vulnerabilities{State: v.State, Grade: v.Grade, Counts: v.Counts, DataAt: stampOf(v.DataAt)}
	return r
}

func originOf(o Origin) origin {
	if o.Kind == OriginFill {
		return origin{Kind: OriginFill, Upstream: o.Upstream, FetchedAt: stampOf(o.FetchedAt)}
	}
	out := origin{Kind: OriginUpload}
	if o.PrincipalKind != "" {
		out.Principal = &principal{Kind: o.PrincipalKind, Name: o.PrincipalName}
	}
	if len(o.CI) > 0 {
		out.CI = withoutEmail(o.CI)
	}
	return out
}

// withoutEmail drops an e-mail claim a record might still carry: R-CI-020 keeps none, and the statement never names one.
func withoutEmail(ci map[string]any) map[string]any {
	out := make(map[string]any, len(ci))
	for k, v := range ci {
		if k == "user_email" || k == "email" {
			continue
		}
		out[k] = v
	}
	return out
}

func signaturesOf(in []Signature) []signature {
	out := make([]signature, 0, len(in))
	for _, s := range in {
		out = append(out, signature(s))
	}
	return out
}

// Receipt is a statement read back (R-SIG-055): the facts it carries, as Build took them, and the number of signatures Build left out.
// Host is not read (the subject's name begins with it but does not mark where it ends), and the CI claims are what the statement holds.
type Receipt struct {
	Facts
	SignaturesOmitted int
}

// ErrNotReceipt is a statement that is not receipt v1, or that is beyond the bounds Build keeps to.
var ErrNotReceipt = errors.New("statement: not a receipt v1 within its bounds")

// Read reads a statement Build made (R-SIG-055): the inverse of Build for what the statement carries. It checks the statement's type,
// its predicate type and the bounds of R-SIG-051 (at most MaxBytes, at most MaxSignatures signatures, one subject with a SHA-256); it
// verifies nothing, so its caller reads only a statement it took from a bundle of its own store.
func Read(b []byte) (Receipt, error) {
	if len(b) > MaxBytes {
		return Receipt{}, fmt.Errorf("%w: %d bytes", ErrNotReceipt, len(b))
	}
	var st statement
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	if err := dec.Decode(&st); err != nil {
		return Receipt{}, fmt.Errorf("%w: %w", ErrNotReceipt, err)
	}
	if st.Type != Type || st.PredicateType != PredicateType || len(st.Subject) != 1 || len(st.Predicate.Signatures) > MaxSignatures {
		return Receipt{}, ErrNotReceipt
	}
	sum, err := hex.DecodeString(st.Subject[0].Digest["sha256"])
	if err != nil || len(sum) != 32 {
		return Receipt{}, ErrNotReceipt
	}
	p := st.Predicate
	f := Facts{
		InstallationID: p.Installation.ID, BaseURL: p.Installation.URL,
		Project: p.Repository.Project, Repository: p.Repository.Key, Format: p.Repository.Format, Class: p.Repository.Class,
		Path: p.Artifact.Path, Name: p.Artifact.Name, Version: p.Artifact.Version, MediaType: p.Artifact.MediaType, Size: p.Artifact.Size,
		SHA256: [32]byte(sum), ReceivedAt: time.Time(p.ReceivedAt), IssuedAt: time.Time(p.IssuedAt),
		Origin: Origin{Kind: p.Origin.Kind, CI: p.Origin.CI, Upstream: p.Origin.Upstream, FetchedAt: timeOf(p.Origin.FetchedAt)},
		Malware: Malware{
			State: p.Malware.State, Database: p.Malware.Database, DatabaseBuiltAt: timeOf(p.Malware.DatabaseBuiltAt),
			ScannedAt: timeOf(p.Malware.ScannedAt), Released: p.Malware.Released,
		},
		Vulnerabilities: Vulnerabilities{State: p.Vulnerabilities.State, Grade: p.Vulnerabilities.Grade, Counts: p.Vulnerabilities.Counts, DataAt: timeOf(p.Vulnerabilities.DataAt)},
	}
	if s512, err := hex.DecodeString(st.Subject[0].Digest["sha512"]); err == nil && len(s512) == 64 {
		f.SHA512 = s512
	}
	if p.Origin.Principal != nil {
		f.Origin.PrincipalKind, f.Origin.PrincipalName = p.Origin.Principal.Kind, p.Origin.Principal.Name
	}
	for _, s := range p.Signatures {
		f.Signatures = append(f.Signatures, Signature(s))
	}
	return Receipt{Facts: f, SignaturesOmitted: p.SignaturesOmitted}, nil
}

func malwareOf(m Malware) malware {
	if m.State == "" {
		m.State = MalwareNotScanned
	}
	out := malware{State: m.State, Database: m.Database, Released: m.Released}
	if m.State != MalwareNotScanned && m.State != MalwarePending {
		out.DatabaseBuiltAt, out.ScannedAt = stampOf(m.DatabaseBuiltAt), stampOf(m.ScannedAt)
	}
	return out
}
