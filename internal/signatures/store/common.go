package store

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/cluster"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
	"github.com/guided-traffic/quader/internal/signatures/anchors"
)

// The actors of the events the store records on behalf of the job, the task and quader migrate (R-SIG-039).
const (
	actorSignatures    = "signatures"
	actorConfiguration = "configuration"
)

// SystemActor is the actor of the job's and the task's events (system:signatures).
func SystemActor() audit.Actor { return audit.Actor{Kind: "system", Name: actorSignatures} }

// ConfigurationActor is the actor of the anchors of the Helm values (system:configuration).
func ConfigurationActor() audit.Actor { return audit.Actor{Kind: "system", Name: actorConfiguration} }

// uuidText is the form of a principal's or a project's ID the statements take; anything else is none.
var uuidText = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// principalOf is the actor's principal as created_by and updated_by keep it: "" for an actor that is no principal (the release, the
// configuration, the job).
func principalOf(a audit.Actor) string {
	if uuidText.MatchString(a.ID) {
		return a.ID
	}
	return ""
}

// details renders an event's details.
func details(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return b
}

func hexOf(b []byte) string { return hex.EncodeToString(b) }

func idText(id int64) string { return strconv.FormatInt(id, 10) }

// changed raises the anchor epoch, announces the change area signature_anchors and wakes the trust job, all in tx (R-SIG-012,
// R-SIG-041). It answers the raised epoch.
func (s *Store) changed(ctx context.Context, qs *q.Queries, tx meta.Tx) (int64, error) {
	epoch, err := qs.SignatureRaiseEpoch(ctx)
	if err != nil {
		return 0, fmt.Errorf("raise the anchor epoch: %w", err)
	}
	if s.o.Changes != nil {
		if err := s.o.Changes.Notify(tx, cluster.AreaSignatureAnchors, 1, map[string]int64{"epoch": epoch}); err != nil {
			return 0, fmt.Errorf("announce the change of anchors: %w", err)
		}
	}
	if s.o.WakeTrust != nil {
		if err := s.o.WakeTrust(ctx, tx); err != nil {
			return 0, fmt.Errorf("wake the trust job: %w", err)
		}
	}
	return epoch, nil
}

// fieldErrors is a list of refusals of a request's members, answered as one Invalid error (validation_failed, R-SIG-015).
type fieldErrors []errs.FieldError

func (f *fieldErrors) add(pointer, code, detail string) {
	*f = append(*f, errs.FieldError{Pointer: pointer, Code: code, Detail: detail})
}

func (f fieldErrors) err() error {
	if len(f) == 0 {
		return nil
	}
	detail := "1 field is invalid."
	if len(f) > 1 {
		detail = strconv.Itoa(len(f)) + " fields are invalid."
	}
	return errs.E(errs.Invalid, errs.CodeValidation, detail, errs.WithFields(f...))
}

// chars is the length of a text as PostgreSQL's length() counts it.
func chars(s string) int { return utf8.RuneCountInString(s) }

// anchorKey is the pattern of an anchor's key (R-SIG-015).
var anchorKey = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// The bounds of an anchor's and a block's texts (R-SIG-015, R-SIG-020).
const (
	maxKey        = 64
	maxTitle      = 128
	maxNote       = 1000
	maxMaterial   = 1 << 20
	maxBlockValue = 1024
	maxIssuer     = 512
	maxReason     = 1000
	// maxHolderNames is the most repositories the event signature.invalid lists (R-SIG-039).
	maxHolderNames = 100
	// maxFilesPerFinding is the most files a finding lists.
	maxFilesPerFinding = 20
)

// checkAnchorTexts refuses a key, a title or a note out of its bounds.
func checkAnchorTexts(fe *fieldErrors, key, title, note *string) {
	if key != nil && (!anchorKey.MatchString(*key) || len(*key) > maxKey) {
		fe.add("/key", anchors.CodeInvalidValue, "The key is 1 to 64 lower-case letters and digits in groups joined by single hyphens.")
	}
	if title != nil && (chars(*title) < 1 || chars(*title) > maxTitle || strings.TrimSpace(*title) == "") {
		fe.add("/title", anchors.CodeInvalidValue, "The title is 1 to 128 characters.")
	}
	if note != nil && chars(*note) > maxNote {
		fe.add("/note", anchors.CodeInvalidValue, "The note is at most 1000 characters.")
	}
}

// decodeMaterial reads an anchor's material as the API and the configuration carry it: the JSON of anchors.Material, nothing else.
func decodeMaterial(raw json.RawMessage) (anchors.Material, *errs.FieldError) {
	var m anchors.Material
	if len(raw) > maxMaterial {
		return m, &errs.FieldError{Pointer: "", Code: anchors.CodeInvalidValue, Detail: "The material is larger than 1 MiB."}
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return m, &errs.FieldError{Pointer: "", Code: anchors.CodeInvalidValue, Detail: "The material does not read: " + err.Error()}
	}
	return m, nil
}

// validateMaterial applies the checks on save (anchors.Validate) and answers the refusal at the member it names.
func (s *Store) validateMaterial(fe *fieldErrors, kind string, m anchors.Material) *anchors.Parsed {
	p, err := anchors.Validate(kind, m, s.o.Now())
	if err == nil {
		return p
	}
	if inv, ok := anchors.AsInvalid(err); ok {
		fe.add(inv.Pointer, inv.Code, inv.Reason)
		return nil
	}
	fe.add("", anchors.CodeInvalidValue, err.Error())
	return nil
}

// encodeMaterial is the stored form of material: the JSON of anchors.Material, so that a comparison of two is a comparison of bytes.
func encodeMaterial(m anchors.Material) (json.RawMessage, error) {
	b, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("encode the material: %w", err)
	}
	return b, nil
}

// storedMaterial reads stored material; what does not read is empty material, which parses as nothing.
func storedMaterial(raw json.RawMessage) anchors.Material {
	var m anchors.Material
	_ = json.Unmarshal(raw, &m) //nolint:errcheck // stored by this store; a row written around it is empty material
	return m
}

// fingerprintsOf is what an anchor shows of its material (R-SIG-015): empty for material that does not parse any more.
func fingerprintsOf(kind string, m anchors.Material) []anchors.Fingerprint {
	p, err := anchors.Parse(kind, m)
	if err != nil || p == nil {
		return []anchors.Fingerprint{}
	}
	if p.Describes == nil {
		return []anchors.Fingerprint{}
	}
	return p.Describes
}

// fingerprintValues are the values of fingerprints, for an event and for the comparison of a narrowing.
func fingerprintValues(fps []anchors.Fingerprint) []string {
	out := make([]string, 0, len(fps))
	for _, f := range fps {
		out = append(out, f.Kind+":"+f.Value)
	}
	return out
}

// subsetOf reports whether every value of a is in b.
func subsetOf(a, b []string) bool {
	in := make(map[string]bool, len(b))
	for _, v := range b {
		in[v] = true
	}
	for _, v := range a {
		if !in[v] {
			return false
		}
	}
	return true
}

// int64Array renders an anchor set as the text of a bigint array, as SignatureTrustUpsert takes it.
func int64Array(ids []int64) string {
	var b strings.Builder
	b.WriteByte('{')
	for i, id := range ids {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatInt(id, 10))
	}
	b.WriteByte('}')
	return b.String()
}
